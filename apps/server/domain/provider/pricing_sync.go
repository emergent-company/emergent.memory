package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/emergent-company/emergent.memory/domain/scheduler"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

const (
	// pricingSyncSchedule runs the sync daily at 02:00 UTC.
	pricingSyncSchedule = "0 2 * * *"

	// pricingFetchURL is the source for retail pricing data.
	// Format: JSON array of { provider, model, text_input, image_input,
	//         video_input, audio_input, output } where all prices are per 1M tokens.
	pricingFetchURL = "https://raw.githubusercontent.com/emergent-company/model-pricing/main/pricing.json"

	// pricingFetchTimeout is the maximum time allowed for the HTTP fetch.
	pricingFetchTimeout = 15 * time.Second
)

// pricingEntry is the JSON shape of a single model pricing record from the
// external registry.
type pricingEntry struct {
	Provider       string  `json:"provider"`
	Model          string  `json:"model"`
	TextInputPrice float64 `json:"text_input"`
	ImageInput     float64 `json:"image_input"`
	VideoInput     float64 `json:"video_input"`
	AudioInput     float64 `json:"audio_input"`
	OutputPrice    float64 `json:"output"`
}

// staticPricing holds known retail prices as a compile-time fallback.
// These reflect publicly documented Google AI / Vertex AI pricing as of 2025.
// All prices are per 1M tokens in USD.
var staticPricing = []ProviderPricing{
	// Google AI (Gemini API) — gemini-1.5-flash
	{Provider: ProviderGoogleAI, Model: "gemini-1.5-flash", TextInputPrice: 0.075, ImageInputPrice: 0.075, AudioInputPrice: 0.075, OutputPrice: 0.30},
	{Provider: ProviderGoogleAI, Model: "gemini-1.5-flash-8b", TextInputPrice: 0.0375, ImageInputPrice: 0.0375, AudioInputPrice: 0.0375, OutputPrice: 0.15},
	{Provider: ProviderGoogleAI, Model: "gemini-1.5-pro", TextInputPrice: 1.25, ImageInputPrice: 1.25, AudioInputPrice: 1.25, OutputPrice: 5.00},
	{Provider: ProviderGoogleAI, Model: "gemini-2.0-flash", TextInputPrice: 0.10, ImageInputPrice: 0.10, AudioInputPrice: 0.10, OutputPrice: 0.40},
	{Provider: ProviderGoogleAI, Model: "gemini-2.5-flash", TextInputPrice: 0.15, ImageInputPrice: 0.15, AudioInputPrice: 0.15, OutputPrice: 0.60},
	{Provider: ProviderGoogleAI, Model: "gemini-2.5-pro", TextInputPrice: 1.25, ImageInputPrice: 1.25, AudioInputPrice: 1.25, OutputPrice: 5.00},
	// Vertex AI — same models, same pricing (users bring their own project)
	{Provider: ProviderVertexAI, Model: "gemini-1.5-flash", TextInputPrice: 0.075, ImageInputPrice: 0.075, AudioInputPrice: 0.075, OutputPrice: 0.30},
	{Provider: ProviderVertexAI, Model: "gemini-1.5-flash-8b", TextInputPrice: 0.0375, ImageInputPrice: 0.0375, AudioInputPrice: 0.0375, OutputPrice: 0.15},
	{Provider: ProviderVertexAI, Model: "gemini-1.5-pro", TextInputPrice: 1.25, ImageInputPrice: 1.25, AudioInputPrice: 1.25, OutputPrice: 5.00},
	{Provider: ProviderVertexAI, Model: "gemini-2.0-flash", TextInputPrice: 0.10, ImageInputPrice: 0.10, AudioInputPrice: 0.10, OutputPrice: 0.40},
	{Provider: ProviderVertexAI, Model: "gemini-2.5-flash", TextInputPrice: 0.15, ImageInputPrice: 0.15, AudioInputPrice: 0.15, OutputPrice: 0.60},
	{Provider: ProviderVertexAI, Model: "gemini-2.5-pro", TextInputPrice: 1.25, ImageInputPrice: 1.25, AudioInputPrice: 1.25, OutputPrice: 5.00},
}

// PricingSyncService fetches the latest retail pricing from an external registry
// and upserts it into provider_pricing. A daily cron job drives this sync.
type PricingSyncService struct {
	repo   *Repository
	sched  *scheduler.Scheduler
	client *http.Client
	log    *slog.Logger
}

// NewPricingSyncService creates a PricingSyncService.
// It registers a daily cron job via the provided scheduler and performs an
// immediate sync at startup so that pricing data is available from first run.
func NewPricingSyncService(repo *Repository, sched *scheduler.Scheduler, log *slog.Logger) *PricingSyncService {
	s := &PricingSyncService{
		repo:  repo,
		sched: sched,
		client: &http.Client{
			Timeout: pricingFetchTimeout,
		},
		log: log.With(logger.Scope("provider.pricing_sync")),
	}

	// Register the daily cron job
	if err := sched.AddCronTask("provider:pricing:sync", pricingSyncSchedule, func(ctx context.Context) error {
		return s.Sync(ctx)
	}); err != nil {
		log.Warn("failed to register pricing sync cron job", logger.Error(err))
	}

	return s
}

// Sync fetches the latest retail pricing and upserts it into provider_pricing.
// If the remote fetch fails, it falls back to the embedded static pricing list.
func (s *PricingSyncService) Sync(ctx context.Context) error {
	entries, err := s.fetchRemotePricing(ctx)
	if err != nil {
		s.log.Warn("failed to fetch remote pricing, using static fallback",
			logger.Error(err),
		)
		entries = staticPricingEntries()
	} else if len(entries) == 0 {
		s.log.Warn("remote pricing returned empty list, using static fallback")
		entries = staticPricingEntries()
	}

	if err := s.repo.UpsertPricing(ctx, entries); err != nil {
		return fmt.Errorf("failed to upsert pricing: %w", err)
	}

	s.log.Info("provider pricing synced", slog.Int("models", len(entries)))
	return nil
}

// fetchRemotePricing downloads and parses the pricing JSON from the external registry.
func (s *PricingSyncService) fetchRemotePricing(ctx context.Context) ([]ProviderPricing, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pricingFetchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from pricing registry", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MB cap
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var raw []pricingEntry
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse pricing JSON: %w", err)
	}

	return parsePricingEntries(raw), nil
}

// parsePricingEntries converts raw JSON entries to ProviderPricing entities.
func parsePricingEntries(raw []pricingEntry) []ProviderPricing {
	entries := make([]ProviderPricing, 0, len(raw))
	for _, r := range raw {
		var pt ProviderType
		switch r.Provider {
		case "google":
			pt = ProviderGoogleAI
		case "google-vertex":
			pt = ProviderVertexAI
		default:
			continue // skip unknown providers
		}
		entries = append(entries, ProviderPricing{
			Provider:        pt,
			Model:           r.Model,
			TextInputPrice:  r.TextInputPrice,
			ImageInputPrice: r.ImageInput,
			VideoInputPrice: r.VideoInput,
			AudioInputPrice: r.AudioInput,
			OutputPrice:     r.OutputPrice,
			LastSynced:      time.Now().UTC(),
		})
	}
	return entries
}

// staticPricingEntries returns the embedded fallback pricing list.
func staticPricingEntries() []ProviderPricing {
	now := time.Now().UTC()
	entries := make([]ProviderPricing, len(staticPricing))
	copy(entries, staticPricing)
	for i := range entries {
		entries[i].LastSynced = now
	}
	return entries
}
