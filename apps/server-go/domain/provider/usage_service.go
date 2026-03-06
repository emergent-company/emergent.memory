package provider

import (
	"context"
	"log/slog"
	"time"

	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/pkg/logger"
)

const (
	// usageBufferSize is the maximum number of events buffered before the
	// background writer blocks. Sized to hold ~30 seconds of heavy traffic.
	usageBufferSize = 1024

	// usageFlushTimeout is how long the shutdown waits for the buffer to drain.
	usageFlushTimeout = 10 * time.Second

	// pricePerMillionTokens is the divisor for per-token cost calculation
	// (all prices in the DB are stored per 1M tokens).
	pricePerMillionTokens = 1_000_000.0
)

// UsageService records LLM usage events asynchronously.
// Events are dispatched via a buffered channel; a background goroutine
// persists them to the database with estimated cost calculation.
type UsageService struct {
	repo   *Repository
	ch     chan *LLMUsageEvent
	log    *slog.Logger
	doneCh chan struct{} // closed when the worker goroutine exits
}

// NewUsageService creates a UsageService and registers lifecycle hooks with fx.
func NewUsageService(lc fx.Lifecycle, repo *Repository, log *slog.Logger) *UsageService {
	s := &UsageService{
		repo:   repo,
		ch:     make(chan *LLMUsageEvent, usageBufferSize),
		log:    log.With(logger.Scope("provider.usage")),
		doneCh: make(chan struct{}),
	}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			go s.worker()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			return s.shutdown(ctx)
		},
	})

	return s
}

// RecordAsync enqueues an LLMUsageEvent for asynchronous persistence.
// Non-blocking: if the buffer is full, the event is dropped with a warning.
func (s *UsageService) RecordAsync(event *LLMUsageEvent) {
	select {
	case s.ch <- event:
	default:
		s.log.Warn("usage event buffer full, dropping event",
			slog.String("project", event.ProjectID),
			slog.String("model", event.Model),
		)
	}
}

// worker drains the event channel and persists events to the database.
func (s *UsageService) worker() {
	defer close(s.doneCh)

	for event := range s.ch {
		if err := s.persist(context.Background(), event); err != nil {
			s.log.Error("failed to persist usage event",
				logger.Error(err),
				slog.String("project", event.ProjectID),
				slog.String("model", event.Model),
			)
		}
	}
}

// persist calculates estimated cost and inserts the event into the database.
func (s *UsageService) persist(ctx context.Context, event *LLMUsageEvent) error {
	event.EstimatedCostUSD = s.calculateCost(ctx, event)
	return s.repo.InsertUsageEvent(ctx, event)
}

// calculateCost resolves pricing via org custom rates → global retail rates
// and returns the estimated cost in USD.
//
// Pricing is per 1 million tokens. Returns 0.0 when no pricing data is found.
func (s *UsageService) calculateCost(ctx context.Context, event *LLMUsageEvent) float64 {
	// Try org custom pricing first (enterprise negotiated rates)
	pricing, err := s.repo.GetOrgCustomPricing(ctx, event.OrgID, event.Provider, event.Model)
	if err != nil || pricing == nil {
		// Fall back to global retail pricing
		global, err := s.repo.GetPricing(ctx, event.Provider, event.Model)
		if err != nil || global == nil {
			// No pricing data — log debug and return 0
			s.log.Debug("no pricing data found for model",
				slog.String("provider", string(event.Provider)),
				slog.String("model", event.Model),
			)
			return 0.0
		}
		return computeCost(event, global.TextInputPrice, global.ImageInputPrice,
			global.VideoInputPrice, global.AudioInputPrice, global.OutputPrice)
	}

	return computeCost(event, pricing.TextInputPrice, pricing.ImageInputPrice,
		pricing.VideoInputPrice, pricing.AudioInputPrice, pricing.OutputPrice)
}

// computeCost multiplies per-modality token counts by their respective prices.
// All prices are per 1M tokens.
func computeCost(
	event *LLMUsageEvent,
	textInputPrice, imageInputPrice, videoInputPrice, audioInputPrice, outputPrice float64,
) float64 {
	cost := float64(event.TextInputTokens)*textInputPrice/pricePerMillionTokens +
		float64(event.ImageInputTokens)*imageInputPrice/pricePerMillionTokens +
		float64(event.VideoInputTokens)*videoInputPrice/pricePerMillionTokens +
		float64(event.AudioInputTokens)*audioInputPrice/pricePerMillionTokens +
		float64(event.OutputTokens)*outputPrice/pricePerMillionTokens
	return cost
}

// shutdown closes the event channel and waits for the worker to drain.
func (s *UsageService) shutdown(ctx context.Context) error {
	close(s.ch)

	select {
	case <-s.doneCh:
		return nil
	case <-ctx.Done():
		s.log.Warn("usage service shutdown timed out; some events may be lost")
		return nil
	}
}
