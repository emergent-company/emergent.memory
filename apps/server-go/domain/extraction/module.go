package extraction

import (
	"context"
	"log/slog"
	"time"

	"github.com/uptrace/bun"
	"go.uber.org/fx"

	"github.com/emergent/emergent-core/domain/chunking"
	"github.com/emergent/emergent-core/domain/documents"
	"github.com/emergent/emergent-core/domain/graph"
	"github.com/emergent/emergent-core/internal/config"
	"github.com/emergent/emergent-core/internal/storage"
	"github.com/emergent/emergent-core/pkg/adk"
	"github.com/emergent/emergent-core/pkg/embeddings"
	"github.com/emergent/emergent-core/pkg/kreuzberg"
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
	_, err := a.svc.CreateJob(ctx, CreateJobOptions{
		OrganizationID: orgID,
		ProjectID:      opts.ProjectID,
		DocumentID:     &opts.DocumentID,
		SourceType:     opts.SourceType,
		SourceFilename: opts.SourceFilename,
		MimeType:       opts.MimeType,
		FileSizeBytes:  opts.FileSizeBytes,
		StorageKey:     opts.StorageKey,
	})
	return err
}

func provideParsingJobCreator(svc *DocumentParsingJobsService) documents.ParsingJobCreator {
	return &ParsingJobCreatorAdapter{svc: svc}
}

// Module provides extraction functionality including job queues and workers
var Module = fx.Module("extraction",
	fx.Provide(
		NewExtractionConfig,
		provideGraphEmbeddingJobsService,
		provideGraphEmbeddingWorker,
		provideChunkEmbeddingJobsService,
		provideChunkEmbeddingWorker,
		provideDocumentParsingJobsService,
		provideDocumentParsingWorker,
		provideObjectExtractionJobsService,
		provideTemplatePackSchemaProvider,
		provideObjectExtractionWorker,
		provideAdminHandler,
		provideParsingJobCreator,
	),
	fx.Invoke(
		RegisterAdminRoutes,
		RegisterGraphEmbeddingWorkerLifecycle,
		RegisterChunkEmbeddingWorkerLifecycle,
		RegisterDocumentParsingWorkerLifecycle,
		RegisterObjectExtractionWorkerLifecycle,
	),
)

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
}

// NewExtractionConfig creates extraction configuration from app config
func NewExtractionConfig(cfg *config.Config) *ExtractionConfig {
	return &ExtractionConfig{
		GraphEmbedding:   DefaultGraphEmbeddingConfig(),
		ChunkEmbedding:   DefaultChunkEmbeddingConfig(),
		DocumentParsing:  DefaultDocumentParsingConfig(),
		ObjectExtraction: DefaultObjectExtractionConfig(),
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
) *GraphEmbeddingWorker {
	return NewGraphEmbeddingWorker(jobs, embeds, db, cfg.GraphEmbedding, log)
}

// RegisterGraphEmbeddingWorkerLifecycle registers the worker with fx lifecycle
func RegisterGraphEmbeddingWorkerLifecycle(lc fx.Lifecycle, worker *GraphEmbeddingWorker) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			// Use context.Background() - fx lifecycle context has a 15s timeout
			return worker.Start(context.Background())
		},
		OnStop: func(ctx context.Context) error {
			return worker.Stop(ctx)
		},
	})
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
) *ChunkEmbeddingWorker {
	return NewChunkEmbeddingWorker(jobs, embeds, db, cfg.ChunkEmbedding, log)
}

// RegisterChunkEmbeddingWorkerLifecycle registers the chunk embedding worker with fx lifecycle
func RegisterChunkEmbeddingWorkerLifecycle(lc fx.Lifecycle, worker *ChunkEmbeddingWorker) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			// Use context.Background() - fx lifecycle context has a 15s timeout
			return worker.Start(context.Background())
		},
		OnStop: func(ctx context.Context) error {
			return worker.Stop(ctx)
		},
	})
}

// provideDocumentParsingJobsService creates document parsing jobs service with fx
func provideDocumentParsingJobsService(db bun.IDB, log *slog.Logger, cfg *ExtractionConfig) *DocumentParsingJobsService {
	return NewDocumentParsingJobsService(db, log, cfg.DocumentParsing)
}

// provideObjectExtractionJobsService creates object extraction jobs service with fx
func provideObjectExtractionJobsService(db bun.IDB, log *slog.Logger, cfg *ExtractionConfig) *ObjectExtractionJobsService {
	return NewObjectExtractionJobsService(db, log, cfg.ObjectExtraction)
}

// provideTemplatePackSchemaProvider creates template pack schema provider with fx
func provideTemplatePackSchemaProvider(db bun.IDB, log *slog.Logger) *TemplatePackSchemaProvider {
	return NewTemplatePackSchemaProvider(db, log)
}

// provideObjectExtractionWorker creates object extraction worker with fx
func provideObjectExtractionWorker(
	jobs *ObjectExtractionJobsService,
	graphService *graph.Service,
	docService *documents.Service,
	schemaProvider *TemplatePackSchemaProvider,
	modelFactory *adk.ModelFactory,
	cfg *ExtractionConfig,
	log *slog.Logger,
) *ObjectExtractionWorker {
	workerConfig := &ObjectExtractionWorkerConfig{
		PollInterval:    time.Duration(cfg.ObjectExtraction.WorkerIntervalMs) * time.Millisecond,
		OrphanThreshold: 0.3,
		MaxRetries:      uint(cfg.ObjectExtraction.DefaultMaxRetries),
	}
	return NewObjectExtractionWorker(jobs, graphService, docService, schemaProvider, modelFactory, workerConfig, log)
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
	chunkingService *chunking.Service,
	kreuzbergClient *kreuzberg.Client,
	storageService *storage.Service,
	cfg *ExtractionConfig,
	log *slog.Logger,
) *DocumentParsingWorker {
	workerConfig := &DocumentParsingWorkerConfig{
		Interval:  time.Duration(cfg.DocumentParsing.WorkerIntervalMs) * time.Millisecond,
		BatchSize: cfg.DocumentParsing.WorkerBatchSize,
	}
	return NewDocumentParsingWorker(jobs, documentsRepo, chunkingService, kreuzbergClient, storageService, workerConfig, log)
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
