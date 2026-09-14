package main

import (
	"context"
	"net/http"
)

// Branch is one graph branch (GET /api/graph/branches). The "main" branch is
// implicit (branch_id absent), so it is not part of this list.
type Branch struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

// ListBranches lists the project's graph branches (excluding the implicit main
// branch).
func (m *MemoryClient) ListBranches(ctx context.Context) ([]Branch, error) {
	var out []Branch
	if err := m.doH(ctx, http.MethodGet, "/api/graph/branches", nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// --- skills ---

// SkillMetadata is the optional provenance block of a skill. All fields are
// recorded by the source that authored the skill (registry, file, …) and are
// read-only from the gateway's perspective.
type SkillMetadata struct {
	Source      string `json:"source,omitempty"`
	License     string `json:"license,omitempty"`
	Version     string `json:"version,omitempty"`
	SourceURL   string `json:"source_url,omitempty"`
	OriginID    string `json:"origin_id,omitempty"`
	ContentHash string `json:"content_hash,omitempty"`
	Location    string `json:"location,omitempty"`
}

// Skill mirrors memory's SkillDTO. Skills responses are BARE JSON (a list is
// {"skills":[...]}, a single object is the DTO directly) — unlike the
// agent-definition routes there is no {success,data,error} envelope.
type Skill struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	Content      string         `json:"content"`
	Metadata     *SkillMetadata `json:"metadata,omitempty"`
	HasEmbedding bool           `json:"hasEmbedding"`
	ProjectID    string         `json:"projectId,omitempty"`
	OrgID        string         `json:"orgId,omitempty"`
	Scope        string         `json:"scope"`
	CreatedAt    string         `json:"createdAt,omitempty"`
	UpdatedAt    string         `json:"updatedAt,omitempty"`
}

// ListSkills lists the project's skills merged with org- and global-scope
// skills (GET /api/projects/{projectID}/skills).
func (m *MemoryClient) ListSkills(ctx context.Context) ([]Skill, error) {
	var out struct {
		Skills []Skill `json:"skills"`
	}
	if err := m.do(ctx, http.MethodGet, "/api/projects/"+m.projectIDFor(ctx)+"/skills", nil, &out); err != nil {
		return nil, err
	}
	return out.Skills, nil
}

// GetSkill fetches a single skill by id (GET /api/skills/{id}). Unlike the
// other skill routes this one is global, not project-scoped.
func (m *MemoryClient) GetSkill(ctx context.Context, id string) (*Skill, error) {
	var out Skill
	if err := m.do(ctx, http.MethodGet, "/api/skills/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateSkill creates a project-scoped skill (POST
// /api/projects/{projectID}/skills). The name must be a slug
// (^[a-z0-9]+(-[a-z0-9]+)*$, 1–64 chars); description and content must be
// non-empty and content is capped at 1 MiB — memory enforces all of these.
func (m *MemoryClient) CreateSkill(ctx context.Context, name, description, content string) (*Skill, error) {
	body := map[string]any{"name": name, "description": description, "content": content}
	var out Skill
	if err := m.do(ctx, http.MethodPost, "/api/projects/"+m.projectIDFor(ctx)+"/skills", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateSkill partially updates a skill (PATCH
// /api/projects/{projectID}/skills/{id}). Only description and content are
// updatable — name and scope are immutable on the server.
func (m *MemoryClient) UpdateSkill(ctx context.Context, id, description, content string) (*Skill, error) {
	body := map[string]any{"description": description, "content": content}
	var out Skill
	if err := m.do(ctx, http.MethodPatch, "/api/projects/"+m.projectIDFor(ctx)+"/skills/"+id, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteSkill removes a project-scoped skill (DELETE
// /api/projects/{projectID}/skills/{id}, 204 no body).
func (m *MemoryClient) DeleteSkill(ctx context.Context, id string) error {
	return m.do(ctx, http.MethodDelete, "/api/projects/"+m.projectIDFor(ctx)+"/skills/"+id, nil, nil)
}
