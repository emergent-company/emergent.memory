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
	// modelLimitsSyncSchedule runs the sync daily at 03:00 UTC.
	modelLimitsSyncSchedule = "0 3 * * *"

	// modelLimitsFetchURL is the models.dev canonical API endpoint.
	modelLimitsFetchURL = "https://models.dev/api.json"

	// modelLimitsFetchTimeout is the maximum time allowed for the HTTP fetch.
	modelLimitsFetchTimeout = 15 * time.Second
)

// staticModelLimits maps model name → max output tokens.
// These are fallback values from models.dev as of 2025 for the 6 platform-known models.
// Only generative models are included (embeddings are excluded).
var staticModelLimits = map[string]int{
	"gemini-1.5-flash":              8192,
	"gemini-1.5-flash-8b":           8192,
	"gemini-1.5-pro":                8192,
	"gemini-2.0-flash":              8192,
	"gemini-2.5-flash":              65536,
	"gemini-2.5-pro":                65536,
	"gemini-3.1-flash-lite-preview": 65536,
	"gemini-3.1-flash":              65536,
	"gemini-3.1-pro":                65536,
	// DeepSeek models
	"deepseek-v4-flash": 393216, // 384K output
	"deepseek-v4-pro":   393216, // 384K output
	"deepseek-chat":     8192,
	"deepseek-reasoner": 65536,
}

// modelsDevResponse is the top-level shape of the models.dev /api.json response.
// It maps provider slug → provider entry (which contains a models map).
type modelsDevResponse map[string]modelsDevProviderEntry

// modelsDevProviderEntry is a single provider entry in the models.dev response.
type modelsDevProviderEntry struct {
	Models map[string]modelsDevModel `json:"models"`
}

// modelsDevModel is a single model entry from models.dev.
type modelsDevModel struct {
	Type  string         `json:"type"` // "generative" or "embed"
	Limit modelsDevLimit `json:"limit"`
}

// modelsDevLimit contains the model's token limits.
type modelsDevLimit struct {
	Context int `json:"context"`
	Output  int `json:"output"`
}

// ModelLimitsSyncService fetches the latest per-model output token limits from
// models.dev and stores them in provider_supported_models.max_output_tokens.
// A daily cron job drives this sync.
type ModelLimitsSyncService struct {
	repo   *Repository
	sched  *scheduler.Scheduler
	client *http.Client
	log    *slog.Logger
}

// NewModelLimitsSyncService creates a ModelLimitsSyncService.
// It registers a daily cron job via the provided scheduler and performs an
// immediate sync at startup so that limit data is available from first run.
func NewModelLimitsSyncService(repo *Repository, sched *scheduler.Scheduler, log *slog.Logger) *ModelLimitsSyncService {
	s := &ModelLimitsSyncService{
		repo:  repo,
		sched: sched,
		client: &http.Client{
			Timeout: modelLimitsFetchTimeout,
		},
		log: log.With(logger.Scope("provider.model_limits_sync")),
	}

	if err := sched.AddCronTask("provider:model_limits:sync", modelLimitsSyncSchedule, func(ctx context.Context) error {
		return s.Sync(ctx)
	}); err != nil {
		log.Warn("failed to register model limits sync cron job", logger.Error(err))
	}

	return s
}

// Sync fetches the latest model output limits from models.dev and upserts them
// into provider_supported_models. Falls back to static values if the fetch fails.
func (s *ModelLimitsSyncService) Sync(ctx context.Context) error {
	limits, err := s.fetchRemoteLimits(ctx)
	if err != nil {
		s.log.Warn("failed to fetch remote model limits, using static fallback",
			logger.Error(err),
		)
		limits = staticModelLimits
	} else if len(limits) == 0 {
		s.log.Warn("remote model limits returned empty map, using static fallback")
		limits = staticModelLimits
	}

	if err := s.repo.UpdateModelOutputLimits(ctx, limits); err != nil {
		return fmt.Errorf("failed to update model output limits: %w", err)
	}

	s.log.Info("model output limits synced", slog.Int("models", len(limits)))
	return nil
}

// fetchRemoteLimits downloads models.dev/api.json and extracts generative model
// output token limits for the "google" provider.
func (s *ModelLimitsSyncService) fetchRemoteLimits(ctx context.Context) (map[string]int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelLimitsFetchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from models.dev", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20)) // 4 MB cap
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var raw modelsDevResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse models.dev JSON: %w", err)
	}

	return parseModelLimits(raw), nil
}

// parseModelLimits extracts generative model output limits from the models.dev
// API response. Only "generative" type models are included; embeddings are
// excluded because their limit.output is a vector dimension, not tokens.
// Model IDs are provider-agnostic (same model name is the same limit across
// google and google-vertex), so we deduplicate by model ID.
func parseModelLimits(raw modelsDevResponse) map[string]int {
	limits := make(map[string]int)
	for _, providerEntry := range raw {
		for modelID, m := range providerEntry.Models {
			if m.Type != "generative" {
				continue
			}
			if modelID == "" || m.Limit.Output <= 0 {
				continue
			}
			limits[modelID] = m.Limit.Output
		}
	}
	return limits
}
