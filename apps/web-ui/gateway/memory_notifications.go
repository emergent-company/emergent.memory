package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
)

// --- notifications (inbox subsystem) ---
//
// Thin client for memory's notification API (apps/server/domain/notifications).
// Every call rides the request's session context: the bearer token and the
// X-Project-ID/X-Org-ID scoping headers are attached by doOnce from the session
// context, never from process-global state. The scope query param selects the
// account vs project inbox; the server resolves the effective tab/lifecycle.
//
// Wire-shape note: memory wraps notification collections/counts in a
// {"data": …} envelope, but the decode paths below tolerate a bare array/object
// too, so a future envelope change does not silently yield an empty inbox.

// NotificationAction is one inline action on an actionable notification
// (invitation accept/decline, approval approve/deny, comment jump).
//
// Canonical wire shape (memory's `actions` jsonb, emitted by invites and the
// ask_user tool): `{"label": string, "value": string}`. `label` is the button
// text; `value` is the verb/outcome POSTed back to POST /:id/resolve. The
// id/url/tone fields are reserved for richer action payloads and are currently
// unset by producers.
type NotificationAction struct {
	ID    string `json:"id,omitempty"`
	Label string `json:"label"`
	Value string `json:"value"`
	URL   string `json:"url,omitempty"`
	Tone  string `json:"tone,omitempty"`
}

// Notification is one inbox item from GET /api/notifications. Nullable fields
// degrade to zero values; times stay as raw RFC3339 strings (the UI renders
// them, it does not compare them).
type Notification struct {
	ID     string `json:"id"`
	UserID string `json:"userId,omitempty"`
	Scope  string `json:"scope"`
	// ProjectID is set for project-scope notifications and for account events
	// that link to a project (e.g. "added to project").
	ProjectID  string `json:"projectId,omitempty"`
	Title      string `json:"title"`
	Message    string `json:"message,omitempty"`
	Type       string `json:"type,omitempty"`
	Category   string `json:"category,omitempty"`
	EventKey   string `json:"eventKey,omitempty"`
	Severity   string `json:"severity,omitempty"`
	Importance string `json:"importance,omitempty"`
	// RequiresAction drives the inline controls and the "Action required" tab.
	RequiresAction bool                 `json:"requiresAction"`
	Read           bool                 `json:"read"`
	ReadAt         string               `json:"readAt,omitempty"`
	Dismissed      bool                 `json:"dismissed,omitempty"`
	ClearedAt      string               `json:"clearedAt,omitempty"`
	SnoozedUntil   string               `json:"snoozedUntil,omitempty"`
	Actions        []NotificationAction `json:"actions,omitempty"`
	ActionStatus   string               `json:"actionStatus,omitempty"`
	ActionURL      string               `json:"actionUrl,omitempty"`
	ActionLabel    string               `json:"actionLabel,omitempty"`
	TaskID         string               `json:"taskId,omitempty"`
	SourceType     string               `json:"sourceType,omitempty"`
	SourceID       string               `json:"sourceId,omitempty"`
	GroupKey       string               `json:"groupKey,omitempty"`
	CreatedAt      string               `json:"createdAt,omitempty"`
	UpdatedAt      string               `json:"updatedAt,omitempty"`
}

// NotificationCounts is the per-tab count summary for one inbox scope
// (GET /api/notifications/counts). RequiresAction is best-effort: the server
// may not send it, in which case the "Action required" tab renders without a
// count rather than a misleading zero.
type NotificationCounts struct {
	All            int `json:"all"`
	Important      int `json:"important"`
	Other          int `json:"other"`
	Snoozed        int `json:"snoozed"`
	Cleared        int `json:"cleared"`
	Unread         int `json:"unread"`
	RequiresAction int `json:"requiresAction"`
}

// NotificationPreference is one effective project-event preference
// (GET /api/notifications/preferences). Required keys are always delivered and
// render as a locked "always on" toggle.
type NotificationPreference struct {
	EventKey    string `json:"eventKey"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
	Category    string `json:"category,omitempty"`
	Scope       string `json:"scope,omitempty"`
	Channel     string `json:"channel,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Enabled     bool   `json:"enabled"`
}

// NotificationListParams is the filter set for GET /api/notifications.
type NotificationListParams struct {
	Scope          string // account|project
	ProjectID      string // project-scope inbox; empty for account
	Tab            string // all|important|other|snoozed|cleared
	Category       string
	UnreadOnly     bool
	RequiresAction bool
	Search         string
}

// encode builds the query string, omitting empty/false values so the request
// URL is stable and legible.
func (p NotificationListParams) encode() string {
	q := url.Values{}
	if p.Scope != "" {
		q.Set("scope", p.Scope)
	}
	if p.ProjectID != "" {
		q.Set("project_id", p.ProjectID)
	}
	if p.Tab != "" {
		q.Set("tab", p.Tab)
	}
	if p.Category != "" {
		q.Set("category", p.Category)
	}
	if p.UnreadOnly {
		q.Set("unread_only", "true")
	}
	if p.RequiresAction {
		q.Set("requires_action", "true")
	}
	if p.Search != "" {
		q.Set("search", p.Search)
	}
	return q.Encode()
}

// ListNotifications fetches the inbox list for one scope/filter set.
func (m *MemoryClient) ListNotifications(ctx context.Context, p NotificationListParams) ([]Notification, error) {
	var body json.RawMessage
	if err := m.do(ctx, http.MethodGet, "/api/notifications?"+p.encode(), nil, &body); err != nil {
		return nil, err
	}
	return decodeNotificationList(body), nil
}

// NotificationCounts fetches the per-tab counts for one scope.
func (m *MemoryClient) NotificationCounts(ctx context.Context, scope, projectID string) (*NotificationCounts, error) {
	q := url.Values{}
	if scope != "" {
		q.Set("scope", scope)
	}
	if projectID != "" {
		q.Set("project_id", projectID)
	}
	var body json.RawMessage
	if err := m.do(ctx, http.MethodGet, "/api/notifications/counts?"+q.Encode(), nil, &body); err != nil {
		return nil, err
	}
	return decodeNotificationCounts(body), nil
}

// ListNotificationPreferences fetches the user's effective project-event
// preferences for one project.
func (m *MemoryClient) ListNotificationPreferences(ctx context.Context, projectID string) ([]NotificationPreference, error) {
	path := "/api/notifications/preferences"
	if projectID != "" {
		path += "?" + url.Values{"project_id": {projectID}}.Encode()
	}
	var body json.RawMessage
	if err := m.do(ctx, http.MethodGet, path, nil, &body); err != nil {
		return nil, err
	}
	return decodeNotificationPreferences(body), nil
}

// SetNotificationPreference upserts one project-scoped preference (per event
// key + channel). projectID is the project the preference applies to.
func (m *MemoryClient) SetNotificationPreference(ctx context.Context, projectID, eventKey, channel string, enabled bool) error {
	if channel == "" {
		channel = "in_app"
	}
	body := map[string]any{"eventKey": eventKey, "channel": channel, "enabled": enabled}
	if projectID != "" {
		body["projectId"] = projectID
	}
	return m.do(ctx, http.MethodPut, "/api/notifications/preferences", body, nil)
}

// MarkNotificationRead marks one notification read.
func (m *MemoryClient) MarkNotificationRead(ctx context.Context, id string) error {
	return m.do(ctx, http.MethodPatch, notificationPath(id)+"/read", nil, nil)
}

// MarkNotificationUnread marks one notification unread.
func (m *MemoryClient) MarkNotificationUnread(ctx context.Context, id string) error {
	return m.do(ctx, http.MethodPost, notificationPath(id)+"/unread", nil, nil)
}

// DismissNotification dismisses (removes from the default view) one item.
func (m *MemoryClient) DismissNotification(ctx context.Context, id string) error {
	return m.do(ctx, http.MethodDelete, notificationPath(id)+"/dismiss", nil, nil)
}

// SnoozeNotification hides one item until the given RFC3339 instant.
func (m *MemoryClient) SnoozeNotification(ctx context.Context, id, until string) error {
	body := map[string]any{}
	if until != "" {
		body["until"] = until
	}
	return m.do(ctx, http.MethodPost, notificationPath(id)+"/snooze", body, nil)
}

// UnsnoozeNotification clears a snooze.
func (m *MemoryClient) UnsnoozeNotification(ctx context.Context, id string) error {
	return m.do(ctx, http.MethodPost, notificationPath(id)+"/unsnooze", nil, nil)
}

// ClearNotification archives one item into the Cleared tab.
func (m *MemoryClient) ClearNotification(ctx context.Context, id string) error {
	return m.do(ctx, http.MethodPost, notificationPath(id)+"/clear", nil, nil)
}

// RestoreNotification restores a cleared item to its normal tab.
func (m *MemoryClient) RestoreNotification(ctx context.Context, id string) error {
	return m.do(ctx, http.MethodPost, notificationPath(id)+"/restore", nil, nil)
}

// ResolveNotification resolves an actionable item with the chosen action verb
// (accept|decline|approve|deny|…). The verb travels in the `status` field —
// memory's resolve endpoint reads `status` (query param or JSON body), not
// `action`.
func (m *MemoryClient) ResolveNotification(ctx context.Context, id, action string) error {
	body := map[string]any{}
	if action != "" {
		body["status"] = action
	}
	return m.do(ctx, http.MethodPost, notificationPath(id)+"/resolve", body, nil)
}

// MarkAllNotificationsRead marks every notification read within one scope.
func (m *MemoryClient) MarkAllNotificationsRead(ctx context.Context, scope, projectID string) error {
	q := url.Values{}
	if scope != "" {
		q.Set("scope", scope)
	}
	if projectID != "" {
		q.Set("project_id", projectID)
	}
	return m.do(ctx, http.MethodPost, "/api/notifications/mark-all-read?"+q.Encode(), nil, nil)
}

// NotificationEventStream opens memory's real-time event stream for the
// signed-in session. An empty projectID selects the account/global stream (no
// projectId query param) that carries account-scope notification events; a
// non-empty projectID selects that project's stream. The caller owns the
// returned body and must close it.
//
// This deliberately does NOT send the session's X-Project-ID header: the
// account stream must stay project-agnostic, and the upstream stream scopes by
// its projectId query param. Only the bearer token is attached.
func (m *MemoryClient) NotificationEventStream(ctx context.Context, projectID string) (io.ReadCloser, error) {
	path := "/api/events/stream/account"
	if projectID != "" {
		path = "/api/events/stream?projectId=" + url.QueryEscape(projectID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+m.tokenFor(ctx))
	req.Header.Set("Accept", "text/event-stream")
	resp, err := m.streamHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		defer func() { _ = resp.Body.Close() }()
		raw, _ := io.ReadAll(resp.Body)
		perr := parseMemoryError(resp.StatusCode, raw)
		captureMemoryError(http.MethodGet, "/api/events/stream", resp.StatusCode, perr)
		return nil, perr
	}
	return resp.Body, nil
}

// notificationPath builds the per-item notification path with the id escaped.
func notificationPath(id string) string {
	return "/api/notifications/" + url.PathEscape(id)
}

// decodeNotificationList tolerates both the {"data":[…]} envelope and a bare
// array.
func decodeNotificationList(body json.RawMessage) []Notification {
	var env struct {
		Data []Notification `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err == nil && env.Data != nil {
		return env.Data
	}
	var arr []Notification
	if err := json.Unmarshal(body, &arr); err == nil {
		return arr
	}
	return nil
}

// decodeNotificationCounts tolerates both the {"data":{…}} envelope and a bare
// object.
func decodeNotificationCounts(body json.RawMessage) *NotificationCounts {
	var env struct {
		Data NotificationCounts `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err == nil && (env.Data != (NotificationCounts{})) {
		return &env.Data
	}
	var c NotificationCounts
	if err := json.Unmarshal(body, &c); err == nil {
		return &c
	}
	return &NotificationCounts{}
}

// decodeNotificationPreferences tolerates both the {"data":[…]} envelope and a
// bare array.
func decodeNotificationPreferences(body json.RawMessage) []NotificationPreference {
	var env struct {
		Data []NotificationPreference `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err == nil && env.Data != nil {
		return env.Data
	}
	var arr []NotificationPreference
	if err := json.Unmarshal(body, &arr); err == nil {
		return arr
	}
	return nil
}
