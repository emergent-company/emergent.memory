package useractivity

import (
	"time"

	"github.com/uptrace/bun"
)

// UserRecentItem represents a user's recent activity item in kb.user_recent_items
type UserRecentItem struct {
	bun.BaseModel `bun:"table:kb.user_recent_items,alias:uri"`

	ID              string    `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	UserID          string    `bun:"user_id,notnull" json:"userId"`
	ProjectID       string    `bun:"project_id,notnull,type:uuid" json:"projectId"`
	ResourceType    string    `bun:"resource_type,notnull" json:"resourceType"`
	ResourceID      string    `bun:"resource_id,notnull,type:uuid" json:"resourceId"`
	ResourceName    *string   `bun:"resource_name" json:"resourceName,omitempty"`
	ResourceSubtype *string   `bun:"resource_subtype" json:"resourceSubtype,omitempty"`
	ActionType      string    `bun:"action_type,notnull" json:"actionType"`
	AccessedAt      time.Time `bun:"accessed_at,notnull" json:"accessedAt"`
	CreatedAt       time.Time `bun:"created_at,notnull,default:now()" json:"createdAt"`
}

// RecordActivityRequest is the request body for recording user activity
type RecordActivityRequest struct {
	ResourceType    string  `json:"resourceType" validate:"required"`
	ResourceID      string  `json:"resourceId" validate:"required,uuid"`
	ResourceName    *string `json:"resourceName,omitempty"`
	ResourceSubtype *string `json:"resourceSubtype,omitempty"`
	ActionType      string  `json:"actionType" validate:"required"`
}

// RecentItemResponse is a single recent item in the response
type RecentItemResponse struct {
	ID              string    `json:"id"`
	ResourceType    string    `json:"resourceType"`
	ResourceID      string    `json:"resourceId"`
	ResourceName    *string   `json:"resourceName,omitempty"`
	ResourceSubtype *string   `json:"resourceSubtype,omitempty"`
	ActionType      string    `json:"actionType"`
	AccessedAt      time.Time `json:"accessedAt"`
	ProjectID       string    `json:"projectId"`
}

// RecentItemsResponse is the response for listing recent items
type RecentItemsResponse struct {
	Data []RecentItemResponse `json:"data"`
}
