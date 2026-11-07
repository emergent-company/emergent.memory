package skills

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// Proposal kinds and statuses.
const (
	ProposalKindCreate = "create"
	ProposalKindPatch  = "patch"
	ProposalKindDelete = "delete"

	ProposalStatusPending  = "pending"
	ProposalStatusApplying = "applying"
	ProposalStatusApplied  = "applied"
	ProposalStatusRejected = "rejected"
	ProposalStatusFailed   = "failed"

	ReviewStatusPending = "pending"
	ReviewStatusRunning = "running"
	ReviewStatusDone    = "done"
	ReviewStatusFailed  = "failed"
	ReviewStatusSkipped = "skipped"

	ProposalSourceBackground = "background"
	ProposalSourceForeground = "foreground"
)

// SkillProposal is a staged candidate from the skill-capture loop. It is inert
// until the proposal service applies it through the normal skills store.
type SkillProposal struct {
	bun.BaseModel `bun:"table:kb.skill_proposals,alias:sp"`

	ID              uuid.UUID  `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	ProjectID       uuid.UUID  `bun:"project_id,type:uuid,notnull" json:"projectId"`
	AgentID         *uuid.UUID `bun:"agent_id,type:uuid" json:"agentId,omitempty"`
	AgentRunID      *uuid.UUID `bun:"agent_run_id,type:uuid" json:"agentRunId,omitempty"`
	AgentName       string     `bun:"agent_name,notnull" json:"agentName,omitempty"`
	ReviewID        *uuid.UUID `bun:"review_id,type:uuid" json:"reviewId,omitempty"`
	Kind            string     `bun:"kind,notnull" json:"kind"`
	Name            string     `bun:"name,notnull" json:"name"`
	Description     string     `bun:"description,notnull" json:"description"`
	Content         string     `bun:"content,notnull" json:"content"`
	BaseSkillID     *uuid.UUID `bun:"base_skill_id,type:uuid" json:"baseSkillId,omitempty"`
	BaseContentHash string     `bun:"base_content_hash,notnull" json:"baseContentHash"`
	PatchOld        string     `bun:"patch_old,notnull" json:"patchOld"`
	PatchNew        string     `bun:"patch_new,notnull" json:"patchNew"`
	Evidence        string     `bun:"evidence,notnull" json:"evidence"`
	Confidence      float64    `bun:"confidence,notnull" json:"confidence"`
	Source          string     `bun:"source,notnull" json:"source"`
	Status          string     `bun:"status,notnull" json:"status"`
	AutoApplied     bool       `bun:"auto_applied,notnull" json:"autoApplied"`
	ReviewedBy      *uuid.UUID `bun:"reviewed_by,type:uuid" json:"reviewedBy,omitempty"`
	ReviewNote      string     `bun:"review_note,notnull" json:"reviewNote"`
	CreatedAt       time.Time  `bun:"created_at,notnull,default:now()" json:"createdAt"`
	AppliedAt       *time.Time `bun:"applied_at" json:"appliedAt,omitempty"`
	ClaimedAt       *time.Time `bun:"claimed_at" json:"claimedAt,omitempty"`

	// Lint/scan findings are computed on read for pending proposals; they are
	// not persisted (the review input keeps the audit trail).
	Findings []LintFinding `bun:"-" json:"findings,omitempty"`
}

// SkillReview is the persisted, bounded background-review job for one run.
// The executor only inserts a pending row; a single poller claims it with
// FOR UPDATE SKIP LOCKED.
type SkillReview struct {
	bun.BaseModel `bun:"table:kb.skill_reviews,alias:sr"`

	ID             uuid.UUID      `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	ProjectID      uuid.UUID      `bun:"project_id,type:uuid,notnull" json:"projectId"`
	OrgID          *uuid.UUID     `bun:"org_id,type:uuid" json:"orgId,omitempty"`
	AgentID        *uuid.UUID     `bun:"agent_id,type:uuid" json:"agentId,omitempty"`
	AgentRunID     *uuid.UUID     `bun:"agent_run_id,type:uuid" json:"agentRunId,omitempty"`
	AgentName      string         `bun:"agent_name,notnull" json:"agentName"`
	Status         string         `bun:"status,notnull" json:"status"`
	Model          string         `bun:"model,notnull" json:"model"`
	Input          map[string]any `bun:"input,type:jsonb,notnull" json:"input,omitempty"`
	InputTokens    int            `bun:"input_tokens,notnull" json:"inputTokens"`
	OutputTokens   int            `bun:"output_tokens,notnull" json:"outputTokens"`
	CandidateCount int            `bun:"candidate_count,notnull" json:"candidateCount"`
	Error          *string        `bun:"error" json:"error,omitempty"`
	CreatedAt      time.Time      `bun:"created_at,notnull,default:now()" json:"createdAt"`
	StartedAt      *time.Time     `bun:"started_at" json:"startedAt,omitempty"`
	FinishedAt     *time.Time     `bun:"finished_at" json:"finishedAt,omitempty"`
}

// SkillUsage is the runtime use telemetry row (one per skill).
type SkillUsage struct {
	bun.BaseModel `bun:"table:kb.skill_usage,alias:su"`

	SkillID       uuid.UUID  `bun:"skill_id,pk,type:uuid" json:"skillId"`
	ProjectID     *uuid.UUID `bun:"project_id,type:uuid" json:"projectId,omitempty"`
	UseCount      int64      `bun:"use_count,notnull" json:"useCount"`
	PatchCount    int64      `bun:"patch_count,notnull" json:"patchCount"`
	LastUsedAt    *time.Time `bun:"last_used_at" json:"lastUsedAt,omitempty"`
	LastPatchedAt *time.Time `bun:"last_patched_at" json:"lastPatchedAt,omitempty"`
	CreatedAt     time.Time  `bun:"created_at,notnull,default:now()" json:"createdAt"`
	UpdatedAt     time.Time  `bun:"updated_at,notnull,default:now()" json:"updatedAt"`
}

// ProposalDTO is the API representation of a staged proposal.
type ProposalDTO struct {
	ID              string        `json:"id"`
	ProjectID       string        `json:"projectId"`
	AgentID         *string       `json:"agentId,omitempty"`
	AgentRunID      *string       `json:"agentRunId,omitempty"`
	AgentName       string        `json:"agentName,omitempty"`
	ReviewID        *string       `json:"reviewId,omitempty"`
	Kind            string        `json:"kind"`
	Name            string        `json:"name"`
	Description     string        `json:"description"`
	Content         string        `json:"content"`
	BaseSkillID     *string       `json:"baseSkillId,omitempty"`
	BaseContentHash string        `json:"baseContentHash,omitempty"`
	PatchOld        string        `json:"patchOld,omitempty"`
	PatchNew        string        `json:"patchNew,omitempty"`
	Evidence        string        `json:"evidence,omitempty"`
	Confidence      float64       `json:"confidence"`
	Source          string        `json:"source"`
	Status          string        `json:"status"`
	AutoApplied     bool          `json:"autoApplied"`
	ReviewNote      string        `json:"reviewNote,omitempty"`
	Findings        []LintFinding `json:"findings,omitempty"`
	CreatedAt       string        `json:"createdAt"`
	AppliedAt       *string       `json:"appliedAt,omitempty"`
}

func strPtr(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}

// ToDTO converts a proposal to its API shape.
func (p *SkillProposal) ToDTO() *ProposalDTO {
	dto := &ProposalDTO{
		ID:              p.ID.String(),
		ProjectID:       p.ProjectID.String(),
		AgentID:         strPtr(p.AgentID),
		AgentRunID:      strPtr(p.AgentRunID),
		AgentName:       p.AgentName,
		ReviewID:        strPtr(p.ReviewID),
		Kind:            p.Kind,
		Name:            p.Name,
		Description:     p.Description,
		Content:         p.Content,
		BaseSkillID:     strPtr(p.BaseSkillID),
		BaseContentHash: p.BaseContentHash,
		PatchOld:        p.PatchOld,
		PatchNew:        p.PatchNew,
		Evidence:        p.Evidence,
		Confidence:      p.Confidence,
		Source:          p.Source,
		Status:          p.Status,
		AutoApplied:     p.AutoApplied,
		ReviewNote:      p.ReviewNote,
		Findings:        p.Findings,
		CreatedAt:       p.CreatedAt.Format(time.RFC3339),
	}
	if p.AppliedAt != nil {
		s := p.AppliedAt.Format(time.RFC3339)
		dto.AppliedAt = &s
	}
	return dto
}

// ApproveProposalDTO is the optional body for approving a proposal.
type ApproveProposalDTO struct {
	Note string `json:"note,omitempty"`
}

// RejectProposalDTO is the body for rejecting a proposal.
type RejectProposalDTO struct {
	Note string `json:"note,omitempty"`
}
