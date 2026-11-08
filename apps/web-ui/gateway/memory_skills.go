package main

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
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

// --- skill capture proposals (kb.skill_proposals) ---

// Proposal statuses as memory reports them. "applying" is transient: a row is
// claimed by one reviewer while the write runs.
const (
	captureProposalPending  = "pending"
	captureProposalApplying = "applying"
	captureProposalApplied  = "applied"
	captureProposalRejected = "rejected"
	captureProposalFailed   = "failed"
)

// SkillCaptureFinding is one linter finding recorded on a staged proposal.
// Errors never stage, so these are warnings the reviewer should weigh — they
// render beside the candidate content, not as a blocker.
type SkillCaptureFinding struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

// SkillCaptureProposal mirrors memory's ProposalDTO: a skill the background capture
// review derived from an agent run and staged for a human decision. Content is
// the resulting skill body for both "create" and "patch" (the server merges a
// patch before staging it); PatchOld/PatchNew carry the diff fragments and are
// empty for the other kinds. "delete" proposals carry no content at all.
type SkillCaptureProposal struct {
	ID          string                `json:"id"`
	ProjectID   string                `json:"projectId"`
	AgentID     string                `json:"agentId,omitempty"`
	AgentRunID  string                `json:"agentRunId,omitempty"`
	AgentName   string                `json:"agentName,omitempty"`
	ReviewID    string                `json:"reviewId,omitempty"`
	Kind        string                `json:"kind"`
	Name        string                `json:"name"`
	Description string                `json:"description"`
	Content     string                `json:"content"`
	BaseSkillID string                `json:"baseSkillId,omitempty"`
	PatchOld    string                `json:"patchOld,omitempty"`
	PatchNew    string                `json:"patchNew,omitempty"`
	Evidence    string                `json:"evidence,omitempty"`
	Confidence  float64               `json:"confidence"`
	Source      string                `json:"source"`
	Status      string                `json:"status"`
	AutoApplied bool                  `json:"autoApplied"`
	ReviewNote  string                `json:"reviewNote,omitempty"`
	Findings    []SkillCaptureFinding `json:"findings,omitempty"`
	CreatedAt   string                `json:"createdAt"`
	AppliedAt   string                `json:"appliedAt,omitempty"`
}

// AwaitingReview reports whether the proposal is still a decision for a human.
// An "applying" row is mid-write — another reviewer already claimed it — so it
// is deliberately not actionable: offering the buttons would race that write and
// come back as a 409.
func (p *SkillCaptureProposal) AwaitingReview() bool {
	return p != nil && p.Status == captureProposalPending
}

// ListSkillProposals lists the project's staged proposals (GET
// /api/projects/{projectID}/skills/proposals). status is pending, applied,
// rejected, failed, or all ("" also means all); limit 0 uses the server default
// (50, max 200).
func (m *MemoryClient) ListSkillProposals(ctx context.Context, status string, limit int) ([]SkillCaptureProposal, error) {
	q := url.Values{}
	if status != "" {
		q.Set("status", status)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	path := "/api/projects/" + m.projectIDFor(ctx) + "/skills/proposals"
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	var out struct {
		Proposals []SkillCaptureProposal `json:"proposals"`
	}
	if err := m.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Proposals, nil
}

// GetSkillProposal fetches one staged proposal (GET
// /api/projects/{projectID}/skills/proposals/{id}). The server returns the DTO
// bare — no {success,data} envelope on this route.
func (m *MemoryClient) GetSkillProposal(ctx context.Context, id string) (*SkillCaptureProposal, error) {
	var out SkillCaptureProposal
	path := "/api/projects/" + m.projectIDFor(ctx) + "/skills/proposals/" + url.PathEscape(id)
	if err := m.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ApproveSkillProposal applies a proposal through the normal skills store (POST
// …/proposals/{id}/approve). The security scan always re-runs server-side and can
// still block the write, so the error is surfaced to the operator rather than
// swallowed. note is optional review context stored on the row.
func (m *MemoryClient) ApproveSkillProposal(ctx context.Context, id, note string) error {
	body := map[string]any{"note": note}
	path := "/api/projects/" + m.projectIDFor(ctx) + "/skills/proposals/" + url.PathEscape(id) + "/approve"
	return m.do(ctx, http.MethodPost, path, body, nil)
}

// RejectSkillProposal discards a proposal without touching the skill library
// (POST …/proposals/{id}/reject).
func (m *MemoryClient) RejectSkillProposal(ctx context.Context, id, note string) error {
	body := map[string]any{"note": note}
	path := "/api/projects/" + m.projectIDFor(ctx) + "/skills/proposals/" + url.PathEscape(id) + "/reject"
	return m.do(ctx, http.MethodPost, path, body, nil)
}
