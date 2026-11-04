package tasks

import (
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
)

// Task represents a task in the kb.tasks table
type Task struct {
	bun.BaseModel `bun:"table:kb.tasks,alias:t"`

	ID              string          `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	ProjectID       string          `bun:"project_id,notnull,type:uuid" json:"projectId"`
	Title           string          `bun:"title,notnull" json:"title"`
	Description     *string         `bun:"description" json:"description,omitempty"`
	Type            string          `bun:"type,notnull" json:"type"`
	Status          string          `bun:"status,notnull,default:'pending'" json:"status"`
	ResolvedAt      *time.Time      `bun:"resolved_at" json:"resolvedAt,omitempty"`
	ResolvedBy      *string         `bun:"resolved_by,type:uuid" json:"resolvedBy,omitempty"`
	ResolutionNotes *string         `bun:"resolution_notes" json:"resolutionNotes,omitempty"`
	SourceType      *string         `bun:"source_type" json:"sourceType,omitempty"`
	SourceID        *string         `bun:"source_id" json:"sourceId,omitempty"`
	Metadata        json.RawMessage `bun:"metadata,type:jsonb,default:'{}'" json:"metadata,omitempty"`
	CreatedAt       time.Time       `bun:"created_at,default:now()" json:"createdAt"`
	UpdatedAt       time.Time       `bun:"updated_at,default:now()" json:"updatedAt"`
}

// TaskCounts represents task counts by status
type TaskCounts struct {
	Pending   int64 `json:"pending"`
	Accepted  int64 `json:"accepted"`
	Rejected  int64 `json:"rejected"`
	Cancelled int64 `json:"cancelled"`
}

// TaskCountsResponse wraps the counts for the API response
type TaskCountsResponse struct {
	Pending   int64 `json:"pending"`
	Accepted  int64 `json:"accepted"`
	Rejected  int64 `json:"rejected"`
	Cancelled int64 `json:"cancelled"`
}

// TaskListParams contains parameters for listing tasks
type TaskListParams struct {
	ProjectID string
	Status    string
	Type      string
	Limit     int
	Offset    int
}

// TaskListResponse wraps the list of tasks for the API response
type TaskListResponse struct {
	Data  []Task `json:"data"`
	Total int    `json:"total"`
}

// ResolveTaskRequest is the request body for resolving a task
type ResolveTaskRequest struct {
	Resolution      string  `json:"resolution"` // "accepted" or "rejected"
	ResolutionNotes *string `json:"resolutionNotes,omitempty"`
}

// TaskResponse wraps a single task for the API response
type TaskResponse struct {
	Data Task `json:"data"`
}
