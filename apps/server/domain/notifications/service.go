package notifications

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/domain/events"
	"github.com/emergent-company/emergent.memory/domain/notifications/taxonomy"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// defaultChannel is the only channel delivered today.
const defaultChannel = "in_app"

// Service handles business logic for notifications
type Service struct {
	repo   *Repository
	events *events.Service
	log    *slog.Logger
}

// NewService creates a new notifications service
func NewService(repo *Repository, eventsSvc *events.Service, log *slog.Logger) *Service {
	return &Service{
		repo:   repo,
		events: eventsSvc,
		log:    log.With(logger.Scope("notifications.svc")),
	}
}

// GetStats returns notification statistics for a user
func (s *Service) GetStats(ctx context.Context, userID string) (*NotificationStats, error) {
	return s.repo.GetStats(ctx, userID)
}

// GetCounts returns notification counts by tab for a user
func (s *Service) GetCounts(ctx context.Context, userID string, params CountParams) (*NotificationCounts, error) {
	return s.repo.GetCounts(ctx, userID, params)
}

// List returns notifications for a user with filters
func (s *Service) List(ctx context.Context, userID string, params ListParams) ([]Notification, error) {
	return s.repo.List(ctx, userID, params)
}

// Create is the single producer entry point. It resolves the event key against
// the taxonomy, applies scope/preference delivery rules and group coalescing,
// inserts the row, and emits a real-time entity event. It returns (nil, nil)
// when the notification is suppressed (opt-out or coalesced).
func (s *Service) Create(ctx context.Context, in CreateInput) (*Notification, error) {
	entry, ok := taxonomy.Lookup(in.EventKey)
	if !ok {
		return nil, apperror.NewValidation("unknown notification event key: " + in.EventKey)
	}

	// The stored scope reflects the requested scope; when absent, fall back to
	// the taxonomy's declared scope.
	scope := in.Scope
	if scope == "" {
		scope = Scope(entry.Scope)
	}

	// Resolve delivery. Only opt-in project events consult preferences.
	prefEnabled := false
	if entry.Delivery == taxonomy.DeliveryOptIn {
		pref, err := s.repo.GetPreference(ctx, in.UserID, in.ProjectID, in.EventKey, defaultChannel)
		if err != nil {
			return nil, err
		}
		if pref != nil {
			prefEnabled = pref.Enabled
		}
	}
	if !taxonomy.ResolveDelivery(entry, prefEnabled) {
		s.log.Debug("notification suppressed by preference",
			slog.String("event_key", in.EventKey),
			slog.String("user_id", in.UserID),
		)
		return nil, nil
	}

	// Group coalescing: a repeated event that already has an unread row for
	// the same group key does not stack another row.
	if in.GroupKey != nil && *in.GroupKey != "" {
		exists, err := s.repo.GroupKeyExists(ctx, in.UserID, *in.GroupKey)
		if err != nil {
			return nil, err
		}
		if exists {
			s.log.Debug("notification coalesced by group key",
				slog.String("event_key", in.EventKey),
				slog.String("group_key", *in.GroupKey),
			)
			return nil, nil
		}
	}

	severity := in.Severity
	if severity == "" {
		severity = "info"
	}
	importance := in.Importance
	if importance == "" {
		importance = "other"
	}

	n := &Notification{
		ProjectID:           in.ProjectID,
		UserID:              in.UserID,
		Title:               in.Title,
		Message:             in.Message,
		Type:                in.Type,
		Severity:            severity,
		Importance:          importance,
		Category:            in.Category,
		SourceType:          in.SourceType,
		SourceID:            in.SourceID,
		RelatedResourceType: in.RelatedResourceType,
		RelatedResourceID:   in.RelatedResourceID,
		ActionURL:           in.ActionURL,
		ActionLabel:         in.ActionLabel,
		Actions:             in.Actions,
		GroupKey:            in.GroupKey,
		ExpiresAt:           in.ExpiresAt,
		Details:             in.Details,
		TaskID:              in.TaskID,
		Scope:               scope,
		RequiresAction:      in.RequiresAction,
		EventKey:            &in.EventKey,
	}

	created, err := s.repo.Create(ctx, n)
	if err != nil {
		return nil, err
	}

	s.emitCreated(created)

	return created, nil
}

// emitCreated publishes a real-time notification entity event.
func (s *Service) emitCreated(n *Notification) {
	if s.events == nil {
		return
	}
	projectID := ""
	if n.ProjectID != nil {
		projectID = *n.ProjectID
	}
	eventKey := ""
	if n.EventKey != nil {
		eventKey = *n.EventKey
	}
	s.events.EmitCreated(events.EntityNotification, n.ID, projectID, &events.EmitOptions{
		Data: map[string]any{
			"userId":   n.UserID,
			"scope":    string(n.Scope),
			"eventKey": eventKey,
		},
	})
}

// MarkRead marks a notification as read
func (s *Service) MarkRead(ctx context.Context, userID, notificationID string) error {
	return s.repo.MarkRead(ctx, userID, notificationID)
}

// MarkUnread marks a notification as unread
func (s *Service) MarkUnread(ctx context.Context, userID, notificationID string) error {
	return s.repo.MarkUnread(ctx, userID, notificationID)
}

// Dismiss dismisses a notification
func (s *Service) Dismiss(ctx context.Context, userID, notificationID string) error {
	return s.repo.Dismiss(ctx, userID, notificationID)
}

// Snooze snoozes a notification until the given time.
func (s *Service) Snooze(ctx context.Context, userID, notificationID string, until time.Time) error {
	return s.repo.Snooze(ctx, userID, notificationID, until)
}

// Unsnooze clears a notification's snooze.
func (s *Service) Unsnooze(ctx context.Context, userID, notificationID string) error {
	return s.repo.Unsnooze(ctx, userID, notificationID)
}

// Clear moves a notification to the cleared tab.
func (s *Service) Clear(ctx context.Context, userID, notificationID string) error {
	return s.repo.Clear(ctx, userID, notificationID)
}

// Restore un-clears a notification.
func (s *Service) Restore(ctx context.Context, userID, notificationID string) error {
	return s.repo.Restore(ctx, userID, notificationID)
}

// ResolveAction records the outcome of an actionable notification.
func (s *Service) ResolveAction(ctx context.Context, userID, notificationID, status string) error {
	return s.repo.ResolveAction(ctx, userID, notificationID, status)
}

// MarkAllRead marks all notifications as read for a user within the given scope.
func (s *Service) MarkAllRead(ctx context.Context, userID string, scope Scope, projectID *string) (int64, error) {
	return s.repo.MarkAllRead(ctx, userID, scope, projectID)
}

// ListEffectivePreferences materialises the effective preference for every
// project event key (account keys are mandatory and not user-configurable, so
// they are omitted). Defaults are project opt-in keys disabled.
func (s *Service) ListEffectivePreferences(ctx context.Context, userID string, projectID *string) ([]PreferenceEntry, error) {
	prefs, err := s.repo.GetPreferences(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}

	stored := make(map[string]bool, len(prefs))
	for _, p := range prefs {
		stored[p.EventKey] = p.Enabled
	}

	out := make([]PreferenceEntry, 0, len(taxonomy.Registry))
	for _, e := range taxonomy.Registry {
		if e.Scope != taxonomy.ScopeProject {
			continue
		}
		enabled := false
		isDefault := true
		if v, ok := stored[e.Key]; ok {
			enabled = v
			isDefault = false
		}
		out = append(out, PreferenceEntry{
			EventKey:       e.Key,
			Scope:          ScopeProject,
			Channel:        defaultChannel,
			Delivery:       string(e.Delivery),
			Required:       e.Delivery == taxonomy.DeliveryRequired,
			RequiresAction: e.RequiresAction,
			Actionable:     e.Actionable,
			Category:       e.Category,
			Enabled:        enabled,
			Default:        isDefault,
		})
	}
	return out, nil
}

// SavePreference stores a delivery preference for a project event key. Account
// keys are mandatory and not user-suppressible, so setting them is a no-op and
// returns (nil, nil).
func (s *Service) SavePreference(ctx context.Context, userID string, projectID *string, eventKey, channel string, enabled bool) (*NotificationPreference, error) {
	entry, ok := taxonomy.Lookup(eventKey)
	if !ok {
		return nil, apperror.NewValidation("unknown notification event key: " + eventKey)
	}
	if entry.Scope == taxonomy.ScopeAccount {
		return nil, nil
	}
	// Preferences are project-scoped: delivery (Create) looks the preference up
	// by the event's project, so a project-scope row stored without a project
	// would never be consulted. Reject it rather than persist an inert row.
	if projectID == nil || strings.TrimSpace(*projectID) == "" {
		return nil, apperror.NewValidation("projectId is required for project-scope notification preferences")
	}
	if channel == "" {
		channel = defaultChannel
	}

	p := &NotificationPreference{
		UserID:    userID,
		ProjectID: projectID,
		EventKey:  eventKey,
		Channel:   channel,
		Enabled:   enabled,
	}
	return s.repo.UpsertPreference(ctx, p)
}
