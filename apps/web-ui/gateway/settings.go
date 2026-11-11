package main

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

// --- project settings (project record, agent overrides, generic settings) ---

// Project mirrors memory's ProjectDTO (GET /api/projects/current and
// PATCH /api/projects/:id). Optional fields are pointers/maps so "absent" is
// distinguishable from a zero value. ProjectInfo/ChatPromptTemplate are
// snake_case on the wire, matching memory's JSON field names.
type Project struct {
	ID                          string         `json:"id"`
	Name                        string         `json:"name"`
	OrgID                       string         `json:"orgId,omitempty"`
	ProjectInfo                 string         `json:"project_info,omitempty"`
	ChatPromptTemplate          string         `json:"chat_prompt_template,omitempty"`
	AutoExtractObjects          *bool          `json:"auto_extract_objects,omitempty"`
	AutoExtractSourceSyncs      *bool          `json:"auto_extract_source_syncs,omitempty"`
	AutoExtractConfig           map[string]any `json:"auto_extract_config,omitempty"`
	BudgetUSD                   *float64       `json:"budget_usd,omitempty"`
	BudgetAlertThreshold        *float64       `json:"budget_alert_threshold,omitempty"`
	Stats                       map[string]any `json:"stats,omitempty"`
	AutoMergeExtractionBranches *bool          `json:"auto_merge_extraction_branches,omitempty"`
	// AutoDiscoveryEnabled is emitted by memory for every project (the column
	// defaults to true); a nil here means an older server, which is treated as
	// on to match the server-side default.
	AutoDiscoveryEnabled *bool  `json:"auto_discovery_enabled,omitempty"`
	MainBranchID         string `json:"main_branch_id,omitempty"`
	// SkillCapture is the project-level automatic skill-capture policy, kept as
	// a raw map so the UI round-trips the guardrail knobs it does not render
	// (nudgeInterval, caps, thresholds…). memory REPLACES the whole policy on
	// PATCH, so a typed struct narrowed to the two fields the UI edits would
	// silently reset everything else on the first save. See
	// apps/server/domain/skills/capture_config.go for the shape.
	SkillCapture map[string]any `json:"skill_capture,omitempty"`
}

// SkillCaptureMode returns the project's normalized capture mode: "off",
// "suggest", or "auto". Unset or unrecognized reads as "off", matching memory's
// default (the feature is opt-in).
func (p *Project) SkillCaptureMode() string {
	if p == nil || p.SkillCapture == nil {
		return "off"
	}
	v, _ := p.SkillCapture["mode"].(string)
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "suggest":
		return "suggest"
	case "auto":
		return "auto"
	default:
		return "off"
	}
}

// SkillCaptureAutoApply reports whether the project auto-apply key is on. It is
// only meaningful when SkillCaptureMode is "auto" (memory requires both).
func (p *Project) SkillCaptureAutoApply() bool {
	if p == nil || p.SkillCapture == nil {
		return false
	}
	v, _ := p.SkillCapture["autoApply"].(bool)
	return v
}

// ProjectUpdate is the PATCH /api/projects/:id body. Every field is a pointer
// so only the fields the form actually set are serialized (omitempty drops
// nil pointers, keeps explicit empty strings for the free-text fields).
type ProjectUpdate struct {
	Name                        string         `json:"name,omitempty"`
	ProjectInfo                 *string        `json:"project_info,omitempty"`
	ChatPromptTemplate          *string        `json:"chat_prompt_template,omitempty"`
	AutoExtractObjects          *bool          `json:"auto_extract_objects,omitempty"`
	AutoExtractSourceSyncs      *bool          `json:"auto_extract_source_syncs,omitempty"`
	AutoExtractConfig           map[string]any `json:"auto_extract_config,omitempty"`
	BudgetUSD                   *float64       `json:"budget_usd,omitempty"`
	BudgetAlertThreshold        *float64       `json:"budget_alert_threshold,omitempty"`
	AutoMergeExtractionBranches *bool          `json:"auto_merge_extraction_branches,omitempty"`
	AutoDiscoveryEnabled        *bool          `json:"auto_discovery_enabled,omitempty"`
	// SkillCapture replaces the project capture policy when non-nil. Callers
	// must start from the current policy (see capturePolicyWith) so knobs the
	// UI does not render survive the replace.
	SkillCapture map[string]any `json:"skill_capture,omitempty"`
}

// AgentOverrideEntry is one entry of GET
// /api/projects/:projectId/agent-definitions/overrides.
type AgentOverrideEntry struct {
	AgentName string              `json:"agentName"`
	Override  *AgentOverrideInput `json:"override"`
}

// AgentOverrideInput is one per-agent definition override (PUT
// /api/projects/:projectId/agent-definitions/overrides/:agentName). Only the
// fields the form set are non-nil.
type AgentOverrideInput struct {
	SystemPrompt  *string        `json:"systemPrompt,omitempty"`
	Model         *ModelConfig   `json:"model,omitempty"`
	Tools         []string       `json:"tools,omitempty"`
	MaxSteps      *int           `json:"maxSteps,omitempty"`
	SandboxConfig map[string]any `json:"sandboxConfig,omitempty"`
}

// ProjectSetting is one row of the generic per-project settings store
// (GET/PUT/DELETE /api/projects/:projectId/settings/:category/:key).
type ProjectSetting struct {
	ID        string         `json:"id"`
	ProjectID string         `json:"projectId"`
	Category  string         `json:"category"`
	Key       string         `json:"key"`
	Value     map[string]any `json:"value"`
	CreatedAt string         `json:"createdAt,omitempty"`
	UpdatedAt string         `json:"updatedAt,omitempty"`
}

// GetCurrentProject fetches the active project record. It resolves the project
// by id first (GET /api/projects/:id) using projectIDFor: a web session's
// Zitadel access token is never project-bound, so the token-bound
// /api/projects/current lookup returns null for it. When no project id is
// resolvable (e.g. an account-level API key with no static project), it falls
// back to the token-bound lookup (GET /api/projects/current), whose response is
// a bare {project, message} object and whose project may be null.
func (m *MemoryClient) GetCurrentProject(ctx context.Context) (*Project, error) {
	if id := m.projectIDFor(ctx); id != "" {
		var out Project
		if err := m.do(ctx, http.MethodGet, "/api/projects/"+url.PathEscape(id), nil, &out); err != nil {
			return nil, err
		}
		return &out, nil
	}
	var out struct {
		Project *Project `json:"project"`
		Message string   `json:"message"`
	}
	if err := m.do(ctx, http.MethodGet, "/api/projects/current", nil, &out); err != nil {
		return nil, err
	}
	return out.Project, nil
}

// UpdateProject partially updates the project record (PATCH /api/projects/:id).
// The response is the bare ProjectDTO (no envelope). Requires the
// projects:write scope; a token without it gets a 403 which surfaces as an
// error.
func (m *MemoryClient) UpdateProject(ctx context.Context, id string, in *ProjectUpdate) (*Project, error) {
	var out Project
	if err := m.do(ctx, http.MethodPatch, "/api/projects/"+url.PathEscape(id), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListAgentOverrides lists the project's agent definition overrides (GET
// .../agent-definitions/overrides). A 404 (no overrides) is surfaced as
// (nil, nil) rather than an error.
func (m *MemoryClient) ListAgentOverrides(ctx context.Context) ([]AgentOverrideEntry, error) {
	var env successEnvelope[[]AgentOverrideEntry]
	if err := m.do(ctx, http.MethodGet, "/api/projects/"+m.projectIDFor(ctx)+"/agent-definitions/overrides", nil, &env); err != nil {
		if isMemoryNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return env.Data, nil
}

// SetAgentOverride upserts one agent definition override (PUT
// .../agent-definitions/overrides/:agentName). Writing an existing agent name
// updates it; a new name creates it.
func (m *MemoryClient) SetAgentOverride(ctx context.Context, agentName string, in *AgentOverrideInput) error {
	var env successEnvelope[ProjectSetting]
	return m.do(ctx, http.MethodPut, "/api/projects/"+m.projectIDFor(ctx)+"/agent-definitions/overrides/"+url.PathEscape(agentName), in, &env)
}

// DeleteAgentOverride removes one agent definition override (DELETE
// .../agent-definitions/overrides/:agentName). A 404 (already absent) is
// treated as success — deleting an override that does not exist leaves the
// desired end state.
func (m *MemoryClient) DeleteAgentOverride(ctx context.Context, agentName string) error {
	var env successEnvelope[any]
	if err := m.do(ctx, http.MethodDelete, "/api/projects/"+m.projectIDFor(ctx)+"/agent-definitions/overrides/"+url.PathEscape(agentName), nil, &env); err != nil {
		if isMemoryNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

// GetProjectSetting fetches one generic project setting (GET
// .../settings/:category/:key). A 404 (setting absent) is surfaced as
// (nil, nil) so pages can render "not set".
func (m *MemoryClient) GetProjectSetting(ctx context.Context, category, key string) (*ProjectSetting, error) {
	var env successEnvelope[ProjectSetting]
	if err := m.do(ctx, http.MethodGet, "/api/projects/"+m.projectIDFor(ctx)+"/settings/"+url.PathEscape(category)+"/"+url.PathEscape(key), nil, &env); err != nil {
		if isMemoryNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return &env.Data, nil
}

// SetProjectSetting creates or updates one generic project setting (PUT
// .../settings/:category/:key). The body is the raw JSON value object.
func (m *MemoryClient) SetProjectSetting(ctx context.Context, category, key string, value map[string]any) error {
	var env successEnvelope[ProjectSetting]
	return m.do(ctx, http.MethodPut, "/api/projects/"+m.projectIDFor(ctx)+"/settings/"+url.PathEscape(category)+"/"+url.PathEscape(key), value, &env)
}

// DeleteProjectSetting removes one generic project setting (DELETE
// .../settings/:category/:key). A 404 (already absent) is treated as success
// — clearing an unset value is a no-op.
func (m *MemoryClient) DeleteProjectSetting(ctx context.Context, category, key string) error {
	var env successEnvelope[any]
	if err := m.do(ctx, http.MethodDelete, "/api/projects/"+m.projectIDFor(ctx)+"/settings/"+url.PathEscape(category)+"/"+url.PathEscape(key), nil, &env); err != nil {
		if isMemoryNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

// isMemoryNotFound reports whether a memory client error is a 404-style
// "absent" response. Settings reads and deletes map these to nil/success
// instead of errors so the UI can render "not set" states.
func isMemoryNotFound(err error) bool {
	if isMemoryStatus(err, http.StatusNotFound) {
		return true
	}
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "404") || strings.Contains(msg, "not_found") || strings.Contains(msg, "not found")
}

// boolPtr returns a pointer to b (PATCH bodies need pointers to distinguish
// "unset" from "false").
func boolPtr(b bool) *bool { return &b }
