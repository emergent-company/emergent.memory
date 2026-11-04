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
	// pricingSyncSchedule runs the sync daily at 02:00 UTC.
	// Scheduler uses seconds-precision (6-field) cron: "second minute hour dom month dow".
	pricingSyncSchedule = "0 0 2 * * *"

	// startupPricingSyncTimeout bounds the best-effort startup sync. It is
	// deliberately longer than modelsDevPricingFetchTimeout (30s) so a slow
	// feed cannot consume the whole budget and abort the baseline upsert that
	// follows the fetch.
	startupPricingSyncTimeout = 2 * time.Minute
)

// staticPricing is the embedded retail-pricing baseline. It is always upserted,
// and it is the guaranteed fallback when the models.dev feed is unavailable, so
// a feed outage never removes already-known rates. Sync overlays the models.dev
// feed on top for every registry vendor (see pricing_modelsdev.go); rows here
// cover the legacy vendors with hand-verified rates. These reflect publicly
// documented Google AI / Vertex AI / OpenAI / DeepSeek pricing as of Sept 2026.
// All prices are per 1M tokens in USD.
//
// Embedding prices are text-input rates only (embedding usage events carry no
// output tokens).
var staticPricing = []ProviderPricing{
	// Google AI (Gemini API) — gemini-1.5-flash
	{Provider: ProviderGoogleAI, Model: "gemini-1.5-flash", TextInputPrice: 0.075, ImageInputPrice: 0.075, AudioInputPrice: 0.075, OutputPrice: 0.30},
	{Provider: ProviderGoogleAI, Model: "gemini-1.5-flash-8b", TextInputPrice: 0.0375, ImageInputPrice: 0.0375, AudioInputPrice: 0.0375, OutputPrice: 0.15},
	{Provider: ProviderGoogleAI, Model: "gemini-1.5-pro", TextInputPrice: 1.25, ImageInputPrice: 1.25, AudioInputPrice: 1.25, OutputPrice: 5.00},
	{Provider: ProviderGoogleAI, Model: "gemini-2.0-flash", TextInputPrice: 0.10, ImageInputPrice: 0.10, AudioInputPrice: 0.10, OutputPrice: 0.40},
	{Provider: ProviderGoogleAI, Model: "gemini-2.5-flash", TextInputPrice: 0.15, ImageInputPrice: 0.15, AudioInputPrice: 0.15, OutputPrice: 0.60},
	{Provider: ProviderGoogleAI, Model: "gemini-2.5-pro", TextInputPrice: 1.25, ImageInputPrice: 1.25, AudioInputPrice: 1.25, OutputPrice: 5.00},
	{Provider: ProviderGoogleAI, Model: "gemini-3.1-flash-lite-preview", TextInputPrice: 0.10, ImageInputPrice: 0.10, AudioInputPrice: 0.10, OutputPrice: 0.40},
	{Provider: ProviderGoogleAI, Model: "gemini-3.1-flash", TextInputPrice: 0.15, ImageInputPrice: 0.15, AudioInputPrice: 0.15, OutputPrice: 0.60},
	{Provider: ProviderGoogleAI, Model: "gemini-3.1-pro", TextInputPrice: 1.25, ImageInputPrice: 1.25, AudioInputPrice: 1.25, OutputPrice: 5.00},
	// Google AI (Gemini API) — embeddings, text input price per 1M tokens.
	// gemini-embedding-2-preview's list price is not published separately;
	// it is charged at the GA Gemini Embedding 2 rate.
	{Provider: ProviderGoogleAI, Model: "gemini-embedding-001", TextInputPrice: 0.15},
	{Provider: ProviderGoogleAI, Model: "gemini-embedding-2", TextInputPrice: 0.20},
	{Provider: ProviderGoogleAI, Model: "gemini-embedding-2-preview", TextInputPrice: 0.20},
	// Vertex AI — same models, same pricing (users bring their own project)
	{Provider: ProviderVertexAI, Model: "gemini-1.5-flash", TextInputPrice: 0.075, ImageInputPrice: 0.075, AudioInputPrice: 0.075, OutputPrice: 0.30},
	{Provider: ProviderVertexAI, Model: "gemini-1.5-flash-8b", TextInputPrice: 0.0375, ImageInputPrice: 0.0375, AudioInputPrice: 0.0375, OutputPrice: 0.15},
	{Provider: ProviderVertexAI, Model: "gemini-1.5-pro", TextInputPrice: 1.25, ImageInputPrice: 1.25, AudioInputPrice: 1.25, OutputPrice: 5.00},
	{Provider: ProviderVertexAI, Model: "gemini-2.0-flash", TextInputPrice: 0.10, ImageInputPrice: 0.10, AudioInputPrice: 0.10, OutputPrice: 0.40},
	{Provider: ProviderVertexAI, Model: "gemini-2.5-flash", TextInputPrice: 0.15, ImageInputPrice: 0.15, AudioInputPrice: 0.15, OutputPrice: 0.60},
	{Provider: ProviderVertexAI, Model: "gemini-2.5-pro", TextInputPrice: 1.25, ImageInputPrice: 1.25, AudioInputPrice: 1.25, OutputPrice: 5.00},
	{Provider: ProviderVertexAI, Model: "gemini-3.1-flash-lite-preview", TextInputPrice: 0.10, ImageInputPrice: 0.10, AudioInputPrice: 0.10, OutputPrice: 0.40},
	{Provider: ProviderVertexAI, Model: "gemini-3.1-flash", TextInputPrice: 0.15, ImageInputPrice: 0.15, AudioInputPrice: 0.15, OutputPrice: 0.60},
	{Provider: ProviderVertexAI, Model: "gemini-3.1-pro", TextInputPrice: 1.25, ImageInputPrice: 1.25, AudioInputPrice: 1.25, OutputPrice: 5.00},
	// Vertex AI — embeddings, text input price per 1M tokens.
	{Provider: ProviderVertexAI, Model: "gemini-embedding-001", TextInputPrice: 0.15},
	{Provider: ProviderVertexAI, Model: "gemini-embedding-2", TextInputPrice: 0.20},
	{Provider: ProviderVertexAI, Model: "gemini-embedding-2-preview", TextInputPrice: 0.20},
	{Provider: ProviderVertexAI, Model: "text-embedding-004", TextInputPrice: 0.025},
	// OpenAI — embeddings, text input price per 1M tokens. Useful when
	// OpenAI-compatible/LiteLLM proxies serve embedding models; the model-only
	// fallback matches these regardless of the recorded provider.
	{Provider: ProviderOpenAI, Model: "text-embedding-3-small", TextInputPrice: 0.02},
	{Provider: ProviderOpenAI, Model: "text-embedding-3-large", TextInputPrice: 0.13},
	{Provider: ProviderOpenAI, Model: "text-embedding-ada-002", TextInputPrice: 0.10},
	// DeepSeek — generative only, no embeddings. Prices per 1M tokens USD.
	{Provider: ProviderDeepSeek, Model: "deepseek-v4-flash", TextInputPrice: 0.14, OutputPrice: 0.28},
	{Provider: ProviderDeepSeek, Model: "deepseek-v4-pro", TextInputPrice: 1.74, OutputPrice: 3.48},
	{Provider: ProviderDeepSeek, Model: "deepseek-chat", TextInputPrice: 0.28, OutputPrice: 0.42},
	{Provider: ProviderDeepSeek, Model: "deepseek-reasoner", TextInputPrice: 0.28, OutputPrice: 0.42},
}

// pricingUpserter is the subset of Repository used by PricingSyncService.
// It exists so Sync can be exercised with a recording double in tests without
// a database.
type pricingUpserter interface {
	UpsertPricing(ctx context.Context, entries []ProviderPricing) error
	// PruneStalePricing deletes managed-vendor rows absent from keep and
	// returns the number removed. Callers must only invoke it after a feed
	// fetch+parse succeeded.
	PruneStalePricing(ctx context.Context, managedProviders []ProviderType, keep []ProviderPricing) (int64, error)
}

// PricingSyncService seeds and refreshes provider_pricing. It merges the
// embedded staticPricing baseline with rates fetched from the models.dev
// catalog (the same feed used for token limits). A daily cron job drives the
// sync.
type PricingSyncService struct {
	repo    pricingUpserter
	fetcher pricingRatesFetcher
	sched   *scheduler.Scheduler
	log     *slog.Logger
}

// NewPricingSyncService creates a PricingSyncService.
// It registers a daily cron job via the provided scheduler and performs an
// immediate sync at startup so that pricing data is available from first run.
func NewPricingSyncService(repo *Repository, sched *scheduler.Scheduler, log *slog.Logger) *PricingSyncService {
	s := &PricingSyncService{
		repo:    repo,
		fetcher: newModelsDevPricingFetcher(),
		sched:   sched,
		log:     log.With(logger.Scope("provider.pricing_sync")),
	}

	// Register the daily cron job
	if err := sched.AddCronTask("provider:pricing:sync", pricingSyncSchedule, func(ctx context.Context) error {
		return s.Sync(ctx)
	}); err != nil {
		log.Warn("failed to register pricing sync cron job", logger.Error(err))
	}

	return s
}

// Sync upserts retail pricing into provider_pricing: the embedded staticPricing
// baseline overlaid with rates fetched from models.dev. If the feed is
// unavailable the baseline alone is written and the failure is logged, so a
// transient outage cannot wipe known rates. When the feed fetch+parse succeeds,
// managed-vendor rows the feed no longer publishes (removed or now $0) are
// pruned so stale rates do not persist.
func (s *PricingSyncService) Sync(ctx context.Context) error {
	remote, fetched := s.fetchRemoteRates(ctx)
	entries := mergePricingEntries(remote)

	if err := s.repo.UpsertPricing(ctx, entries); err != nil {
		return fmt.Errorf("failed to upsert pricing: %w", err)
	}

	if fetched {
		pruned, err := s.repo.PruneStalePricing(ctx, managedPricingProviders(), entries)
		if err != nil {
			return fmt.Errorf("failed to prune stale pricing: %w", err)
		}
		if pruned > 0 {
			s.log.Info("provider pricing: pruned stale rows",
				slog.Int64("rows", pruned),
			)
		}
	}

	s.log.Info("provider pricing synced",
		slog.Int("rows", len(entries)),
		slog.Int("static_baseline", len(staticPricing)),
		slog.Bool("feed_parsed", fetched),
		slog.String("source", "models.dev overlay on embedded static list"),
	)
	return nil
}

// fetchRemoteRates returns models.dev rates for mapped registry vendors and
// whether the fetch+parse succeeded. It never returns an error: the static
// baseline is sufficient to keep the pricing table populated. The success flag
// gates stale-row pruning — an outage or parse failure must never wipe rows.
func (s *PricingSyncService) fetchRemoteRates(ctx context.Context) ([]fetchedPricing, bool) {
	if s.fetcher == nil {
		return nil, false
	}

	rates, err := s.fetcher.FetchRates(ctx)
	if err != nil {
		s.log.Warn("failed to fetch models.dev pricing, using embedded static baseline",
			logger.Error(err),
		)
		return nil, false
	}

	// An empty but successfully parsed feed is treated as untrustworthy for
	// pruning: wiping every managed row on a schema-drift/empty response is far
	// worse than leaving some stale rates until the next sync. Upsert still runs.
	if len(rates) == 0 {
		s.log.Warn("models.dev pricing feed parsed empty, skipping stale-row pruning")
		return nil, false
	}
	return rates, true
}
