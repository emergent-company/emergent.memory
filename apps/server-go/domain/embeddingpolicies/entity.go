package embeddingpolicies

import (
	"time"

	"github.com/lib/pq"
	"github.com/uptrace/bun"
)

// EmbeddingPolicy represents an embedding policy entity from kb.embedding_policies table
type EmbeddingPolicy struct {
	bun.BaseModel `bun:"table:kb.embedding_policies"`

	ID               string         `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	ProjectID        string         `bun:"project_id,type:uuid,notnull" json:"projectId"`
	ObjectType       string         `bun:"object_type,notnull" json:"objectType"`
	Enabled          bool           `bun:"enabled,notnull,default:true" json:"enabled"`
	MaxPropertySize  *int           `bun:"max_property_size" json:"maxPropertySize"`
	RequiredLabels   pq.StringArray `bun:"required_labels,type:text[],notnull,default:'{}'" json:"requiredLabels"`
	ExcludedLabels   pq.StringArray `bun:"excluded_labels,type:text[],notnull,default:'{}'" json:"excludedLabels"`
	RelevantPaths    pq.StringArray `bun:"relevant_paths,type:text[],notnull,default:'{}'" json:"relevantPaths"`
	ExcludedStatuses pq.StringArray `bun:"excluded_statuses,type:text[],notnull,default:'{}'" json:"excludedStatuses"`
	CreatedAt        time.Time      `bun:"created_at,notnull,default:now()" json:"createdAt"`
	UpdatedAt        time.Time      `bun:"updated_at,notnull,default:now()" json:"updatedAt"`
}
