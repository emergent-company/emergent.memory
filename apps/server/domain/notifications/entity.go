package notifications

import (
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
)

// Notification represents a notification in the kb.notifications table
type Notification struct {
	bun.BaseModel `bun:"table:kb.notifications,alias:n"`

	ID                  string           `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	ProjectID           *string          `bun:"project_id,type:uuid" json:"projectId,omitempty"`
	UserID              string           `bun:"user_id,notnull,type:uuid" json:"userId"`
	Title               string           `bun:"title,notnull" json:"title"`
	Message             string           `bun:"message,notnull" json:"message"`
	Type                *string          `bun:"type" json:"type,omitempty"`
	Severity            string           `bun:"severity,notnull,default:'info'" json:"severity"`
	RelatedResourceType *string          `bun:"related_resource_type" json:"relatedResourceType,omitempty"`
	RelatedResourceID   *string          `bun:"related_resource_id,type:uuid" json:"relatedResourceId,omitempty"`
	Read                bool             `bun:"read,notnull,default:false" json:"read"`
	Dismissed           bool             `bun:"dismissed,notnull,default:false" json:"dismissed"`
	DismissedAt         *time.Time       `bun:"dismissed_at" json:"dismissedAt,omitempty"`
	Actions             json.RawMessage  `bun:"actions,notnull,default:'[]'" json:"actions"`
	ExpiresAt           *time.Time       `bun:"expires_at" json:"expiresAt,omitempty"`
	ReadAt              *time.Time       `bun:"read_at" json:"readAt,omitempty"`
	Importance          string           `bun:"importance,notnull,default:'other'" json:"importance"`
	ClearedAt           *time.Time       `bun:"cleared_at" json:"clearedAt,omitempty"`
	SnoozedUntil        *time.Time       `bun:"snoozed_until" json:"snoozedUntil,omitempty"`
	Category            *string          `bun:"category" json:"category,omitempty"`
	SourceType          *string          `bun:"source_type" json:"sourceType,omitempty"`
	SourceID            *string          `bun:"source_id" json:"sourceId,omitempty"`
	ActionURL           *string          `bun:"action_url" json:"actionUrl,omitempty"`
	ActionLabel         *string          `bun:"action_label" json:"actionLabel,omitempty"`
	GroupKey            *string          `bun:"group_key" json:"groupKey,omitempty"`
	Details             json.RawMessage  `bun:"details,type:jsonb" json:"details,omitempty"`
	CreatedAt           time.Time        `bun:"created_at,notnull,default:now()" json:"createdAt"`
	UpdatedAt           time.Time        `bun:"updated_at,notnull,default:now()" json:"updatedAt"`
	ActionStatus        *string          `bun:"action_status" json:"actionStatus,omitempty"`
	ActionStatusAt      *time.Time       `bun:"action_status_at" json:"actionStatusAt,omitempty"`
	ActionStatusBy      *string          `bun:"action_status_by,type:uuid" json:"actionStatusBy,omitempty"`
	TaskID              *string          `bun:"task_id,type:uuid" json:"taskId,omitempty"`
}

// NotificationStats represents aggregated notification statistics
type NotificationStats struct {
	Unread    int64 `json:"unread"`
	Dismissed int64 `json:"dismissed"`
	Total     int64 `json:"total"`
}

// NotificationCounts represents counts by tab
type NotificationCounts struct {
	All       int64 `json:"all"`
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
	Tab        NotificationTab
	Category   string
	UnreadOnly bool
	Search     string
}

// NotificationListResponse wraps the notification list
type NotificationListResponse struct {
	Data []Notification `json:"data"`
}

// NotificationCountsResponse wraps the counts
type NotificationCountsResponse struct {
	Data NotificationCounts `json:"data"`
}
