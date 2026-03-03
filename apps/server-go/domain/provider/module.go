package provider

import (
	"context"
	"log/slog"

	"github.com/uptrace/bun"
	"go.uber.org/fx"

	"github.com/emergent-company/emergent/domain/scheduler"
	"github.com/emergent-company/emergent/internal/config"
	"github.com/emergent-company/emergent/pkg/adk"
	"github.com/emergent-company/emergent/pkg/embeddings"
)

// Module provides the provider domain as an fx module.
// It supplies:
//   - *Repository                  — database access for credentials, policies, pricing
//   - *Registry                    — list of supported providers
//   - *CredentialService            — credential resolution hierarchy (Project → Org → Env)
//   - *ModelCatalogService          — model catalog with API fetch + static fallback
//   - *UsageService                 — async LLM usage event recording
//   - *PricingSyncService           — daily pricing sync cron job
//   - adk.CredentialResolver        — adapts CredentialService to pkg/adk interface
//   - embeddings.EmbeddingResolver  — adapts CredentialService to pkg/embeddings interface
var Module = fx.Module("provider",
	fx.Provide(
		provideProviderRepository,
		provideProviderRegistry,
		provideCredentialService,
		provideModelCatalogService,
		provideUsageService,
		providePricingSyncService,
		provideADKCredentialAdapter,
		provideEmbeddingCredentialAdapter,
		NewHandler,
	),
	fx.Invoke(
		runStartupPricingSync,
		RegisterRoutes,
	),
)

func provideProviderRepository(db bun.IDB, log *slog.Logger) *Repository {
	return NewRepository(db, log)
}

func provideProviderRegistry() *Registry {
	return NewRegistry()
}

func provideCredentialService(repo *Repository, registry *Registry, cfg *config.Config, log *slog.Logger) *CredentialService {
	return NewCredentialService(repo, registry, cfg, log)
}

func provideModelCatalogService(repo *Repository, log *slog.Logger) *ModelCatalogService {
	return NewModelCatalogService(repo, log)
}

func provideUsageService(lc fx.Lifecycle, repo *Repository, log *slog.Logger) *UsageService {
	return NewUsageService(lc, repo, log)
}

func providePricingSyncService(repo *Repository, sched *scheduler.Scheduler, log *slog.Logger) *PricingSyncService {
	return NewPricingSyncService(repo, sched, log)
}

// runStartupPricingSync performs an initial pricing sync on server startup.
// This ensures the pricing table is populated on first run without waiting
// for the next daily cron execution.
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

// provideADKCredentialAdapter exposes CredentialService as adk.CredentialResolver
// via the ADKCredentialAdapter. This is consumed by the adk.Module to inject
// per-request credential resolution into ModelFactory.
func provideADKCredentialAdapter(svc *CredentialService) adk.CredentialResolver {
	return NewADKCredentialAdapter(svc)
}

// provideEmbeddingCredentialAdapter exposes CredentialService as embeddings.EmbeddingResolver
// via the EmbeddingCredentialAdapter. Consumed by embeddings.Module.
func provideEmbeddingCredentialAdapter(svc *CredentialService) embeddings.EmbeddingResolver {
	return NewEmbeddingCredentialAdapter(svc)
}
