package notifications

import (
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
)

// Scope classifies which inbox a notification belongs to.
type Scope string

const (
	ScopeAccount Scope = "account"
	ScopeProject Scope = "project"
)

// Valid reports whether the scope is a recognised value.
func (s Scope) Valid() bool {
	return s == ScopeAccount || s == ScopeProject
}

// Notification represents a notification in the kb.notifications table
type Notification struct {
	bun.BaseModel `bun:"table:kb.notifications,alias:n"`

	ID                  string          `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	ProjectID           *string         `bun:"project_id,type:uuid" json:"projectId,omitempty"`
	UserID              string          `bun:"user_id,notnull,type:uuid" json:"userId"`
	Title               string          `bun:"title,notnull" json:"title"`
	Message             string          `bun:"message,notnull" json:"message"`
	Type                *string         `bun:"type" json:"type,omitempty"`
	Severity            string          `bun:"severity,notnull,default:'info'" json:"severity"`
	RelatedResourceType *string         `bun:"related_resource_type" json:"relatedResourceType,omitempty"`
	RelatedResourceID   *string         `bun:"related_resource_id,type:uuid" json:"relatedResourceId,omitempty"`
	Read                bool            `bun:"read,notnull,default:false" json:"read"`
	Dismissed           bool            `bun:"dismissed,notnull,default:false" json:"dismissed"`
	DismissedAt         *time.Time      `bun:"dismissed_at" json:"dismissedAt,omitempty"`
	Actions             json.RawMessage `bun:"actions,notnull,default:'[]'" json:"actions"`
	ExpiresAt           *time.Time      `bun:"expires_at" json:"expiresAt,omitempty"`
	ReadAt              *time.Time      `bun:"read_at" json:"readAt,omitempty"`
	Importance          string          `bun:"importance,notnull,default:'other'" json:"importance"`
	ClearedAt           *time.Time      `bun:"cleared_at" json:"clearedAt,omitempty"`
	SnoozedUntil        *time.Time      `bun:"snoozed_until" json:"snoozedUntil,omitempty"`
	Category            *string         `bun:"category" json:"category,omitempty"`
	SourceType          *string         `bun:"source_type" json:"sourceType,omitempty"`
	SourceID            *string         `bun:"source_id" json:"sourceId,omitempty"`
	ActionURL           *string         `bun:"action_url" json:"actionUrl,omitempty"`
	ActionLabel         *string         `bun:"action_label" json:"actionLabel,omitempty"`
	GroupKey            *string         `bun:"group_key" json:"groupKey,omitempty"`
	Details             json.RawMessage `bun:"details,type:jsonb" json:"details,omitempty"`
	CreatedAt           time.Time       `bun:"created_at,notnull,default:now()" json:"createdAt"`
	UpdatedAt           time.Time       `bun:"updated_at,notnull,default:now()" json:"updatedAt"`
	ActionStatus        *string         `bun:"action_status" json:"actionStatus,omitempty"`
	ActionStatusAt      *time.Time      `bun:"action_status_at" json:"actionStatusAt,omitempty"`
	ActionStatusBy      *string         `bun:"action_status_by,type:uuid" json:"actionStatusBy,omitempty"`
	TaskID              *string         `bun:"task_id,type:uuid" json:"taskId,omitempty"`
	Scope               Scope           `bun:"scope,notnull,default:'account'" json:"scope"`
	RequiresAction      bool            `bun:"requires_action,notnull,default:false" json:"requiresAction"`
	EventKey            *string         `bun:"event_key" json:"eventKey,omitempty"`
}

// NotificationPreference represents a per-user, per-project, per-event-key,
// per-channel delivery preference in the kb.notification_preferences table.
type NotificationPreference struct {
	bun.BaseModel `bun:"table:kb.notification_preferences,alias:np"`

	ID        string    `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	UserID    string    `bun:"user_id,notnull,type:uuid" json:"userId"`
	ProjectID *string   `bun:"project_id,type:uuid" json:"projectId,omitempty"`
	EventKey  string    `bun:"event_key,notnull" json:"eventKey"`
	Channel   string    `bun:"channel,notnull,default:'in_app'" json:"channel"`
	Enabled   bool      `bun:"enabled,notnull,default:false" json:"enabled"`
	CreatedAt time.Time `bun:"created_at,notnull,default:now()" json:"createdAt"`
	UpdatedAt time.Time `bun:"updated_at,notnull,default:now()" json:"updatedAt"`
}

// NotificationStats represents aggregated notification statistics.
//
// All three counters share the base population `cleared_at IS NULL`, so
// `Dismissed <= Total` always holds.
//
// `Dismissed` is effectively dead for API-produced data: Repository.Dismiss
// sets `dismissed = true` AND `cleared_at = now()` in the same update, so a
// dismissed row is always cleared and is therefore excluded from this counter
// (and from Total/Unread) by construction. Repository.Restore resets both
// `dismissed` and `dismissed_at` alongside `cleared_at`, so a restored row is
// likewise neither cleared nor dismissed and never counts here. The counter is
// retained for schema and API compatibility; removing or repurposing it —
// together with the `dismissed`/`dismissed_at` columns — is a follow-up.
type NotificationStats struct {
	Unread    int64 `json:"unread"`
	Dismissed int64 `json:"dismissed"`
	Total     int64 `json:"total"`
}

// NotificationCounts represents counts by tab. Unread is the bell badge count:
// unread AND not cleared AND not currently snoozed (matches the other lifecycle
// buckets' cleared/snoozed exclusion, plus read=false).
type NotificationCounts struct {
	All       int64 `json:"all"`
	Unread    int64 `json:"unread"`
	Important int64 `json:"important"`
	Other     int64 `json:"other"`
	Snoozed   int64 `json:"snoozed"`
	Cleared   int64 `json:"cleared"`
}

// NotificationTab represents the notification tab filter
type NotificationTab string

const (
	TabAll       NotificationTab = "all"
	TabImportant NotificationTab = "important"
	TabOther     NotificationTab = "other"
	TabSnoozed   NotificationTab = "snoozed"
	TabCleared   NotificationTab = "cleared"
)

// ListParams contains parameters for listing notifications
type ListParams struct {
	Tab            NotificationTab
	Category       string
	UnreadOnly     bool
	Search         string
	Scope          Scope
	ProjectID      *string
	RequiresAction bool
}

// CountParams contains parameters for counting notifications.
type CountParams struct {
	Scope          Scope
	ProjectID      *string
	RequiresAction bool
}

// CreateInput is the producer entry point payload for creating a notification.
type CreateInput struct {
	UserID              string
	ProjectID           *string
	Scope               Scope
	EventKey            string
	Title               string
	Message             string
	Type                *string
	Severity            string
	Category            *string
	Importance          string
	SourceType          *string
	SourceID            *string
	RelatedResourceType *string
	RelatedResourceID   *string
	ActionURL           *string
	ActionLabel         *string
	Actions             json.RawMessage
	RequiresAction      bool
	GroupKey            *string
	ExpiresAt           *time.Time
	Details             json.RawMessage
	TaskID              *string
}

// PreferenceEntry is the effective notification preference for a single event
// key, materialised from the taxonomy and any stored preference row.
type PreferenceEntry struct {
	EventKey       string `json:"eventKey"`
	Scope          Scope  `json:"scope"`
	Channel        string `json:"channel"`
	Delivery       string `json:"delivery"`
	Required       bool   `json:"required"`
	RequiresAction bool   `json:"requiresAction"`
	Actionable     bool   `json:"actionable"`
	Category       string `json:"category,omitempty"`
	Enabled        bool   `json:"enabled"`
	Default        bool   `json:"default"`
}

// NotificationListResponse wraps the notification list
type NotificationListResponse struct {
	Data []Notification `json:"data"`
}

// NotificationCountsResponse wraps the counts
type NotificationCountsResponse struct {
	Data NotificationCounts `json:"data"`
}

// PreferenceListResponse wraps the effective preference list.
type PreferenceListResponse struct {
	Data []PreferenceEntry `json:"data"`
}
