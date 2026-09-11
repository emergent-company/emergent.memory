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
)

// staticPricing is the canonical retail pricing list, embedded at compile time.
// It is the single source of truth for retail pricing: Sync upserts exactly
// these rows into provider_pricing, and there is no remote registry to fetch
// from. These reflect publicly documented Google AI / Vertex AI / OpenAI /
// DeepSeek pricing as of Sept 2026. All prices are per 1M tokens in USD.
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
}

// PricingSyncService seeds and refreshes provider_pricing from the embedded
// staticPricing list, the canonical source of retail pricing. A daily cron job
// drives the sync.
type PricingSyncService struct {
	repo  pricingUpserter
	sched *scheduler.Scheduler
	log   *slog.Logger
}

// NewPricingSyncService creates a PricingSyncService.
// It registers a daily cron job via the provided scheduler and performs an
// immediate sync at startup so that pricing data is available from first run.
func NewPricingSyncService(repo *Repository, sched *scheduler.Scheduler, log *slog.Logger) *PricingSyncService {
	s := &PricingSyncService{
		repo:  repo,
		sched: sched,
		log:   log.With(logger.Scope("provider.pricing_sync")),
	}

	// Register the daily cron job
	if err := sched.AddCronTask("provider:pricing:sync", pricingSyncSchedule, func(ctx context.Context) error {
		return s.Sync(ctx)
	}); err != nil {
		log.Warn("failed to register pricing sync cron job", logger.Error(err))
	}

	return s
}

// Sync upserts the embedded staticPricing list into provider_pricing. Pricing
// has no remote source: staticPricing is the source of truth, so Sync performs
// no network I/O. It runs once at startup and then daily via cron to refresh
// the table (and to backfill rows after a schema change).
func (s *PricingSyncService) Sync(ctx context.Context) error {
	entries := staticPricingEntries()

	if err := s.repo.UpsertPricing(ctx, entries); err != nil {
		return fmt.Errorf("failed to upsert pricing: %w", err)
	}

	s.log.Info("provider pricing synced",
		slog.Int("models", len(entries)),
		slog.String("source", "embedded static list"),
	)
	return nil
}

// staticPricingEntries returns the embedded canonical pricing list with
// LastSynced stamped to the current time.
func staticPricingEntries() []ProviderPricing {
	now := time.Now().UTC()
	entries := make([]ProviderPricing, len(staticPricing))
	copy(entries, staticPricing)
	for i := range entries {
		entries[i].LastSynced = now
	}
	return entries
}
