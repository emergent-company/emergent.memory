package provider

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/uptrace/bun"
	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/domain/email"
	"github.com/emergent-company/emergent.memory/domain/notifications"
	"github.com/emergent-company/emergent.memory/internal/budgetwindow"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

const (
	// usageBufferSize is the maximum number of events buffered before the
	// background writer blocks. Sized to hold ~30 seconds of heavy traffic.
	usageBufferSize = 1024

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

	// notificationsSvc is the central notification producer used for budget
	// alerts. Injected as *notifications.Service — provider → notifications is
	// acyclic (notifications imports events/taxonomy/apperror/logger, none of
	// which import provider), so the leaf-package hand-off D7 warned about is
	// unnecessary. nil is safe: budget alerts are skipped when not wired.
	notificationsSvc *notifications.Service

	// emailSvc delivers the admin budget-alert emails. nil is safe: the in-app
	// notification is the durable record and the mail is best-effort, so a
	// deployment without the email jobs service still alerts in-app.
	emailSvc *email.JobsService

	// frontendURL is the absolute base for the action links in those emails
	// (/o/{org}/p/{project}/usage and .../settings/budget).
	frontendURL string
}

// NewUsageService creates a UsageService and registers lifecycle hooks with fx.
func NewUsageService(lc fx.Lifecycle, repo *Repository, db bun.IDB, notificationsSvc *notifications.Service, emailSvc *email.JobsService, frontendURL string, log *slog.Logger) *UsageService {
	s := &UsageService{
		repo:             repo,
		db:               db,
		ch:               make(chan *LLMUsageEvent, usageBufferSize),
		log:              log.With(logger.Scope("provider.usage")),
		doneCh:           make(chan struct{}),
		notificationsSvc: notificationsSvc,
		emailSvc:         emailSvc,
		frontendURL:      strings.TrimRight(frontendURL, "/"),
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

// pricingLookup abstracts the pricing queries used during cost resolution so
// that calculateCostWith can be tested without a database. *Repository satisfies
// this interface.
type pricingLookup interface {
	GetProjectCustomPricing(ctx context.Context, projectID string, providerSlug string, model string) (*ProjectCustomPricing, error)
	GetPricing(ctx context.Context, provider ProviderType, model string) (*ProviderPricing, error)
	GetPricingByModel(ctx context.Context, model string) (*ProviderPricing, error)
}

// calculateCost resolves pricing via project override → global retail
// (optimistic matching) and returns the estimated cost in USD.
//
// Pricing is per 1 million tokens. Returns 0.0 when no pricing data is found.
func (s *UsageService) calculateCost(ctx context.Context, event *LLMUsageEvent) float64 {
	return s.calculateCostWith(ctx, s.repo, event)
}

// calculateCostWith resolves a usage event's price in order:
//
//  1. Project override for the exact (project_id, provider, model).
//  2. Global retail for the exact (provider, model).
//  3. Global retail by model name only (the model may be served via a
//     different provider, e.g. an OpenAI-compatible/LiteLLM proxy).
//  4. Global retail by a normalized model name (stripVendorModelName first,
//     then normalizeModelName for path prefixes).
//  5. $0 when no match is found.
//
// The first non-nil match SHALL be used. The recorded model name is never
// mutated; normalization only affects the fallback lookup.
func (s *UsageService) calculateCostWith(ctx context.Context, lookup pricingLookup, event *LLMUsageEvent) float64 {
	// 1. Project override (manual rates) — highest precedence. Keyed by the
	// provider instance slug; legacy events without a slug fall back to the
	// dialect (which was the pre-instance identity).
	providerSlug := string(event.ProviderSlug)
	if providerSlug == "" {
		providerSlug = string(event.Provider)
	}
	if pricing, err := lookup.GetProjectCustomPricing(ctx, event.ProjectID, providerSlug, event.Model); err == nil && pricing != nil {
		return computeCost(event, pricing.TextInputPrice, pricing.ImageInputPrice,
			pricing.VideoInputPrice, pricing.AudioInputPrice, pricing.OutputPrice)
	}

	// 2. Global retail — exact provider + model.
	if pricing, err := lookup.GetPricing(ctx, event.Provider, event.Model); err == nil && pricing != nil {
		return computeCost(event, pricing.TextInputPrice, pricing.ImageInputPrice,
			pricing.VideoInputPrice, pricing.AudioInputPrice, pricing.OutputPrice)
	}

	// 3. Global retail — model name only.
	if pricing, err := lookup.GetPricingByModel(ctx, event.Model); err == nil && pricing != nil {
		return computeCost(event, pricing.TextInputPrice, pricing.ImageInputPrice,
			pricing.VideoInputPrice, pricing.AudioInputPrice, pricing.OutputPrice)
	}

	// 4. Global retail — normalized model name (vendor prefixes, :tag/@version).
	if stripped := stripVendorModelName(event.Model); stripped != "" && stripped != event.Model {
		if pricing, err := lookup.GetPricingByModel(ctx, stripped); err == nil && pricing != nil {
			return computeCost(event, pricing.TextInputPrice, pricing.ImageInputPrice,
				pricing.VideoInputPrice, pricing.AudioInputPrice, pricing.OutputPrice)
		}
	}
	if normalized := normalizeModelName(event.Model); normalized != "" && normalized != event.Model {
		if pricing, err := lookup.GetPricingByModel(ctx, normalized); err == nil && pricing != nil {
			return computeCost(event, pricing.TextInputPrice, pricing.ImageInputPrice,
				pricing.VideoInputPrice, pricing.AudioInputPrice, pricing.OutputPrice)
		}
	}

	// 5. No pricing data — log debug and return 0.
	s.log.Debug("no pricing data found for model",
		slog.String("provider", string(event.Provider)),
		slog.String("model", event.Model),
	)
	return 0.0
}

// stripVendorModelName reduces a model name to its bare, vendor-free form for
// optimistic price matching: take the substring after the last '/', strip a
// trailing ':tag' or '@version' suffix, then lower-case and trim.
//
// It is conservative by design — it is applied only for the fallback price
// lookup and never mutates the recorded model name.
func stripVendorModelName(model string) string {
	// Vendor-prefixed names like "deepseek/deepseek-v4-pro" or
	// "azure/gpt-4o": keep only the last path segment.
	if idx := strings.LastIndex(model, "/"); idx != -1 {
		model = model[idx+1:]
	}
	// Strip a trailing ":tag" or "@version" (split on ':' and '@').
	for _, sep := range []string{":", "@"} {
		if idx := strings.Index(model, sep); idx != -1 {
			model = model[:idx]
		}
	}
	return strings.ToLower(strings.TrimSpace(model))
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

// Budget window statuses. "ok" also covers a window with no cap configured, so
// callers never have to distinguish "no cap" from "under the cap" to render a
// badge: they check CapUSD for that.
const (
	budgetStatusOK        = "ok"
	budgetStatusWarning   = "warning"
	budgetStatusExhausted = "exhausted"
)

// BudgetWindowState is one spending window's position: the cap, the threshold,
// the spend and the period it all applies to.
//
// CapUSD and RemainingUSD are omitted for an uncapped window (omitempty on a nil
// pointer): there is no figure to report, and a client that tests the field for
// presence must not read `null` as "cap of zero".
type BudgetWindowState struct {
	Window       budgetwindow.Window `json:"window"`
	CapUSD       *float64            `json:"capUsd,omitempty"`
	Threshold    float64             `json:"threshold"`
	SpendUSD     float64             `json:"spendUsd"`
	RemainingUSD *float64            `json:"remainingUsd,omitempty"`
	PercentUsed  float64             `json:"percentUsed"`
	PeriodStart  time.Time           `json:"periodStart"`
	PeriodEnd    time.Time           `json:"periodEnd"`
	Status       string              `json:"status"`
}

// BudgetState is a project's budget position across both windows. Both windows
// are always present — a window with no cap still reports its period (so the UI
// can say when it would roll) and status "ok"; check CapUSD to tell them apart.
type BudgetState struct {
	Week  *BudgetWindowState `json:"week"`
	Month *BudgetWindowState `json:"month"`
}

// budgetConfig is the budget configuration as stored on kb.projects.
type budgetConfig struct {
	Name             string
	OrgID            string
	MonthlyCapUSD    *float64
	MonthlyThreshold float64
	WeeklyCapUSD     *float64
	WeeklyThreshold  float64
	WeekAnchor       *time.Time
	MonthAnchor      *time.Time
}

// budgetAdmin is a project admin with the address budget alerts are mailed to.
type budgetAdmin struct {
	UserID string `bun:"user_id"`
	Email  string `bun:"email"`
}

// budgetConfigFor reads the project's budget configuration, name and owning org.
func (s *UsageService) budgetConfigFor(ctx context.Context, projectID string) (*budgetConfig, error) {
	var row struct {
		Name             string     `bun:"name"`
		OrgID            string     `bun:"organization_id"`
		MonthlyCapUSD    *float64   `bun:"budget_usd"`
		MonthlyThreshold float64    `bun:"budget_alert_threshold"`
		WeeklyCapUSD     *float64   `bun:"budget_weekly_usd"`
		WeeklyThreshold  float64    `bun:"budget_weekly_alert_threshold"`
		WeekAnchor       *time.Time `bun:"budget_week_anchor"`
		MonthAnchor      *time.Time `bun:"budget_month_anchor"`
	}
	err := s.db.NewSelect().
		TableExpr("kb.projects").
		Column("name", "organization_id", "budget_usd", "budget_alert_threshold",
			"budget_weekly_usd", "budget_weekly_alert_threshold",
			"budget_week_anchor", "budget_month_anchor").
		Where("id = ?", projectID).
		Scan(ctx, &row)
	if err != nil {
		return nil, err
	}
	return &budgetConfig{
		Name:             row.Name,
		OrgID:            row.OrgID,
		MonthlyCapUSD:    row.MonthlyCapUSD,
		MonthlyThreshold: row.MonthlyThreshold,
		WeeklyCapUSD:     row.WeeklyCapUSD,
		WeeklyThreshold:  row.WeeklyThreshold,
		WeekAnchor:       row.WeekAnchor,
		MonthAnchor:      row.MonthAnchor,
	}, nil
}

// GetBudgetState returns the project's position in both spending windows.
//
// It is the single source of truth for everything that reacts to spend: the
// usage page, the sidebar badge, the alert producer and CheckBudgetExceeded all
// read it, so a window boundary or a percentage cannot differ between what the
// UI shows, what the email says and what blocks an agent run.
func (s *UsageService) GetBudgetState(ctx context.Context, projectID string) (*BudgetState, error) {
	cfg, err := s.budgetConfigFor(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("GetBudgetState: read budget config: %w", err)
	}
	return s.budgetStateForConfig(ctx, cfg, projectID, time.Now().UTC())
}

// budgetStateForConfig computes the state at a given instant. Split from
// GetBudgetState so DB tests can pin the clock.
func (s *UsageService) budgetStateForConfig(ctx context.Context, cfg *budgetConfig, projectID string, now time.Time) (*BudgetState, error) {
	weekPeriod := budgetwindow.PeriodFor(now, cfg.WeekAnchor, budgetwindow.Week)
	monthPeriod := budgetwindow.PeriodFor(now, cfg.MonthAnchor, budgetwindow.Month)

	weekSpend, monthSpend, err := s.repo.GetProjectWindowSpend(ctx, projectID, weekPeriod.Start, monthPeriod.Start)
	if err != nil {
		return nil, err
	}

	return &BudgetState{
		Week:  newBudgetWindowState(budgetwindow.Week, cfg.WeeklyCapUSD, cfg.WeeklyThreshold, weekSpend, weekPeriod),
		Month: newBudgetWindowState(budgetwindow.Month, cfg.MonthlyCapUSD, cfg.MonthlyThreshold, monthSpend, monthPeriod),
	}, nil
}

// capConfigured reports whether a cap column means "there is a limit". A nil or
// non-positive cap is uncapped, matching newBudgetWindowState's rule.
func capConfigured(capUSD *float64) bool {
	return capUSD != nil && *capUSD > 0
}

// newBudgetWindowState classifies one window. A nil or non-positive cap is "no
// limit": the state keeps the period (so the UI can still say when it rolls)
// but reports no cap, no remaining and "ok".
func newBudgetWindowState(
	w budgetwindow.Window,
	capUSD *float64,
	threshold float64,
	spend float64,
	period budgetwindow.Period,
) *BudgetWindowState {
	st := &BudgetWindowState{
		Window:      w,
		Threshold:   threshold,
		SpendUSD:    spend,
		PeriodStart: period.Start,
		PeriodEnd:   period.End,
		Status:      budgetStatusOK,
	}
	if !capConfigured(capUSD) {
		return st
	}
	st.CapUSD = capUSD
	remaining := *capUSD - spend
	if remaining < 0 {
		remaining = 0
	}
	st.RemainingUSD = &remaining
	st.PercentUsed = (spend / *capUSD) * 100

	switch {
	case spend >= *capUSD:
		st.Status = budgetStatusExhausted
	case threshold > 0 && spend >= *capUSD*threshold:
		st.Status = budgetStatusWarning
	}
	return st
}

// budgetStateExhausted reports whether either window has hit its cap.
func budgetStateExhausted(state *BudgetState) bool {
	if state == nil {
		return false
	}
	for _, w := range []*BudgetWindowState{state.Week, state.Month} {
		if w != nil && w.Status == budgetStatusExhausted {
			return true
		}
	}
	return false
}

// checkBudget emits the budget alerts for both windows.
//
// Each window has two levels — a warning at its configured threshold and a
// hard alert at the cap — and each level is emitted at most once per period.
// Deduplication is the notifications producer's durable (user, group_key)
// unique index, exactly as before; that atomic gate is also the email gate:
// only a newly created notification sends a letter, so the retry/re-evaluation
// loop can never mail an admin twice for the same period.
func (s *UsageService) checkBudget(ctx context.Context, projectID string) {
	cfg, err := s.budgetConfigFor(ctx, projectID)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			s.log.Warn("checkBudget: failed to read budget config", logger.Error(err), slog.String("projectID", projectID))
		}
		return
	}

	now := time.Now().UTC()
	// No cap in either window means there is nothing to alert on: short-circuit
	// before the spend aggregate. This runs on every usage write, and the
	// pre-window implementation returned before its month SUM in exactly this
	// case — an uncapped project must not pay an aggregate over
	// kb.llm_usage_events per event.
	if !capConfigured(cfg.WeeklyCapUSD) && !capConfigured(cfg.MonthlyCapUSD) {
		return
	}

	state, err := s.budgetStateForConfig(ctx, cfg, projectID, now)
	if err != nil {
		s.log.Warn("checkBudget: failed to compute budget state", logger.Error(err), slog.String("projectID", projectID))
		return
	}

	var admins []budgetAdmin
	adminsLoaded := false

	for _, w := range []*BudgetWindowState{state.Week, state.Month} {
		if w == nil || !capConfigured(w.CapUSD) {
			continue
		}
		if !adminsLoaded {
			admins, err = s.budgetAdmins(ctx, projectID)
			if err != nil {
				// Fail quiet: the spend check must never break a usage write.
				// The next evaluation retries; the in-app notification is the
				// durable record and it, too, is only attempted on success.
				s.log.Warn("checkBudget: failed to list project admins",
					logger.Error(err), slog.String("projectID", projectID))
				return
			}
			if len(admins) == 0 {
				// No project admin to tell — the recipient set is per project,
				// so a further window has nobody to notify either.
				return
			}
			adminsLoaded = true
		}
		// A threshold of 1.0 is the cap itself: sending the warning and the
		// exhaustion alert at the same instant would be two identical emails.
		if w.Status == budgetStatusWarning && w.Threshold < 1 {
			s.emitBudgetAlert(ctx, cfg, projectID, w, false, admins)
		}
		if w.Status == budgetStatusExhausted {
			s.emitBudgetAlert(ctx, cfg, projectID, w, true, admins)
		}
	}
}

// budgetAdmins returns the project's admins with their primary *verified*
// address. An unverified address is never mailed: this is an unsolicited
// automated email, and the address on an unverified row may belong to someone
// else (the same rule userprofile.Repository.GetEmail applies when it resolves
// "the" address for a user). An admin with no verified address still receives
// the in-app notification, which is the durable record; email is best-effort.
func (s *UsageService) budgetAdmins(ctx context.Context, projectID string) ([]budgetAdmin, error) {
	var rows []budgetAdmin
	err := s.db.NewSelect().
		TableExpr("kb.project_memberships AS pm").
		ColumnExpr("pm.user_id AS user_id").
		ColumnExpr("COALESCE(ue.email, '') AS email").
		Join(`LEFT JOIN LATERAL (
			SELECT email FROM core.user_emails
			WHERE user_id = pm.user_id AND verified
			ORDER BY created_at ASC
			LIMIT 1
		) AS ue ON true`).
		Where("pm.project_id = ?", projectID).
		Where("pm.role = 'project_admin'").
		Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// budgetAlertGroupKey is the durable deduplication key for one alert level of
// one window period.
//
// Monthly keys keep the pre-existing YYYY-MM format for calendar projects: those
// notifications already exist in production, and switching the format on
// rollout would re-alert every project that is already over its threshold. A
// project that has reset its month uses its anchor date instead, so a new cycle
// can alert again.
func budgetAlertGroupKey(projectID string, cfg *budgetConfig, w *BudgetWindowState, exhausted bool) string {
	period := budgetwindow.Period{Start: w.PeriodStart, End: w.PeriodEnd}
	if w.Window == budgetwindow.Week {
		kind := "budget-week-alert"
		if exhausted {
			kind = "budget-week-exceeded"
		}
		return fmt.Sprintf("%s-%s-%s", kind, projectID, period.Key())
	}

	monthKey := budgetwindow.CalendarKeyMonth(period)
	if cfg.MonthAnchor != nil {
		monthKey = period.Key()
	}
	kind := "budget-alert"
	if exhausted {
		kind = "budget-exceeded"
	}
	return fmt.Sprintf("%s-%s-%s", kind, projectID, monthKey)
}

// emitBudgetAlert writes the in-app notification for every admin and, when the
// notification is newly created, one email.
func (s *UsageService) emitBudgetAlert(
	ctx context.Context,
	cfg *budgetConfig,
	projectID string,
	w *BudgetWindowState,
	exhausted bool,
	admins []budgetAdmin,
) {
	windowLabel := "monthly"
	if w.Window == budgetwindow.Week {
		windowLabel = "weekly"
	}
	levelLabel := "alert"
	if exhausted {
		levelLabel = "exhausted"
	}

	remaining := 0.0
	if w.RemainingUSD != nil {
		remaining = *w.RemainingUSD
	}
	resets := w.PeriodEnd.UTC().Format("Mon, Jan 2")

	title := fmt.Sprintf("%s budget %s", strings.ToUpper(windowLabel[:1])+windowLabel[1:], levelLabel)
	message := fmt.Sprintf(
		"%s has used $%.2f of its $%.2f %s budget (%.0f%%). $%.2f left — resets %s.",
		cfg.Name, w.SpendUSD, *w.CapUSD, windowLabel, w.PercentUsed, remaining, resets,
	)
	if exhausted {
		message += " Agent runs are rejected until the period resets."
	}

	severity := "warning"
	importance := "important"
	if exhausted {
		severity = "critical"
		importance = "urgent"
	}

	usageURL := s.frontendURL + "/o/" + cfg.OrgID + "/p/" + projectID + "/usage"
	groupKey := budgetAlertGroupKey(projectID, cfg, w, exhausted)
	sourceType := "project"
	category := "budget"
	actionLabel := "View usage"

	if s.notificationsSvc == nil {
		return
	}

	for _, admin := range admins {
		gk := groupKey
		st := sourceType
		sid := projectID
		cat := category
		au := usageURL
		al := actionLabel

		created, err := s.notificationsSvc.Create(ctx, notifications.CreateInput{
			UserID:      admin.UserID,
			ProjectID:   &projectID,
			Scope:       notifications.ScopeAccount,
			EventKey:    "account.usage.budget_alert",
			Title:       title,
			Message:     message,
			Severity:    severity,
			Importance:  importance,
			GroupKey:    &gk,
			SourceType:  &st,
			SourceID:    &sid,
			Category:    &cat,
			ActionURL:   &au,
			ActionLabel: &al,
		})
		if err != nil {
			s.log.Warn("budget alert: failed to create notification",
				logger.Error(err),
				slog.String("projectID", projectID),
				slog.String("userID", admin.UserID),
			)
			continue
		}
		// coalesced: this admin already has this alert for this period, so the
		// email was sent (or attempted) with it.
		if created == nil {
			continue
		}
		s.sendBudgetAlertEmail(ctx, cfg, projectID, w, admin, windowLabel, levelLabel, exhausted, remaining, resets, usageURL)
	}

	s.log.Info("budget alert emitted",
		slog.String("projectID", projectID),
		slog.String("window", string(w.Window)),
		slog.String("level", levelLabel),
		slog.Float64("spend", w.SpendUSD),
		slog.Float64("cap", *w.CapUSD),
		slog.Int("admins", len(admins)),
	)
}

// sendBudgetAlertEmail enqueues the admin email for one alert. A nil email
// service (or an admin without an address) is not an error: the in-app
// notification is the durable record, and the mail is best-effort.
func (s *UsageService) sendBudgetAlertEmail(
	ctx context.Context,
	cfg *budgetConfig,
	projectID string,
	w *BudgetWindowState,
	admin budgetAdmin,
	windowLabel, levelLabel string,
	exhausted bool,
	remaining float64,
	resets, usageURL string,
) {
	if s.emailSvc == nil || admin.Email == "" {
		return
	}

	subject := fmt.Sprintf("%s budget %s — %s", strings.ToUpper(windowLabel[:1])+windowLabel[1:], levelLabel, cfg.Name)
	period := budgetwindow.Period{Start: w.PeriodStart, End: w.PeriodEnd}
	sourceType := "budget_alert"
	sourceID := fmt.Sprintf("%s:%s:%s:%s", projectID, admin.UserID, w.Window, period.Key())
	toName := admin.Email
	budgetURL := s.frontendURL + "/o/" + cfg.OrgID + "/p/" + projectID + "/settings/budget"

	_, err := s.emailSvc.Enqueue(ctx, email.EnqueueOptions{
		ProjectID:    &projectID,
		TemplateName: "budget-alert",
		ToEmail:      admin.Email,
		ToName:       &toName,
		Subject:      subject,
		TemplateData: map[string]interface{}{
			"projectName":      cfg.Name,
			"windowLabel":      windowLabel,
			"levelLabel":       levelLabel,
			"exhausted":        exhausted,
			"spend":            fmt.Sprintf("%.2f", w.SpendUSD),
			"cap":              fmt.Sprintf("%.2f", *w.CapUSD),
			"remaining":        fmt.Sprintf("%.2f", remaining),
			"percent":          fmt.Sprintf("%.0f", w.PercentUsed),
			"thresholdPercent": fmt.Sprintf("%.0f", w.Threshold*100),
			"resetsOn":         resets,
			"usageUrl":         usageURL,
			"budgetUrl":        budgetURL,
			"plainText": email.BudgetAlertPlainText(email.BudgetAlertData{
				ProjectName:      cfg.Name,
				WindowLabel:      windowLabel,
				Exhausted:        exhausted,
				SpendUSD:         w.SpendUSD,
				CapUSD:           *w.CapUSD,
				RemainingUSD:     remaining,
				PercentUsed:      w.PercentUsed,
				ThresholdPercent: w.Threshold * 100,
				ResetsOn:         resets,
				UsageURL:         usageURL,
				BudgetURL:        budgetURL,
			}),
		},
		SourceType: &sourceType,
		SourceID:   &sourceID,
	})
	if err != nil {
		s.log.Warn("budget alert: failed to enqueue admin email",
			logger.Error(err),
			slog.String("projectID", projectID),
			slog.String("userID", admin.UserID),
		)
	}
}

// CheckBudgetExceeded returns true when either of the project's spending
// windows has reached its cap. No cap configured anywhere returns false. If any
// query fails it returns false so that callers fail-open and do not block agent
// execution on a broken budget query.
func (s *UsageService) CheckBudgetExceeded(ctx context.Context, projectID string) (bool, error) {
	state, err := s.GetBudgetState(ctx, projectID)
	if err != nil {
		return false, fmt.Errorf("CheckBudgetExceeded: %w", err)
	}
	return budgetStateExhausted(state), nil
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
