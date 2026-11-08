package main

import (
	"context"
	"net/http"
	"net/url"
)

// Branch is one graph branch (GET /api/graph/branches). The "main" branch is
// implicit (branch_id absent), so it is not part of this list.
type Branch struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	// MergedAt is set once the branch has been merged into main; empty while
	// the branch is still open.
	MergedAt string `json:"merged_at,omitempty"`
	// Origin is who or what created the branch (extraction | agent | manual |
	// system); OriginLabel is the human-readable source (source document
	// filename, agent name, user name). Both are metadata for labels — every
	// branch is read the same way.
	Origin      string `json:"origin,omitempty"`
	OriginID    string `json:"origin_id,omitempty"`
	OriginLabel string `json:"origin_label,omitempty"`
}

// branchDisplayName is how a branch is presented to a person: its origin and
// source when known ("Extraction · Invoice.pdf"), else its name. Extraction
// branch names are opaque (extraction/<doc>/<job>) so the label is the only
// readable thing in a picker.
func branchDisplayName(b Branch) string {
	switch b.Origin {
	case "extraction":
		if b.OriginLabel != "" {
			return "Extraction · " + b.OriginLabel
		}
		return "Extraction · " + b.Name
	case "agent":
		if b.OriginLabel != "" {
			return "Agent · " + b.OriginLabel
		}
		return "Agent · " + b.Name
	case "system":
		if b.OriginLabel != "" {
			return "System · " + b.OriginLabel
		}
		return "System · " + b.Name
	}
	return b.Name
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
	// LifecycleState is maintained by memory's curator for agent-created
	// skills: active | stale | archived. Archived skills are not auto-loaded
	// or semantically retrieved; they remain readable by name.
	LifecycleState string `json:"lifecycle_state,omitempty"`
	// Pinned skills are never aged by the curator; locked skills refuse
	// agent-initiated writes.
	Pinned bool `json:"pinned,omitempty"`
	Locked bool `json:"locked,omitempty"`
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

// LifecycleState returns the stored lifecycle state, defaulting to active
// (mirrors memory's Skill.LifecycleStateOrDefault).
func (s Skill) LifecycleState() string {
	if s.Metadata == nil || s.Metadata.LifecycleState == "" {
		return "active"
	}
	return s.Metadata.LifecycleState
}

// Archived reports whether the curator has aged this skill out. Archived
// skills stay visible here but are hidden from agents until restored.
func (s Skill) Archived() bool { return s.LifecycleState() == "archived" }

// IsPinned reports whether the curator is barred from ageing this skill.
func (s Skill) IsPinned() bool { return s.Metadata != nil && s.Metadata.Pinned }

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

// ListSkillsIncludingArchived lists the project's skills merged with org- and
// global-scope skills, including archived ones (GET
// /api/projects/{projectID}/skills?includeArchived=true). Only the skills
// management page uses this: archived skills must stay visible and restorable
// there, while agent retrieval keeps excluding them on the server.
func (m *MemoryClient) ListSkillsIncludingArchived(ctx context.Context) ([]Skill, error) {
	var out struct {
		Skills []Skill `json:"skills"`
	}
	if err := m.do(ctx, http.MethodGet, "/api/projects/"+m.projectIDFor(ctx)+"/skills?includeArchived=true", nil, &out); err != nil {
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

// RestoreSkill returns an archived skill to the active lifecycle state (POST
// /api/projects/{projectID}/skills/{id}/restore). Memory changes only the
// lifecycle state; every other metadata field is preserved. pin=true also pins
// the skill, the durable form that stops the curator from archiving it again.
func (m *MemoryClient) RestoreSkill(ctx context.Context, id string, pin bool) (*Skill, error) {
	path := "/api/projects/" + m.projectIDFor(ctx) + "/skills/" + url.PathEscape(id) + "/restore"
	body := map[string]any{"pin": pin}
	var out Skill
	if err := m.do(ctx, http.MethodPost, path, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// --- skill proposals ---

// SkillCaptureFinding mirrors memory's skills.LintFinding: one linter or
// security-scanner finding on a proposal. The list endpoint recomputes these
// live for pending proposals, so they are the current verdict rather than a
// snapshot taken when the proposal was staged.
type SkillCaptureFinding struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

// SkillCaptureProposal mirrors memory's skills.ProposalDTO: an agent-captured skill
// change waiting for a human decision. Proposals responses are BARE JSON (a list
// is {"proposals":[...]}), like the other skills routes and unlike the
// agent-definition {success,data,error} envelope.
type SkillCaptureProposal struct {
	ID          string                `json:"id"`
	ProjectID   string                `json:"projectId"`
	AgentID     string                `json:"agentId,omitempty"`
	AgentName   string                `json:"agentName,omitempty"`
	Kind        string                `json:"kind"`
	Name        string                `json:"name"`
	Description string                `json:"description"`
	Content     string                `json:"content"`
	BaseSkillID string                `json:"baseSkillId,omitempty"`
	Evidence    string                `json:"evidence,omitempty"`
	Confidence  float64               `json:"confidence"`
	Source      string                `json:"source"`
	Status      string                `json:"status"`
	AutoApplied bool                  `json:"autoApplied"`
	Findings    []SkillCaptureFinding `json:"findings,omitempty"`
	CreatedAt   string                `json:"createdAt"`
}

// ListSkillCaptureProposals lists the project's pending skill proposals (GET
// /api/projects/{projectID}/skills/proposals?status=pending). Pending is the
// only state the review panel acts on; applied/rejected history is not shown.
func (m *MemoryClient) ListSkillCaptureProposals(ctx context.Context) ([]SkillCaptureProposal, error) {
	var out struct {
		Proposals []SkillCaptureProposal `json:"proposals"`
	}
	path := "/api/projects/" + m.projectIDFor(ctx) + "/skills/proposals?status=pending"
	if err := m.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Proposals, nil
}

// GetSkillCaptureProposal fetches one proposal (GET
// /api/projects/{projectID}/skills/proposals/{id}).
func (m *MemoryClient) GetSkillCaptureProposal(ctx context.Context, id string) (*SkillCaptureProposal, error) {
	var out SkillCaptureProposal
	path := "/api/projects/" + m.projectIDFor(ctx) + "/skills/proposals/" + url.PathEscape(id)
	if err := m.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ApproveSkillCaptureProposal approves and applies a pending proposal (POST
// /api/projects/{projectID}/skills/proposals/{id}/approve). memory re-runs the
// security scan on this path and can still block the write, so a failed call
// means the proposal stayed pending — the panel's redirect re-reads the list
// rather than assuming success.
func (m *MemoryClient) ApproveSkillCaptureProposal(ctx context.Context, id string) error {
	path := "/api/projects/" + m.projectIDFor(ctx) + "/skills/proposals/" + url.PathEscape(id) + "/approve"
	return m.do(ctx, http.MethodPost, path, map[string]any{}, nil)
}

// RejectSkillCaptureProposal marks a pending proposal rejected without touching the
// skill library (POST
// /api/projects/{projectID}/skills/proposals/{id}/reject).
func (m *MemoryClient) RejectSkillCaptureProposal(ctx context.Context, id string) error {
	path := "/api/projects/" + m.projectIDFor(ctx) + "/skills/proposals/" + url.PathEscape(id) + "/reject"
	return m.do(ctx, http.MethodPost, path, map[string]any{}, nil)
}
