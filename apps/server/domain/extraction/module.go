package extraction

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uptrace/bun"
	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/domain/branches"
	"github.com/emergent-company/emergent.memory/domain/chunking"
	"github.com/emergent-company/emergent.memory/domain/documents"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/domain/modelconfig"
	"github.com/emergent-company/emergent.memory/domain/notifications"
	"github.com/emergent-company/emergent.memory/domain/projects"
	"github.com/emergent-company/emergent.memory/domain/provider"
	"github.com/emergent-company/emergent.memory/domain/scheduler"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/storage"
	"github.com/emergent-company/emergent.memory/pkg/adk"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/embeddings"
	"github.com/emergent-company/emergent.memory/pkg/governor"
	"github.com/emergent-company/emergent.memory/pkg/syshealth"
	"github.com/emergent-company/emergent.memory/pkg/whisper"
	"github.com/emergent-company/emergent.memory/pkg/xberg"
)

// ParsingJobCreatorAdapter adapts DocumentParsingJobsService to documents.ParsingJobCreator
type ParsingJobCreatorAdapter struct {
	svc *DocumentParsingJobsService
}

func (a *ParsingJobCreatorAdapter) CreateJob(ctx context.Context, opts documents.ParsingJobOptions) error {
	var orgID *string
	if opts.OrganizationID != "" {
		orgID = &opts.OrganizationID
	}
	var metadata map[string]interface{}
	if opts.AutoExtract {
		metadata = map[string]interface{}{"auto_extract": true}
	}
	_, err := a.svc.CreateJob(ctx, CreateJobOptions{
		OrganizationID: orgID,
		ProjectID:      opts.ProjectID,
		DocumentID:     &opts.DocumentID,
		SourceType:     opts.SourceType,
		SourceFilename: opts.SourceFilename,
		MimeType:       opts.MimeType,
		FileSizeBytes:  opts.FileSizeBytes,
		StorageKey:     opts.StorageKey,
		Metadata:       metadata,
	})
	return err
}

func provideParsingJobCreator(svc *DocumentParsingJobsService) documents.ParsingJobCreator {
	return &ParsingJobCreatorAdapter{svc: svc}
}

func provideExtractionJobCreator(svc *ObjectExtractionJobsService) documents.ExtractionJobCreator {
	return svc
}

// chunkingServiceAdapter adapts chunking.Service to documents.ChunkingService.
type chunkingServiceAdapter struct {
	svc *chunking.Service
}

func (a *chunkingServiceAdapter) RecreateChunks(ctx context.Context, projectID, documentID string) error {
	_, err := a.svc.RecreateChunks(ctx, projectID, documentID, false)
	return err
}

func provideChunkingService(svc *chunking.Service) documents.ChunkingService {
	return &chunkingServiceAdapter{svc: svc}
}

// embeddingEnqueuerAdapter adapts GraphEmbeddingJobsService to graph.EmbeddingEnqueuer.
type embeddingEnqueuerAdapter struct {
	svc *GraphEmbeddingJobsService
}

func (a *embeddingEnqueuerAdapter) EnqueueEmbedding(ctx context.Context, objectID string) error {
	_, err := a.svc.Enqueue(ctx, EnqueueOptions{ObjectID: objectID})
	return err
}

func (a *embeddingEnqueuerAdapter) EnqueueBatchEmbeddings(ctx context.Context, objectIDs []string) (int, error) {
	return a.svc.EnqueueBatch(ctx, objectIDs, 0)
}

func provideEmbeddingEnqueuer(svc *GraphEmbeddingJobsService) graph.EmbeddingEnqueuer {
	return &embeddingEnqueuerAdapter{svc: svc}
}

// NewEmbeddingEnqueuerAdapter wraps a GraphEmbeddingJobsService as a
// graph.EmbeddingEnqueuer. Used by test servers that build the graph service
// outside the fx container.
func NewEmbeddingEnqueuerAdapter(svc *GraphEmbeddingJobsService) graph.EmbeddingEnqueuer {
	return &embeddingEnqueuerAdapter{svc: svc}
}

// relEmbeddingEnqueuerAdapter adapts GraphRelationshipEmbeddingJobsService to graph.RelationshipEmbeddingEnqueuer.
type relEmbeddingEnqueuerAdapter struct {
	svc *GraphRelationshipEmbeddingJobsService
}

func (a *relEmbeddingEnqueuerAdapter) EnqueueRelationshipEmbedding(ctx context.Context, relationshipID string) error {
	_, err := a.svc.Enqueue(ctx, relationshipID)
	return err
}

func (a *relEmbeddingEnqueuerAdapter) EnqueueBatchRelationshipEmbeddings(ctx context.Context, relationshipIDs []string) (int, error) {
	return a.svc.EnqueueBatch(ctx, relationshipIDs)
}

func provideRelEmbeddingEnqueuer(svc *GraphRelationshipEmbeddingJobsService) graph.RelationshipEmbeddingEnqueuer {
	return &relEmbeddingEnqueuerAdapter{svc: svc}
}

// provideSysHealthMonitor creates system health monitor with fx. It reads DB
// pool utilization from the pgxpool directly (rather than the bun wrapper),
// because stdlib.OpenDBFromPool never sets MaxOpenConns and the bun-backed
// DB.Stats() would always report 0.
func provideSysHealthMonitor(pool *pgxpool.Pool, log *slog.Logger) syshealth.Monitor {
	cfg := syshealth.DefaultConfig()
	return syshealth.NewMonitor(cfg, pool, log)
}

// provideBackgroundPauser exposes the embedding control handler's PauseAll /
// ResumeAll as the governor's BackgroundPauser. The handler already pauses and
// resumes the object, relationship, and sweep embedding workers.
func provideBackgroundPauser(handler *EmbeddingControlHandler) governor.BackgroundPauser {
	return handler
}

// RegisterGovernorModeWatcherLifecycle starts a goroutine that pauses background
// embedding workers when the governor enters Shed and resumes them when it
// leaves Shed.
func RegisterGovernorModeWatcherLifecycle(lc fx.Lifecycle, gov *governor.Governor, pauser governor.BackgroundPauser, log *slog.Logger) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			// Use context.Background() so the watcher is not bounded by fx's
			// 15-second startup timeout.
			go gov.RunModeWatcher(context.Background(), pauser, governorWatchInterval)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			return nil
		},
	})
}

// governorWatchInterval is how often the watcher polls the governor's mode.
const governorWatchInterval = 5 * time.Second

// RegisterSysHealthMonitorLifecycle registers the monitor with fx lifecycle
func RegisterSysHealthMonitorLifecycle(lc fx.Lifecycle, monitor syshealth.Monitor) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			return monitor.Start()
		},
		OnStop: func(ctx context.Context) error {
			return monitor.Stop()
		},
	})
}

// Module provides extraction functionality including job queues and workers
var Module = fx.Module("extraction",
	fx.Provide(
		NewExtractionConfig,
		provideSysHealthMonitor,
		provideGraphEmbeddingJobsService,
		provideGraphEmbeddingWorker,
		provideGraphRelationshipEmbeddingJobsService,
		provideGraphRelationshipEmbeddingWorker,
		provideChunkEmbeddingJobsService,
		provideChunkEmbeddingWorker,
		provideDocumentParsingJobsService,
		provideDocumentParsingWorker,
		provideObjectExtractionJobsService,
		NewRememberExtractionJobFinder,
		NewRememberEmbeddingJobFinder,
		provideMemorySchemaProvider,
		provideCachedEmbeddingService,
		provideObjectExtractionWorker,
		provideAdminHandler,
		provideEmbeddingControlHandler,
		NewProjectEmbeddingHandler,
		provideParsingJobCreator,
		provideExtractionJobCreator,
		provideChunkingService,
		provideEmbeddingEnqueuer,
		provideRelEmbeddingEnqueuer,
		provideEmbeddingSweepWorker,
		provideBackgroundPauser,
		// Expose DomainClassifierHandler for chat handler to pre-classify documents.
		NewMCPDomainClassifierHandler,
		// Expose cross-domain tool adapters to mcp.Service via fx (no setter injection).
		provideEmbeddingControlHandlerForMCP,
		provideSchemaIndexForMCP,
		provideReextractionQueuerForMCP,
		provideDocumentSignalsReaderForMCP,
	),
	fx.Invoke(
		RegisterSysHealthMonitorLifecycle,
		RegisterAdminRoutes,
		RegisterEmbeddingControlRoutes,
		RegisterProjectEmbeddingRoutes,
		RegisterGraphEmbeddingWorkerLifecycle,
		RegisterGraphRelationshipEmbeddingWorkerLifecycle,
		RegisterChunkEmbeddingWorkerLifecycle,
		RegisterDocumentParsingWorkerLifecycle,
		RegisterObjectExtractionWorkerLifecycle,
		RegisterEmbeddingSweepWorkerLifecycle,
		RegisterGovernorModeWatcherLifecycle,
	),
)

// provideEmbeddingControlHandlerForMCP exposes the embedding control handler as
// mcp.EmbeddingControlHandler so MCP tools can pause/resume/inspect embedding workers.
func provideEmbeddingControlHandlerForMCP(handler *EmbeddingControlHandler) mcp.EmbeddingControlHandler {
	return handler
}

// provideSchemaIndexForMCP exposes the schema index adapter as mcp.SchemaIndexHandler.
func provideSchemaIndexForMCP(schemaProvider *MemorySchemaProvider) mcp.SchemaIndexHandler {
	return NewSchemaIndexMCPAdapter(schemaProvider)
}

// provideReextractionQueuerForMCP exposes the reextraction queuer as mcp.ReextractionQueuer.
func provideReextractionQueuerForMCP(objJobsSvc *ObjectExtractionJobsService) mcp.ReextractionQueuer {
	return NewReextractionQueuerMCPAdapter(objJobsSvc)
}

// provideDocumentSignalsReaderForMCP exposes documents.Service as mcp.DocumentSignalsReader.
func provideDocumentSignalsReaderForMCP(docService *documents.Service) mcp.DocumentSignalsReader {
	return docService
}

// NewMCPDomainClassifierHandler creates a DomainClassifierHandler for injection into
// other handlers (e.g. chat handler) — same constructor used by registerDomainToolsWithMCP.
func NewMCPDomainClassifierHandler(
	modelFactory *adk.ModelFactory,
	cachedEmbeds *CachedEmbeddingService,
	schemaProvider *MemorySchemaProvider,
	docService *documents.Service,
	log *slog.Logger,
) mcp.DomainClassifierHandler {
	schemaProvider.WithEmbeddingService(cachedEmbeds)
	return NewDomainClassifierMCPAdapter(modelFactory, cachedEmbeds, schemaProvider, docService, log)
}

// provideCachedEmbeddingService creates a shared cached embedding service used by the
// object extraction worker, the graph/chunk embedding workers, and the MCP domain
// classifier for vector classification. A per-request model resolver keys the cache
// on the embedding model actually used for the call (project overrides included),
// so the cache never returns vectors generated by a different model.
func provideCachedEmbeddingService(embeds *embeddings.Service, db bun.IDB, appCfg *config.Config, modelCfg *modelconfig.Service, log *slog.Logger) *CachedEmbeddingService {
	svc := NewCachedEmbeddingService(embeds, db, appCfg.Embeddings.Model, log)
	svc.WithModelResolver(func(ctx context.Context) string {
		projectIDStr := auth.ProjectIDFromContext(ctx)
		if projectIDStr == "" {
			return ""
		}
		projectID, err := uuid.Parse(projectIDStr)
		if err != nil {
			return ""
		}
		model, _, err := modelCfg.ResolveEmbeddingModel(ctx, projectID)
		if err != nil {
			return ""
		}
		return model
	})
	return svc
}

// provideAdminHandler creates the extraction jobs admin handler. The 409
// no_generative_model contract is enforced by the shared jobs service's
// job-creation chokepoint (issue #1555). Trace preview (spans + estimated cost)
// is wired only when tracing is enabled and a provider repo is available;
// otherwise the handler degrades to no enrichment.
func provideAdminHandler(jobsService *ObjectExtractionJobsService, cfg *config.Config, providerRepo *provider.Repository) *AdminHandler {
	tempoURL := ""
	if cfg.Otel.Enabled() {
		tempoURL = cfg.Otel.InternalTempoQueryURL()
	}
	// Left as a nil interface (not a typed-nil adapter) when no provider repo is
	// wired, so enrichTrace's nil check actually fires instead of dereferencing
	// a nil repository.
	var pricing modelPricing
	if providerRepo != nil {
		pricing = &extractionPricingAdapter{repo: providerRepo}
	}
	return NewAdminHandler(jobsService, tempoURL, pricing)
}

// extractionPricingAdapter wraps *provider.Repository to satisfy the modelPricing
// interface without importing the provider package from admin_handler.go.
type extractionPricingAdapter struct {
	repo *provider.Repository
}

func (a *extractionPricingAdapter) LookupModelPricing(ctx context.Context, model string) (textIn, out float64, found bool) {
	p, err := a.repo.GetPricingByModel(ctx, model)
	if err != nil || p == nil {
		return 0, 0, false
	}
	return p.TextInputPrice, p.OutputPrice, true
}

// ExtractionConfig contains configuration for all extraction services
type ExtractionConfig struct {
	GraphEmbedding   *GraphEmbeddingConfig
	ChunkEmbedding   *ChunkEmbeddingConfig
	DocumentParsing  *DocumentParsingConfig
	ObjectExtraction *ObjectExtractionConfig
	EmbeddingSweep   *EmbeddingSweepConfig
}

// NewExtractionConfig creates extraction configuration from app config
func NewExtractionConfig(cfg *config.Config) *ExtractionConfig {
	objCfg := DefaultObjectExtractionConfig()
	objCfg.SkipStagingBranch = cfg.ExtractionSkipStagingBranch

	// Override embedding worker concurrency/batch/adaptive-scaling from env-driven
	// app config. GraphRelationshipEmbeddingJobsService reuses GraphEmbeddingConfig,
	// so the graph override applies to relationship embedding as well.
	graphCfg := DefaultGraphEmbeddingConfig()
	graphCfg.WorkerConcurrency = cfg.EmbeddingQueue.GraphConcurrency
	graphCfg.WorkerBatchSize = cfg.EmbeddingQueue.GraphBatchSize
	graphCfg.EmbeddingRequestBatchSize = cfg.EmbeddingQueue.GraphRequestBatchSize
	graphCfg.EnableAdaptiveScaling = cfg.EmbeddingQueue.AdaptiveScaling

	chunkCfg := DefaultChunkEmbeddingConfig()
	chunkCfg.WorkerConcurrency = cfg.EmbeddingQueue.ChunkConcurrency
	chunkCfg.WorkerBatchSize = cfg.EmbeddingQueue.ChunkBatchSize
	chunkCfg.EnableAdaptiveScaling = cfg.EmbeddingQueue.AdaptiveScaling

	return &ExtractionConfig{
		GraphEmbedding:   graphCfg,
		ChunkEmbedding:   chunkCfg,
		DocumentParsing:  DefaultDocumentParsingConfig(),
		ObjectExtraction: objCfg,
		EmbeddingSweep:   DefaultEmbeddingSweepConfig(),
	}
}

// provideGraphEmbeddingJobsService creates graph embedding jobs service with fx
func provideGraphEmbeddingJobsService(db bun.IDB, log *slog.Logger, cfg *ExtractionConfig) *GraphEmbeddingJobsService {
	return NewGraphEmbeddingJobsService(db, log, cfg.GraphEmbedding)
}

// provideGraphEmbeddingWorker creates graph embedding worker with fx
func provideGraphEmbeddingWorker(
	jobs *GraphEmbeddingJobsService,
	cachedEmbeds *CachedEmbeddingService,
	db bun.IDB,
	cfg *ExtractionConfig,
	log *slog.Logger,
	monitor syshealth.Monitor,
	usageSvc *provider.UsageService,
	appCfg *config.Config,
) *GraphEmbeddingWorker {
	scaler := syshealth.NewConcurrencyScaler(
		monitor,
		"graph_embedding",
		cfg.GraphEmbedding.EnableAdaptiveScaling,
		cfg.GraphEmbedding.MinConcurrency,
		cfg.GraphEmbedding.MaxConcurrency,
	)
	return NewGraphEmbeddingWorker(jobs, cachedEmbeds, db, cfg.GraphEmbedding, log, scaler, usageSvc, usageSvc, appCfg.AgentSafeguards.BudgetEnforcementEnabled)
}

// RegisterGraphEmbeddingWorkerLifecycle registers the worker with fx lifecycle.
func RegisterGraphEmbeddingWorkerLifecycle(lc fx.Lifecycle, worker *GraphEmbeddingWorker) {
	RegisterWorkerLifecycle(lc, worker)
}

// provideChunkEmbeddingJobsService creates chunk embedding jobs service with fx
func provideChunkEmbeddingJobsService(db bun.IDB, log *slog.Logger, cfg *ExtractionConfig) *ChunkEmbeddingJobsService {
	return NewChunkEmbeddingJobsService(db, log, cfg.ChunkEmbedding)
}

// provideChunkEmbeddingWorker creates chunk embedding worker with fx
func provideChunkEmbeddingWorker(
	jobs *ChunkEmbeddingJobsService,
	cachedEmbeds *CachedEmbeddingService,
	db bun.IDB,
	cfg *ExtractionConfig,
	log *slog.Logger,
	monitor syshealth.Monitor,
	usageSvc *provider.UsageService,
	appCfg *config.Config,
) *ChunkEmbeddingWorker {
	scaler := syshealth.NewConcurrencyScaler(
		monitor,
		"chunk_embedding",
		cfg.ChunkEmbedding.EnableAdaptiveScaling,
		cfg.ChunkEmbedding.MinConcurrency,
		cfg.ChunkEmbedding.MaxConcurrency,
	)
	return NewChunkEmbeddingWorker(jobs, cachedEmbeds, db, cfg.ChunkEmbedding, log, scaler, usageSvc, usageSvc, appCfg.AgentSafeguards.BudgetEnforcementEnabled)
}

// RegisterChunkEmbeddingWorkerLifecycle registers the chunk embedding worker with fx lifecycle.
func RegisterChunkEmbeddingWorkerLifecycle(lc fx.Lifecycle, worker *ChunkEmbeddingWorker) {
	RegisterWorkerLifecycle(lc, worker)
}

// provideDocumentParsingJobsService creates document parsing jobs service with fx
func provideDocumentParsingJobsService(db bun.IDB, log *slog.Logger, cfg *ExtractionConfig) *DocumentParsingJobsService {
	return NewDocumentParsingJobsService(db, log, cfg.DocumentParsing)
}

// provideObjectExtractionJobsService creates object extraction jobs service with fx.
// The modelconfig service supplies the generative-model gate at the job-creation
// chokepoint: CreateJob rejects (ErrNoGenerativeModel) when the project has no
// resolvable generative model, covering admin API, MCP queue-reextraction and
// document auto-extract paths (issue #1555).
func provideObjectExtractionJobsService(db bun.IDB, log *slog.Logger, cfg *ExtractionConfig, modelCfg *modelconfig.Service) *ObjectExtractionJobsService {
	return NewObjectExtractionJobsService(db, log, cfg.ObjectExtraction).WithGenerativeModelResolver(modelCfg)
}

// provideMemorySchemaProvider creates template pack schema provider with fx
func provideMemorySchemaProvider(db bun.IDB, log *slog.Logger) *MemorySchemaProvider {
	return NewMemorySchemaProvider(db, log)
}

// provideObjectExtractionWorker creates object extraction worker with fx
func provideObjectExtractionWorker(
	jobs *ObjectExtractionJobsService,
	graphService *graph.Service,
	branchService *branches.Service,
	docService *documents.Service,
	projectsRepo *projects.Repository,
	schemaProvider *MemorySchemaProvider,
	modelFactory *adk.ModelFactory,
	embeds *embeddings.Service,
	cachedEmbeds *CachedEmbeddingService,
	cfg *ExtractionConfig,
	log *slog.Logger,
	monitor syshealth.Monitor,
	limitResolver adk.ModelLimitResolver,
	notificationsSvc *notifications.Service,
) *ObjectExtractionWorker {
	workerConfig := &ObjectExtractionWorkerConfig{
		PollInterval:      time.Duration(cfg.ObjectExtraction.WorkerIntervalMs) * time.Millisecond,
		Concurrency:       cfg.ObjectExtraction.WorkerConcurrency,
		OrphanThreshold:   0.3,
		MaxRetries:        uint(cfg.ObjectExtraction.DefaultMaxRetries),
		SkipStagingBranch: cfg.ObjectExtraction.SkipStagingBranch,
	}
	scaler := syshealth.NewConcurrencyScaler(
		monitor,
		"object_extraction",
		cfg.ObjectExtraction.EnableAdaptiveScaling,
		cfg.ObjectExtraction.MinConcurrency,
		cfg.ObjectExtraction.MaxConcurrency,
	)
	// Wire shared cached embedding service into schema provider (for vector classification).
	schemaProvider.WithEmbeddingService(cachedEmbeds)
	return NewObjectExtractionWorker(jobs, graphService, branchService, docService, projectsRepo, schemaProvider, modelFactory, embeds, workerConfig, log, scaler).
		WithLimitResolver(limitResolver).
		WithNotifications(notificationsSvc)
}

// RegisterObjectExtractionWorkerLifecycle registers the object extraction worker with fx lifecycle
func RegisterObjectExtractionWorkerLifecycle(lc fx.Lifecycle, worker *ObjectExtractionWorker) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			// Use context.Background() for long-running worker - the fx lifecycle context
			// has a 15-second timeout which would cause all worker operations to fail.
			// The worker has its own stop mechanism via stopCh.
			worker.Start(context.Background())
			return nil
		},
		OnStop: func(ctx context.Context) error {
			worker.Stop()
			return nil
		},
	})
}

// provideDocumentParsingWorker creates document parsing worker with fx
func provideDocumentParsingWorker(
	jobs *DocumentParsingJobsService,
	documentsRepo *documents.Repository,
	projectsRepo *projects.Repository,
	chunkingService *chunking.Service,
	xbergClient *xberg.Client,
	whisperClient *whisper.Client,
	registry *provider.Registry,
	storageService *storage.Service,
	cfg *ExtractionConfig,
	log *slog.Logger,
	monitor syshealth.Monitor,
	extractionJobsSvc *ObjectExtractionJobsService,
) *DocumentParsingWorker {
	workerConfig := &DocumentParsingWorkerConfig{
		Interval:    time.Duration(cfg.DocumentParsing.WorkerIntervalMs) * time.Millisecond,
		BatchSize:   cfg.DocumentParsing.WorkerBatchSize,
		Concurrency: cfg.DocumentParsing.WorkerConcurrency,
	}
	scaler := syshealth.NewConcurrencyScaler(
		monitor,
		"document_parsing",
		cfg.DocumentParsing.EnableAdaptiveScaling,
		cfg.DocumentParsing.MinConcurrency,
		cfg.DocumentParsing.MaxConcurrency,
	)
	// Opt-in vendor speech-to-text (ASR_PROVIDER). Nil (the default) keeps the
	// in-house Whisper client as the audio transcription path.
	vendorASRTranscriber := vendorTranscriberFromEnv(registry, log)
	return NewDocumentParsingWorker(jobs, documentsRepo, projectsRepo, chunkingService, xbergClient, whisperClient, vendorASRTranscriber, storageService, workerConfig, log, scaler, extractionJobsSvc)
}

// RegisterDocumentParsingWorkerLifecycle registers the document parsing worker with fx lifecycle
func RegisterDocumentParsingWorkerLifecycle(lc fx.Lifecycle, worker *DocumentParsingWorker) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			go worker.Start()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			worker.Stop()
			return nil
		},
	})
}

// provideEmbeddingSweepWorker creates the embedding sweep worker with fx
func provideEmbeddingSweepWorker(
	jobs *GraphEmbeddingJobsService,
	embeds *embeddings.Service,
	db bun.IDB,
	cfg *ExtractionConfig,
	log *slog.Logger,
	usageSvc *provider.UsageService,
	appCfg *config.Config,
	gov *governor.Governor,
) *EmbeddingSweepWorker {
	return NewEmbeddingSweepWorker(jobs, embeds, db, cfg.EmbeddingSweep, log, usageSvc, usageSvc, appCfg.AgentSafeguards.BudgetEnforcementEnabled).WithGovernor(gov)
}

// RegisterEmbeddingSweepWorkerLifecycle registers the sweep worker with fx lifecycle.
func RegisterEmbeddingSweepWorkerLifecycle(lc fx.Lifecycle, worker *EmbeddingSweepWorker) {
	RegisterWorkerLifecycle(lc, worker)
}

// provideGraphRelationshipEmbeddingJobsService creates the relationship embedding jobs service.
func provideGraphRelationshipEmbeddingJobsService(db bun.IDB, log *slog.Logger, cfg *ExtractionConfig) *GraphRelationshipEmbeddingJobsService {
	return NewGraphRelationshipEmbeddingJobsService(db, log, cfg.GraphEmbedding)
}

// provideGraphRelationshipEmbeddingWorker creates the relationship embedding worker.
func provideGraphRelationshipEmbeddingWorker(
	jobs *GraphRelationshipEmbeddingJobsService,
	embeds *embeddings.Service,
	db bun.IDB,
	cfg *ExtractionConfig,
	monitor syshealth.Monitor,
	log *slog.Logger,
	usageSvc *provider.UsageService,
	appCfg *config.Config,
) *GraphRelationshipEmbeddingWorker {
	return NewGraphRelationshipEmbeddingWorker(jobs, embeds, db, cfg.GraphEmbedding, monitor, log, usageSvc, usageSvc, appCfg.AgentSafeguards.BudgetEnforcementEnabled)
}

// RegisterGraphRelationshipEmbeddingWorkerLifecycle registers the relationship embedding worker with fx lifecycle.
func RegisterGraphRelationshipEmbeddingWorkerLifecycle(lc fx.Lifecycle, worker *GraphRelationshipEmbeddingWorker) {
	RegisterWorkerLifecycle(lc, worker)
}

// provideEmbeddingControlHandler creates the embedding control handler.
func provideEmbeddingControlHandler(
	objectWorker *GraphEmbeddingWorker,
	relWorker *GraphRelationshipEmbeddingWorker,
	sweepWorker *EmbeddingSweepWorker,
	staleTask *scheduler.StaleJobCleanupTask,
	objectJobsSvc *GraphEmbeddingJobsService,
	relJobsSvc *GraphRelationshipEmbeddingJobsService,
) *EmbeddingControlHandler {
	return NewEmbeddingControlHandler(objectWorker, relWorker, sweepWorker, staleTask, objectJobsSvc, relJobsSvc)
}
