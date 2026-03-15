package agents

import (
	"time"

	"github.com/uptrace/bun"
)

// ProjectSetting is a general-purpose key/value store for per-project configuration.
// Table: kb.project_settings
type ProjectSetting struct {
	bun.BaseModel `bun:"table:kb.project_settings,alias:ps"`

	ID        string         `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	ProjectID string         `bun:"project_id,type:uuid,notnull" json:"projectId"`
	Category  string         `bun:"category,notnull" json:"category"`
	Key       string         `bun:"key,notnull" json:"key"`
	Value     map[string]any `bun:"value,type:jsonb,notnull,default:'{}'" json:"value"`
	CreatedAt time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"createdAt"`
	UpdatedAt time.Time      `bun:"updated_at,nullzero,notnull,default:current_timestamp" json:"updatedAt"`
}

// Category constants for project settings
const (
	SettingsCategoryAgentOverride = "agent_override"
)

// AgentOverride represents a partial agent definition override.
// Fields that are nil/empty are not overridden — they inherit the canonical defaults.
type AgentOverride struct {
	SystemPrompt  *string        `json:"systemPrompt,omitempty"`
	Model         *ModelConfig   `json:"model,omitempty"`
	Tools         []string       `json:"tools,omitempty"`
	MaxSteps      *int           `json:"maxSteps,omitempty"`
	SandboxConfig map[string]any `json:"sandboxConfig,omitempty"`
}
