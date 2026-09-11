package provider

import (
	"context"
	"log/slog"

	"github.com/uptrace/bun"
	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/domain/scheduler"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/adk"
)

// Module provides the provider domain as an fx module.
// It supplies:
//   - *Repository                  — database access for credentials, policies, pricing
//   - *Registry                    — list of supported providers
//   - *CredentialService            — credential resolution hierarchy (Project → Env)
//   - *ModelCatalogService          — model catalog with API fetch + static fallback
//   - *UsageService                 — async LLM usage event recording
//   - *PricingSyncService           — daily refresh of provider_pricing from the embedded static pricing list
//   - *ModelLimitsSyncService       — daily model output token limits sync from models.dev
//   - *ModelCatalogSyncService      — periodic re-sync of OpenAI-compatible provider model catalogs
//   - adk.CredentialResolver        — adapts CredentialService to pkg/adk interface
var Module = fx.Module("provider",
	fx.Provide(
		provideProviderRepository,
		provideProviderRegistry,
		provideCredentialService,
		provideModelCatalogService,
		provideUsageService,
		providePricingSyncService,
		provideModelLimitsSyncService,
		provideModelCatalogSyncService,
		provideADKCredentialAdapter,
		provideUsageTrackerAdapter,
		provideModelLimitAdapter,
		NewHandler,
	),
	fx.Invoke(
		runStartupPricingSync,
		runStartupModelLimitsSync,
		runStartupModelCatalogSync,
		RegisterRoutes,
	),
)

func provideProviderRepository(db bun.IDB, log *slog.Logger) *Repository {
	return NewRepository(db, log)
}

func provideProviderRegistry() *Registry {
	return NewRegistry()
}

func provideCredentialService(repo *Repository, registry *Registry, catalog *ModelCatalogService, cfg *config.Config, log *slog.Logger) *CredentialService {
	return NewCredentialService(repo, registry, catalog, cfg, log)
}

func provideModelCatalogService(repo *Repository, log *slog.Logger) *ModelCatalogService {
	return NewModelCatalogService(repo, log)
}

func provideUsageService(lc fx.Lifecycle, repo *Repository, db bun.IDB, log *slog.Logger) *UsageService {
	return NewUsageService(lc, repo, db, log)
}

func providePricingSyncService(repo *Repository, sched *scheduler.Scheduler, log *slog.Logger) *PricingSyncService {
	return NewPricingSyncService(repo, sched, log)
}

func provideModelLimitsSyncService(repo *Repository, sched *scheduler.Scheduler, log *slog.Logger) *ModelLimitsSyncService {
	return NewModelLimitsSyncService(repo, sched, log)
}

func provideModelCatalogSyncService(repo *Repository, credsvc *CredentialService, catalog *ModelCatalogService, sched *scheduler.Scheduler, log *slog.Logger) *ModelCatalogSyncService {
	return NewModelCatalogSyncService(repo, credsvc, catalog, sched, log)
}

// runStartupPricingSync seeds provider_pricing from the embedded static pricing
// list on server startup. This ensures the pricing table is populated on first
// run without waiting for the next daily cron execution.
func runStartupPricingSync(lc fx.Lifecycle, pricingSync *PricingSyncService, log *slog.Logger) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			go func() {
				if err := pricingSync.Sync(ctx); err != nil {
					log.Warn("startup pricing sync failed", slog.String("error", err.Error()))
				}
			}()
			return nil
		},
	})
}

// runStartupModelLimitsSync performs an initial model limits sync on server startup.
// This ensures the max_output_tokens column is populated on first run without
// waiting for the next daily cron execution.
func runStartupModelLimitsSync(lc fx.Lifecycle, limitsSync *ModelLimitsSyncService, log *slog.Logger) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			go func() {
				if err := limitsSync.Sync(ctx); err != nil {
					log.Warn("startup model limits sync failed", slog.String("error", err.Error()))
				}
			}()
			return nil
		},
	})
}

// runStartupModelCatalogSync performs an initial OpenAI-compatible provider
// catalog re-sync on server startup, so catalogs are fresh from first boot
// instead of waiting for the first 6-hourly cron pass.
func runStartupModelCatalogSync(lc fx.Lifecycle, catalogSync *ModelCatalogSyncService, log *slog.Logger) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			go func() {
				if err := catalogSync.Sync(ctx); err != nil {
					log.Warn("startup model catalog sync failed", slog.String("error", err.Error()))
				}
			}()
			return nil
		},
	})
}

// provideADKCredentialAdapter exposes CredentialService as adk.CredentialResolver
// via the ADKCredentialAdapter. This is consumed by the adk.Module to inject
// per-request credential resolution into ModelFactory.
func provideADKCredentialAdapter(svc *CredentialService) adk.CredentialResolver {
	return NewADKCredentialAdapter(svc)
}

// provideUsageTrackerAdapter exposes UsageService as adk.ModelWrapper via the
// UsageTrackerAdapter. This is consumed by the adk.Module to inject usage tracking
// into ModelFactory so every LLM created by the factory is automatically wrapped.
func provideUsageTrackerAdapter(svc *UsageService, log *slog.Logger) adk.ModelWrapper {
	return NewUsageTrackerAdapter(svc, log)
}

// provideModelLimitAdapter exposes CredentialService + Repository as
// adk.ModelLimitResolver. Consumed by domain/extraction to cap document text
// at the configured model's context window before sending to the LLM.
func provideModelLimitAdapter(svc *CredentialService, repo *Repository) adk.ModelLimitResolver {
	return NewModelLimitAdapter(svc, repo)
}
