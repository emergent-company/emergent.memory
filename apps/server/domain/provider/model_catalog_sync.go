package provider

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/emergent-company/emergent.memory/domain/scheduler"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

const (
	// modelCatalogSyncSchedule re-syncs configured OpenAI-compatible provider
	// catalogs every 6 hours. Scheduler uses seconds-precision (6-field) cron:
	// "second minute hour dom month dow".
	modelCatalogSyncSchedule = "0 30 */6 * * *"

	// modelCatalogSyncTimeout caps each individual provider's catalog fetch so
	// one slow proxy cannot stall the whole pass.
	modelCatalogSyncTimeout = 15 * time.Second
)

// modelCatalogSyncRepo is the subset of Repository the catalog resync pass
// needs. It is declared as an interface (satisfied by *Repository) so tests can
// substitute an in-memory fake without a database.
type modelCatalogSyncRepo interface {
	ListProjectProviderConfigsByProvider(ctx context.Context, provider ProviderType) ([]ProjectProviderConfig, error)
	UpsertSupportedModels(ctx context.Context, models []ProviderSupportedModel) error
	DeleteSupportedModelsNotIn(ctx context.Context, provider ProviderType, modelNames []string) error
}

// ModelCatalogSyncService periodically re-syncs provider_supported_models for
// configured OpenAI-compatible (openai / LiteLLM) providers. SyncModels only
// runs on provider-config upsert today, so a LiteLLM model list that changes
// afterwards leaves the rates panel and default-model dropdown stale until the
// provider is re-saved. A recurring job (plus a startup pass) re-resolves each
// configured credential and refreshes the catalog snapshot.
type ModelCatalogSyncService struct {
	repo    modelCatalogSyncRepo
	credsvc *CredentialService
	catalog *ModelCatalogService
	sched   *scheduler.Scheduler
	log     *slog.Logger
}

// NewModelCatalogSyncService creates the service and registers the recurring
// catalog re-sync cron job.
func NewModelCatalogSyncService(repo *Repository, credsvc *CredentialService, catalog *ModelCatalogService, sched *scheduler.Scheduler, log *slog.Logger) *ModelCatalogSyncService {
	s := &ModelCatalogSyncService{
		repo:    repo,
		credsvc: credsvc,
		catalog: catalog,
		sched:   sched,
		log:     log.With(logger.Scope("provider.model_catalog_sync")),
	}

	if err := sched.AddCronTask("provider:model_catalog:sync", modelCatalogSyncSchedule, func(ctx context.Context) error {
		return s.Sync(ctx)
	}); err != nil {
		log.Warn("failed to register model catalog sync cron job", logger.Error(err))
	}

	return s
}

// Sync re-resolves every configured OpenAI-compatible provider credential and
// refreshes its model catalog. It iterates over every vendor whose definition
// uses CatalogOpenAIModels (openai, azure-openai, LiteLLM, and the other
// OpenAI-compatible vendors), not just ProviderOpenAI.
//
// The catalog table is keyed globally by (provider, model_name) with no
// per-config (project/base_url) dimension, so the pass cannot resolve-and-prune
// per config: config B's prune would delete config A's models and the final
// catalog would be whichever config ran last. Instead it:
//
//  1. resolves each config's model set WITHOUT persisting anything,
//  2. unions all resolved sets (deduplicated by provider+model_name),
//  3. runs a single upsert against the union, and
//  4. prunes stale rows against the union only when every config contributed a
//     complete live fetch (no fallback, no decrypt/skip failure).
//
// Per-config failures are logged and skipped — they never fail the process and
// never leave the catalog empty: fallback or skipped configs simply suppress the
// prune, so previously synced rows are kept (stale-but-complete over empty).
func (s *ModelCatalogSyncService) Sync(ctx context.Context) error {
	var configs []ProjectProviderConfig
	for _, d := range Builtins() {
		if d.CatalogStrategy != CatalogOpenAIModels {
			continue
		}
		cfgs, err := s.repo.ListProjectProviderConfigsByProvider(ctx, d.Type)
		if err != nil {
			return fmt.Errorf("model catalog resync: list %s provider configs: %w", d.Type, err)
		}
		configs = append(configs, cfgs...)
	}
	if len(configs) == 0 {
		return nil
	}

	// Resolve every config first. ResolveModels never persists, so a config
	// whose /v1/models fetch fails contributes only its configured-model
	// fallback — a partial snapshot that must suppress the prune.
	var union []ProviderSupportedModel
	allFetchedOK := true // prune only when every config yielded a complete live fetch
	resolved := 0
	for i := range configs {
		cfg := configs[i]

		cred, err := s.credsvc.decryptProjectConfig(&cfg)
		if err != nil {
			// Cannot know this config's model set, so its previously synced
			// rows must survive — do not prune this pass.
			allFetchedOK = false
			s.log.Debug("model catalog resync: skipping provider config (credential decryption failed)",
				logger.Error(err),
				slog.String("projectID", cfg.ProjectID),
			)
			continue
		}

		syncCtx, cancel := context.WithTimeout(ctx, modelCatalogSyncTimeout)
		models, pruneOK, resolveErr := s.catalog.ResolveModels(syncCtx, cfg.Provider, cred)
		cancel()
		if resolveErr != nil {
			allFetchedOK = false
			s.log.Warn("model catalog resync: resolve failed for provider config",
				logger.Error(resolveErr),
				slog.String("projectID", cfg.ProjectID),
			)
			continue
		}
		resolved++

		if !pruneOK {
			allFetchedOK = false
		}
		union = unionSupportedModels(union, models)
	}

	if len(union) == 0 {
		s.log.Warn("model catalog resync: no models resolved from any provider config",
			slog.Int("configs", len(configs)),
		)
		return nil
	}

	// Single persist for the whole pass.
	if err := s.repo.UpsertSupportedModels(ctx, union); err != nil {
		return fmt.Errorf("model catalog resync: upsert supported models: %w", err)
	}

	// Single prune against the full union, only when every config resolved a
	// complete live catalog. Any fallback or skipped config means the union may
	// be missing rows, so stale rows are kept rather than deleted. The union may
	// span several OpenAI-compatible vendors, so prune each provider separately.
	if allFetchedOK {
		namesByProvider := make(map[ProviderType][]string)
		for _, m := range union {
			namesByProvider[m.Provider] = append(namesByProvider[m.Provider], m.ModelName)
		}
		for p, names := range namesByProvider {
			if err := s.repo.DeleteSupportedModelsNotIn(ctx, p, names); err != nil {
				// Non-fatal: stale rows are cosmetic, don't fail the whole sync.
				s.log.Warn("model catalog resync: failed to delete stale models",
					logger.Error(err),
				)
			}
		}
	}

	s.log.Info("model catalog resync complete",
		slog.Int("configs", len(configs)),
		slog.Int("resolved", resolved),
		slog.Int("models", len(union)),
	)
	return nil
}

// unionSupportedModels merges resolved model snapshots (one per provider
// config) into a single catalog set, deduplicated by (provider, model_name).
// Rows are appended in input order; the first row for a model wins unless a
// later row carries more catalog detail (context-window limits) than the
// existing one — a live fetch's token limits upgrade an earlier token-less
// fallback row for the same model. Rows without a provider or model name are
// dropped.
func unionSupportedModels(sets ...[]ProviderSupportedModel) []ProviderSupportedModel {
	type key struct {
		provider ProviderType
		name     string
	}

	idx := make(map[key]int, 32)
	richer := func(a, b ProviderSupportedModel) bool {
		aHasLimits := a.MaxInputTokens != nil || a.MaxOutputTokens != nil
		bHasLimits := b.MaxInputTokens != nil || b.MaxOutputTokens != nil
		return aHasLimits && !bHasLimits
	}

	var out []ProviderSupportedModel
	for _, set := range sets {
		for _, m := range set {
			if m.Provider == "" || m.ModelName == "" {
				continue
			}
			k := key{provider: m.Provider, name: m.ModelName}
			if i, ok := idx[k]; ok {
				if richer(m, out[i]) {
					out[i] = m
				}
				continue
			}
			idx[k] = len(out)
			out = append(out, m)
		}
	}
	return out
}
