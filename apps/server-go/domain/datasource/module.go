package datasource

import (
	"context"
	"log/slog"

	"github.com/uptrace/bun"
	"go.uber.org/fx"

	"github.com/emergent/emergent-core/domain/datasource/providers/clickup"
	"github.com/emergent/emergent-core/internal/config"
	"github.com/emergent/emergent-core/pkg/encryption"
)

// Module provides data source sync functionality
var Module = fx.Module("datasource",
	fx.Provide(
		NewConfig,
		NewRepository,
		NewJobsService,
		NewProviderRegistry,
		encryption.NewService,
		NewWorker,
		NewHandler,
	),
	fx.Invoke(
		RegisterProviders,
		RegisterWorkerLifecycle,
		RegisterRoutes,
	),
)

// JobsServiceParams for dependency injection
type JobsServiceParams struct {
	fx.In
	DB  *bun.DB
	Log *slog.Logger
	Cfg *config.Config
}

// NewJobsServiceFromParams creates a JobsService from fx params
func NewJobsServiceFromParams(p JobsServiceParams) *JobsService {
	return NewJobsService(p.DB, p.Log, p.Cfg)
}

// clickupAdapter wraps the clickup.Provider to implement datasource.Provider
type clickupAdapter struct {
	provider *clickup.Provider
}

func (a *clickupAdapter) ProviderType() string {
	return a.provider.ProviderType()
}

func (a *clickupAdapter) TestConnection(ctx context.Context, config ProviderConfig) error {
	// Convert datasource.ProviderConfig to clickup.ProviderConfig
	clickupConfig := clickup.ProviderConfig{
		IntegrationID: config.IntegrationID,
		ProjectID:     config.ProjectID,
		Config:        config.Config,
		Metadata:      config.Metadata,
	}
	return a.provider.TestConnection(ctx, clickupConfig)
}

func (a *clickupAdapter) Sync(ctx context.Context, config ProviderConfig, options SyncOptions, progress ProgressCallback) (*SyncResult, error) {
	// Convert datasource types to clickup types
	clickupConfig := clickup.ProviderConfig{
		IntegrationID: config.IntegrationID,
		ProjectID:     config.ProjectID,
		Config:        config.Config,
		Metadata:      config.Metadata,
	}
	clickupOptions := clickup.SyncOptions{
		Limit:           options.Limit,
		FullSync:        options.FullSync,
		ConfigurationID: options.ConfigurationID,
		Custom:          options.Custom,
	}

	// Wrap the progress callback
	var clickupProgress clickup.ProgressCallback
	if progress != nil {
		clickupProgress = func(p clickup.Progress) {
			progress(Progress{
				Phase:           p.Phase,
				TotalItems:      p.TotalItems,
				ProcessedItems:  p.ProcessedItems,
				SuccessfulItems: p.SuccessfulItems,
				FailedItems:     p.FailedItems,
				SkippedItems:    p.SkippedItems,
				Message:         p.Message,
			})
		}
	}

	result, err := a.provider.Sync(ctx, clickupConfig, clickupOptions, clickupProgress)
	if err != nil {
		return nil, err
	}

	// Convert result
	return &SyncResult{
		TotalItems:      result.TotalItems,
		ProcessedItems:  result.ProcessedItems,
		SuccessfulItems: result.SuccessfulItems,
		FailedItems:     result.FailedItems,
		SkippedItems:    result.SkippedItems,
		DocumentIDs:     result.DocumentIDs,
		Errors:          result.Errors,
	}, nil
}

// RegisterProviders registers all available data source providers
func RegisterProviders(registry *ProviderRegistry, db *bun.DB, log *slog.Logger) {
	// Register ClickUp provider (fully implemented)
	clickupProvider := clickup.NewProvider(db, log)
	registry.Register(&clickupAdapter{provider: clickupProvider})

	// Register placeholder providers for other integrations
	// These will be implemented later
	registry.Register(NewNoOpProvider("imap"))
	registry.Register(NewNoOpProvider("gmail_oauth"))
	registry.Register(NewNoOpProvider("google_drive"))

	log.Info("registered data source providers",
		slog.Any("types", registry.ListTypes()))
}

// RegisterWorkerLifecycle registers the sync worker with fx lifecycle
func RegisterWorkerLifecycle(lc fx.Lifecycle, worker *Worker, cfg *Config) {
	if !cfg.Enabled {
		return
	}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			return worker.Start(ctx)
		},
		OnStop: func(ctx context.Context) error {
			return worker.Stop(ctx)
		},
	})
}
