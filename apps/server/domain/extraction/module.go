package extraction

import (
	"context"
	"log/slog"
	"time"

	"github.com/uptrace/bun"
	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/domain/branches"
	"github.com/emergent-company/emergent.memory/domain/chunking"
	"github.com/emergent-company/emergent.memory/domain/documents"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/domain/projects"
	"github.com/emergent-company/emergent.memory/domain/provider"
	"github.com/emergent-company/emergent.memory/domain/scheduler"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/storage"
	"github.com/emergent-company/emergent.memory/pkg/adk"
	"github.com/emergent-company/emergent.memory/pkg/embeddings"
	"github.com/emergent-company/emergent.memory/pkg/kreuzberg"
	"github.com/emergent-company/emergent.memory/pkg/syshealth"
	"github.com/emergent-company/emergent.memory/pkg/whisper"
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

// provideSysHealthMonitor creates system health monitor with fx
func provideSysHealthMonitor(db bun.IDB, log *slog.Logger) syshealth.Monitor {
	cfg := syshealth.DefaultConfig()
	return syshealth.NewMonitor(cfg, db, log)
}

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
		provideMemorySchemaProvider,
		provideObjectExtractionWorker,
		provideAdminHandler,
		provideEmbeddingControlHandler,
		provideParsingJobCreator,
		provideExtractionJobCreator,
		provideEmbeddingEnqueuer,
		provideRelEmbeddingEnqueuer,
		provideEmbeddingSweepWorker,
	),
	fx.Invoke(
		RegisterSysHealthMonitorLifecycle,
		RegisterAdminRoutes,
		RegisterEmbeddingControlRoutes,
		RegisterGraphEmbeddingWorkerLifecycle,
		RegisterGraphRelationshipEmbeddingWorkerLifecycle,
		RegisterChunkEmbeddingWorkerLifecycle,
		RegisterDocumentParsingWorkerLifecycle,
		RegisterObjectExtractionWorkerLifecycle,
		RegisterEmbeddingSweepWorkerLifecycle,
		registerEmbeddingControlHandlerWithMCP,
		registerDomainToolsWithMCP,
	),
)

// registerEmbeddingControlHandlerWithMCP injects the EmbeddingControlHandler into
// the MCP service so MCP tools can pause/resume/inspect embedding workers.
func registerEmbeddingControlHandlerWithMCP(mcpService *mcp.Service, handler *EmbeddingControlHandler) {
	mcpService.SetEmbeddingControlHandler(handler)
}

// registerDomainToolsWithMCP injects domain classification, schema index, and reextraction
// adapters into the MCP service so the domain tools are available to agents.
func registerDomainToolsWithMCP(
	mcpService *mcp.Service,
	schemaProvider *MemorySchemaProvider,
	objJobsSvc *ObjectExtractionJobsService,
	docService *documents.Service,
	modelFactory *adk.ModelFactory,
	log *slog.Logger,
) {
	classifier := NewDomainClassifierMCPAdapter(modelFactory, schemaProvider, docService, log)
	schemaIndex := NewSchemaIndexMCPAdapter(schemaProvider)
	reextraction := NewReextractionQueuerMCPAdapter(objJobsSvc)

	mcpService.SetDomainClassifier(classifier)
	mcpService.SetSchemaIndex(schemaIndex)
	mcpService.SetReextractionQueuer(reextraction)
}

// provideAdminHandler creates the extraction jobs admin handler
func provideAdminHandler(jobsService *ObjectExtractionJobsService) *AdminHandler {
	return NewAdminHandler(jobsService)
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
	return &ExtractionConfig{
		GraphEmbedding:   DefaultGraphEmbeddingConfig(),
		ChunkEmbedding:   DefaultChunkEmbeddingConfig(),
		DocumentParsing:  DefaultDocumentParsingConfig(),
		ObjectExtraction: DefaultObjectExtractionConfig(),
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
	embeds *embeddings.Service,
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
	return NewGraphEmbeddingWorker(jobs, embeds, db, cfg.GraphEmbedding, log, scaler, usageSvc, usageSvc, appCfg.AgentSafeguards.BudgetEnforcementEnabled)
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
	embeds *embeddings.Service,
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
	return NewChunkEmbeddingWorker(jobs, embeds, db, cfg.ChunkEmbedding, log, scaler, usageSvc, usageSvc, appCfg.AgentSafeguards.BudgetEnforcementEnabled)
}

// RegisterChunkEmbeddingWorkerLifecycle registers the chunk embedding worker with fx lifecycle.
func RegisterChunkEmbeddingWorkerLifecycle(lc fx.Lifecycle, worker *ChunkEmbeddingWorker) {
	RegisterWorkerLifecycle(lc, worker)
}

// provideDocumentParsingJobsService creates document parsing jobs service with fx
func provideDocumentParsingJobsService(db bun.IDB, log *slog.Logger, cfg *ExtractionConfig) *DocumentParsingJobsService {
	return NewDocumentParsingJobsService(db, log, cfg.DocumentParsing)
}

// provideObjectExtractionJobsService creates object extraction jobs service with fx
func provideObjectExtractionJobsService(db bun.IDB, log *slog.Logger, cfg *ExtractionConfig) *ObjectExtractionJobsService {
	return NewObjectExtractionJobsService(db, log, cfg.ObjectExtraction)
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
	schemaProvider *MemorySchemaProvider,
	modelFactory *adk.ModelFactory,
	embeds *embeddings.Service,
	cfg *ExtractionConfig,
	log *slog.Logger,
	monitor syshealth.Monitor,
	limitResolver adk.ModelLimitResolver,
) *ObjectExtractionWorker {
	workerConfig := &ObjectExtractionWorkerConfig{
		PollInterval:    time.Duration(cfg.ObjectExtraction.WorkerIntervalMs) * time.Millisecond,
		Concurrency:     cfg.ObjectExtraction.WorkerConcurrency,
		OrphanThreshold: 0.3,
		MaxRetries:      uint(cfg.ObjectExtraction.DefaultMaxRetries),
	}
	scaler := syshealth.NewConcurrencyScaler(
		monitor,
		"object_extraction",
		cfg.ObjectExtraction.EnableAdaptiveScaling,
		cfg.ObjectExtraction.MinConcurrency,
		cfg.ObjectExtraction.MaxConcurrency,
	)
	// Wire embedding service into schema provider (for vector classification).
	schemaProvider.WithEmbeddingService(embeds)
	return NewObjectExtractionWorker(jobs, graphService, branchService, docService, schemaProvider, modelFactory, embeds, workerConfig, log, scaler).
		WithLimitResolver(limitResolver)
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
	kreuzbergClient *kreuzberg.Client,
	whisperClient *whisper.Client,
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
	return NewDocumentParsingWorker(jobs, documentsRepo, projectsRepo, chunkingService, kreuzbergClient, whisperClient, storageService, workerConfig, log, scaler, extractionJobsSvc)
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
) *EmbeddingSweepWorker {
	return NewEmbeddingSweepWorker(jobs, embeds, db, cfg.EmbeddingSweep, log, usageSvc, usageSvc, appCfg.AgentSafeguards.BudgetEnforcementEnabled)
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
