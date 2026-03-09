package orgs

import (
	"time"

	"github.com/uptrace/bun"
)

// ToolPoolInvalidator is implemented by agents.ToolPool to invalidate all cached
// tool definitions when org-level tool settings change.
// Using InvalidateAll because org changes affect every project in the org.
type ToolPoolInvalidator interface {
	InvalidateAll()
}

// OrgToolSetting represents an org-level override for a built-in MCP tool setting.
// Stored in kb.org_tool_settings.
type OrgToolSetting struct {
	bun.BaseModel `bun:"table:kb.org_tool_settings,alias:ots"`

	ID        string         `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	OrgID     string         `bun:"org_id,notnull,type:uuid" json:"orgId"`
	ToolName  string         `bun:"tool_name,notnull" json:"toolName"`
	Enabled   bool           `bun:"enabled,notnull,default:true" json:"enabled"`
	Config    map[string]any `bun:"config,type:jsonb" json:"config,omitempty"`
	CreatedAt time.Time      `bun:"created_at,notnull,default:now()" json:"createdAt"`
	UpdatedAt time.Time      `bun:"updated_at,notnull,default:now()" json:"updatedAt"`
}

// OrgToolSettingDTO is the API response for an org tool setting.
type OrgToolSettingDTO struct {
	ID        string         `json:"id"`
	OrgID     string         `json:"orgId"`
	ToolName  string         `json:"toolName"`
	Enabled   bool           `json:"enabled"`
	Config    map[string]any `json:"config,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

// UpsertOrgToolSettingRequest is the request body for creating/updating an org tool setting.
type UpsertOrgToolSettingRequest struct {
	Enabled bool           `json:"enabled"`
	Config  map[string]any `json:"config,omitempty"`
}

// ToDTO converts an OrgToolSetting entity to OrgToolSettingDTO.
func (s *OrgToolSetting) ToDTO() OrgToolSettingDTO {
	return OrgToolSettingDTO{
		ID:        s.ID,
		OrgID:     s.OrgID,
		ToolName:  s.ToolName,
		Enabled:   s.Enabled,
		Config:    s.Config,
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
	}
}
