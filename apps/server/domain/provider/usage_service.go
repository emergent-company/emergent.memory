package provider

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/uptrace/bun"
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
	db     bun.IDB
	ch     chan *LLMUsageEvent
	log    *slog.Logger
	doneCh chan struct{} // closed when the worker goroutine exits
}

// NewUsageService creates a UsageService and registers lifecycle hooks with fx.
func NewUsageService(lc fx.Lifecycle, repo *Repository, db bun.IDB, log *slog.Logger) *UsageService {
	s := &UsageService{
		repo:   repo,
		db:     db,
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

// persist calculates estimated cost, inserts the event, then asynchronously
// checks whether a budget alert should be sent.
func (s *UsageService) persist(ctx context.Context, event *LLMUsageEvent) error {
	event.EstimatedCostUSD = s.calculateCost(ctx, event)
	if err := s.repo.InsertUsageEvent(ctx, event); err != nil {
		return err
	}
	// Fire-and-forget budget check so it never blocks the event pipeline.
	go s.checkBudget(context.Background(), event.ProjectID)
	return nil
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

// checkBudget checks whether the project's current-month spend has crossed its
// alert threshold, and if so inserts a warning notification for each project admin.
// It deduplicates by group_key so at most one unread alert exists per project per month.
func (s *UsageService) checkBudget(ctx context.Context, projectID string) {
	// 1. Fetch the project's budget config from kb.projects.
	var budget struct {
		BudgetUSD            *float64 `bun:"budget_usd"`
		BudgetAlertThreshold float64  `bun:"budget_alert_threshold"`
	}
	err := s.db.NewSelect().
		TableExpr("kb.projects").
		ColumnExpr("budget_usd, budget_alert_threshold").
		Where("id = ?", projectID).
		Scan(ctx, &budget)
	if err != nil || budget.BudgetUSD == nil || *budget.BudgetUSD <= 0 {
		// No budget set — nothing to check.
		return
	}

	// 2. Get current-month spend.
	spend, err := s.repo.GetProjectCurrentMonthSpend(ctx, projectID)
	if err != nil {
		s.log.Warn("checkBudget: failed to get current month spend", logger.Error(err), slog.String("projectID", projectID))
		return
	}

	threshold := *budget.BudgetUSD * budget.BudgetAlertThreshold
	if spend < threshold {
		return
	}

	// 3. Deduplicate — skip if an unread alert already exists for this month.
	groupKey := fmt.Sprintf("budget-alert-%s-%s", projectID, time.Now().UTC().Format("2006-01"))
	var existing int
	_ = s.db.NewSelect().
		TableExpr("kb.notifications").
		ColumnExpr("COUNT(*)").
		Where("group_key = ?", groupKey).
		Where("read = false").
		Where("dismissed = false").
		Scan(ctx, &existing)
	if existing > 0 {
		return
	}

	// 4. Look up project admin user IDs.
	var adminIDs []string
	err = s.db.NewSelect().
		TableExpr("kb.project_memberships").
		ColumnExpr("user_id").
		Where("project_id = ?", projectID).
		Where("role = 'project_admin'").
		Scan(ctx, &adminIDs)
	if err != nil || len(adminIDs) == 0 {
		return
	}

	// 5. Insert a notification for each admin.
	pctUsed := (spend / *budget.BudgetUSD) * 100
	title := "Budget alert"
	message := fmt.Sprintf(
		"Project has used $%.2f of its $%.2f monthly budget (%.0f%%).",
		spend, *budget.BudgetUSD, pctUsed,
	)
	sourceType := "project"
	category := "budget"
	severity := "warning"
	importance := "important"

	for _, userID := range adminIDs {
		gk := groupKey
		st := sourceType
		sid := projectID
		cat := category
		n := &budgetNotification{
			ProjectID:  &projectID,
			UserID:     userID,
			Title:      title,
			Message:    message,
			Severity:   severity,
			Importance: importance,
			GroupKey:   &gk,
			SourceType: &st,
			SourceID:   &sid,
			Category:   &cat,
		}
		if _, err := s.db.NewInsert().Model(n).Exec(ctx); err != nil {
			s.log.Warn("checkBudget: failed to insert notification",
				logger.Error(err),
				slog.String("projectID", projectID),
				slog.String("userID", userID),
			)
		}
	}

	s.log.Info("budget alert sent",
		slog.String("projectID", projectID),
		slog.Float64("spend", spend),
		slog.Float64("budget", *budget.BudgetUSD),
		slog.Int("admins", len(adminIDs)),
	)
}

// budgetNotification is a minimal struct for inserting into kb.notifications.
// Using a local struct avoids importing the notifications package and creating
// a circular dependency.
type budgetNotification struct {
	bun.BaseModel `bun:"table:kb.notifications"`

	ProjectID  *string `bun:"project_id,type:uuid"`
	UserID     string  `bun:"user_id,notnull,type:uuid"`
	Title      string  `bun:"title,notnull"`
	Message    string  `bun:"message,notnull"`
	Severity   string  `bun:"severity,notnull"`
	Importance string  `bun:"importance,notnull"`
	GroupKey   *string `bun:"group_key"`
	SourceType *string `bun:"source_type"`
	SourceID   *string `bun:"source_id"`
	Category   *string `bun:"category"`
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
