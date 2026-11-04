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
	n, err := s.prepareNotification(ctx, in)
	if err != nil || n == nil {
		return nil, err
	}

	// Group coalescing: when a group key is set, the insert is made atomic by
	// the partial unique index and ON CONFLICT DO NOTHING, so a repeated event
	// cannot stack duplicate rows even under concurrent producers.
	var created *Notification
	if in.GroupKey != nil && *in.GroupKey != "" {
		created, err = s.repo.CreateCoalescing(ctx, n)
	} else {
		created, err = s.repo.Create(ctx, n)
	}
	if err != nil {
		return nil, err
	}
	if created == nil {
		s.log.Debug("notification coalesced by group key",
			slog.String("event_key", in.EventKey),
			slog.String("group_key", *in.GroupKey),
		)
		return nil, nil
	}

	s.emitCreated(ctx, created)

	return created, nil
}

// CreateOrReopen is Create for a producer whose event has to be actionable on
// every occurrence. Where Create coalesces a repeated event away, this reopens
// the unresolved row that occupies the same group key — refreshing its payload
// and clearing its resolution, snooze and read mark — so the member sees the new
// episode instead of the previous episode's silence (#2414). A cleared or
// dismissed row is off the partial unique index, so it is never resurrected: the
// insert creates a new row.
//
// It returns the stored row and whether it was inserted (true) or reopened
// (false).
func (s *Service) CreateOrReopen(ctx context.Context, in CreateInput) (*Notification, bool, error) {
	n, err := s.prepareNotification(ctx, in)
	if err != nil || n == nil {
		return nil, false, err
	}
	row, created, err := s.repo.CreateOrReopen(ctx, n)
	if err != nil {
		return nil, false, err
	}
	s.emitCreated(ctx, row)
	return row, created, nil
}

// prepareNotification resolves the taxonomy entry, delivery rules and row for a
// producer input. A nil notification means the member's preference suppressed
// it, which both Create and CreateOrReopen report as (nil, nil).
func (s *Service) prepareNotification(ctx context.Context, in CreateInput) (*Notification, error) {
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

	// Classification is taxonomy-authoritative (issue #1364).
	requiresAction := resolveRequiresAction(in.RequiresAction, entry)

	// Resolve delivery. Only the preference-driven classes consult the stored
	// preference; mandatory and required events are delivered unconditionally.
	// The tri-state (nil = no row) is what lets opt-out events deliver by
	// default while still honouring an explicit disable.
	var pref *bool
	if entry.Delivery == taxonomy.DeliveryOptIn || entry.Delivery == taxonomy.DeliveryOptOut {
		stored, err := s.repo.GetPreference(ctx, in.UserID, in.ProjectID, in.EventKey, defaultChannel)
		if err != nil {
			return nil, err
		}
		if stored != nil {
			enabled := stored.Enabled
			pref = &enabled
		}
	}
	if !taxonomy.ResolveDelivery(entry, pref) {
		s.log.Debug("notification suppressed by preference",
			slog.String("event_key", in.EventKey),
			slog.String("user_id", in.UserID),
		)
		return nil, nil
	}

	return newNotificationRow(in, entry, scope, requiresAction), nil
}

// newNotificationRow materialises the row a producer describes, with the
// taxonomy as the authority for category and actionability.
func newNotificationRow(in CreateInput, entry taxonomy.Entry, scope Scope, requiresAction bool) *Notification {
	severity := in.Severity
	if severity == "" {
		severity = "info"
	}
	importance := in.Importance
	if importance == "" {
		importance = "other"
	}
	return &Notification{
		ProjectID:           in.ProjectID,
		UserID:              in.UserID,
		Title:               in.Title,
		Message:             in.Message,
		Type:                in.Type,
		Severity:            severity,
		Importance:          importance,
		Category:            resolveCategory(in.Category, entry),
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
		RequiresAction:      requiresAction,
		EventKey:            &in.EventKey,
	}
}

// resolveRequiresAction decides whether a created notification needs the user
// to act. The taxonomy is authoritative for the event type, so an action-required
// type can never be emitted as an FYI item by a producer that forgot the flag
// (issue #1364). OR-ing rather than overriding keeps a producer that legitimately
// marks a not-yet-registered actionable event from silently losing that flag.
func resolveRequiresAction(producer bool, entry taxonomy.Entry) bool {
	return producer || entry.RequiresAction
}

// resolveCategory keeps the stored category consistent with the taxonomy when a
// producer omits it. A producer-supplied category wins (it may deliberately
// narrow), but an omitted one falls back to the taxonomy entry's category so a
// row is never left NULL for a type the taxonomy classifies (issue #1375).
func resolveCategory(producer *string, entry taxonomy.Entry) *string {
	if producer != nil && *producer != "" {
		return producer
	}
	if entry.Category == "" {
		return producer
	}
	category := entry.Category
	return &category
}

// emitCreated publishes a real-time notification entity event.
func (s *Service) emitCreated(ctx context.Context, n *Notification) {
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
	s.events.EmitCreated(ctx, events.EntityNotification, n.ID, projectID, &events.EmitOptions{
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
// they are omitted). An unconfigured key reports the default the delivery
// resolver actually applies: on for opt-out keys, off for opt-in keys.
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
		enabled := taxonomy.DefaultEnabled(e)
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

// FanoutOptions tunes a project fan-out.
type FanoutOptions struct {
	// ActorUserID is the user whose own action produced the event (the
	// uploader, the extraction trigger). They are skipped entirely: a member is
	// never notified about something they just did. Exclusion happens at write
	// time rather than read time so the actor carries no unread row into any
	// client or badge. Failure events deliberately do not set this.
	ActorUserID string
}

// CreateForProjectMembers fans a single project event out to one notification
// per project member. A member whose preference suppresses the event (or who
// has no member row) is silently skipped by Create, and the optional actor is
// skipped before anything is written. Per-member failures are logged and do not
// abort the fan-out, so one member never blocks the rest.
func (s *Service) CreateForProjectMembers(ctx context.Context, projectID string, in CreateInput, opts ...FanoutOptions) error {
	actor := ""
	for _, o := range opts {
		if o.ActorUserID != "" {
			actor = o.ActorUserID
		}
	}
	members, err := s.repo.ListProjectMemberUserIDs(ctx, projectID)
	if err != nil {
		return err
	}
	in.ProjectID = &projectID
	in.Scope = ScopeProject
	for _, memberID := range members {
		if actor != "" && memberID == actor {
			continue
		}
		in.UserID = memberID
		if _, err := s.Create(ctx, in); err != nil {
			s.log.Warn("failed to create notification for project member",
				slog.String("event_key", in.EventKey),
				slog.String("user_id", memberID),
				logger.Error(err))
		}
	}
	return nil
}

// CreateOrReopenForProjectMembers fans CreateOrReopen out to every project
// member. It sits beside CreateForProjectMembers deliberately: reopening an item
// a member had resolved is a different decision from coalescing a duplicate, so
// the two producers do not share an entry point.
func (s *Service) CreateOrReopenForProjectMembers(ctx context.Context, projectID string, in CreateInput) error {
	members, err := s.repo.ListProjectMemberUserIDs(ctx, projectID)
	if err != nil {
		return err
	}
	in.ProjectID = &projectID
	in.Scope = ScopeProject
	for _, memberID := range members {
		in.UserID = memberID
		if _, _, err := s.CreateOrReopen(ctx, in); err != nil {
			s.log.Warn("failed to create or reopen notification for project member",
				slog.String("event_key", in.EventKey),
				slog.String("user_id", memberID),
				logger.Error(err))
		}
	}
	return nil
}

// ResolveByBranch marks review-ready notifications referencing a staging branch
// as resolved once that branch is merged. It satisfies graph's
// BranchNotificationResolver interface.
func (s *Service) ResolveByBranch(ctx context.Context, projectID, branchID string) error {
	return s.repo.ResolveByBranch(ctx, projectID, branchID)
}

// ResolveResource marks actionable notifications referencing a resource as
// resolved, returning how many were affected. The update is scoped to the
// project so a resolve-resource call cannot resolve another project's
// notifications.
func (s *Service) ResolveResource(ctx context.Context, projectID, resourceType, resourceID string) (int64, error) {
	return s.repo.ResolveResource(ctx, projectID, resourceType, resourceID)
}
