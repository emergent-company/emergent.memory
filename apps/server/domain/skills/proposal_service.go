package skills

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// ProposalService applies and rejects staged skill proposals. Apply is the one
// write path: it goes through the normal skills store so embeddings,
// content_hash and provenance stay consistent.
type ProposalService struct {
	repo *Repository
	log  *slog.Logger
}

// NewProposalService creates the proposal applier.
func NewProposalService(repo *Repository, log *slog.Logger) *ProposalService {
	return &ProposalService{repo: repo, log: log.With(logger.Scope("skills.proposals"))}
}

// List returns proposals for a project (status "" means all).
func (s *ProposalService) List(ctx context.Context, projectID uuid.UUID, status string, limit int) ([]*SkillProposal, error) {
	// Sweep rows stranded mid-apply back to pending before reading, so a crash
	// between claim and write does not hide a proposal from the inbox forever.
	if n, err := s.repo.ReleaseStaleProposals(ctx, projectID); err != nil {
		s.log.Warn("failed to release stale proposals", logger.Error(err))
	} else if n > 0 {
		s.log.Info("released stale skill proposals", slog.Int64("count", n))
	}
	rows, err := s.repo.ListProposals(ctx, projectID, status, limit)
	if err != nil {
		return nil, err
	}
	// Compute live findings for pending rows so the reviewer sees the current
	// linter verdict even for proposals staged before a rule changed.
	for _, p := range rows {
		if p.Status == ProposalStatusPending && p.Kind != ProposalKindDelete {
			content, err := s.effectiveContent(ctx, p)
			if err != nil {
				continue
			}
			lint := LintSkill(p.Name, p.Description, content, s.repo.MaxContentSize())
			scan := ScanSkill(p.Name, content)
			p.Findings = append(lint.Findings, scan.Findings...)
		}
	}
	return rows, nil
}

// Get loads one proposal scoped to a project.
func (s *ProposalService) Get(ctx context.Context, projectID, id uuid.UUID) (*SkillProposal, error) {
	p, err := s.repo.FindProposalByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if p.ProjectID != projectID {
		return nil, apperror.NewNotFound("skill proposal", id.String())
	}
	return p, nil
}

// Approve applies a pending proposal on behalf of a human reviewer.
func (s *ProposalService) Approve(ctx context.Context, projectID, proposalID, actorID uuid.UUID, note string) (*Skill, error) {
	if agentActor(ctx) {
		return nil, apperror.ErrForbidden.WithMessage("an agent may not approve its own proposal")
	}
	p, err := s.Get(ctx, projectID, proposalID)
	if err != nil {
		return nil, err
	}
	if p.Status != ProposalStatusPending && !staleApplying(p) {
		return nil, apperror.ErrConflict.WithMessage(fmt.Sprintf("proposal is %s, not pending", p.Status))
	}
	// Security scan is a hard gate even for a human approval: a credential leak
	// must never reach the library by clicking Approve. It judges the bytes the
	// proposal will actually write (patch fields win over the model's content).
	if p.Kind != ProposalKindDelete {
		content, err := s.effectiveContent(ctx, p)
		if err != nil {
			return nil, err
		}
		if scan := ScanSkill(p.Name, content); !scan.Passed() {
			return nil, apperror.ErrValidation.WithMessage("proposal failed the security scan: " + scan.Findings[0].Message)
		}
	}
	return s.apply(ctx, p, &actorID, false, note)
}

// Reject marks a proposal rejected without touching the library.
func (s *ProposalService) Reject(ctx context.Context, projectID, proposalID, actorID uuid.UUID, note string) error {
	if agentActor(ctx) {
		return apperror.ErrForbidden.WithMessage("an agent may not review its own proposal")
	}
	p, err := s.Get(ctx, projectID, proposalID)
	if err != nil {
		return err
	}
	if p.Status != ProposalStatusPending && !staleApplying(p) {
		return apperror.ErrConflict.WithMessage(fmt.Sprintf("proposal is %s, not pending", p.Status))
	}
	ok, err := s.repo.MarkProposalRejected(ctx, p.ID, actorID, note)
	if err != nil {
		return err
	}
	if !ok {
		return apperror.ErrConflict.WithMessage("proposal is no longer reviewable")
	}
	return nil
}

// Apply executes a proposal. auto=true is the background guardrail path and
// tightens (never relaxes) the checks: it refuses non-agent-created targets,
// locked/pinned drafts and deletes.
func (s *ProposalService) Apply(ctx context.Context, p *SkillProposal, actorID *uuid.UUID, auto bool) (*Skill, error) {
	if p == nil {
		return nil, apperror.ErrBadRequest.WithMessage("proposal is required")
	}
	if p.ID != uuid.Nil {
		if fresh, err := s.repo.FindProposalByID(ctx, p.ID); err == nil {
			p = fresh
		}
	}
	if p.Status != ProposalStatusPending && !staleApplying(p) {
		return nil, apperror.ErrConflict.WithMessage(fmt.Sprintf("proposal is %s, not pending", p.Status))
	}
	return s.apply(ctx, p, actorID, auto, "")
}

func (s *ProposalService) apply(ctx context.Context, p *SkillProposal, actorID *uuid.UUID, auto bool, note string) (*Skill, error) {
	// Claim before writing: two concurrent reviewers must not both apply.
	claimed, err := s.repo.ClaimProposal(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	if !claimed {
		return nil, apperror.ErrConflict.WithMessage("proposal is no longer reviewable (another reviewer may be applying it)")
	}

	var applied *Skill
	switch p.Kind {
	case ProposalKindCreate:
		applied, err = s.applyCreate(ctx, p)
	case ProposalKindPatch:
		applied, err = s.applyPatch(ctx, p, auto)
	case ProposalKindDelete:
		err = s.applyDelete(ctx, p, auto)
	default:
		err = apperror.ErrValidation.WithMessage("unknown proposal kind: " + p.Kind)
	}
	if err != nil {
		if auto {
			if markErr := s.repo.MarkProposalFailed(ctx, p.ID, err.Error()); markErr != nil {
				s.log.Warn("failed to mark proposal failed", logger.Error(markErr))
			}
		} else if markErr := s.repo.MarkProposalPending(ctx, p.ID, err.Error()); markErr != nil {
			s.log.Warn("failed to release proposal claim", logger.Error(markErr))
		}
		return nil, err
	}
	if err := s.repo.MarkProposalApplied(ctx, p.ID, actorID, auto, note); err != nil {
		// The mutation landed but the row could not be marked applied: only a
		// human decision that raced a slow apply (rejected/applied meanwhile)
		// reaches here. Surface it loudly — the library changed even though the
		// proposal's recorded status does not say so.
		s.log.Error("skill proposal write landed but the proposal could not be marked applied",
			slog.String("proposal_id", p.ID.String()),
			slog.String("kind", p.Kind),
			logger.Error(err),
		)
		return nil, err
	}
	if note != "" {
		s.log.Info("skill proposal applied", slog.String("proposal_id", p.ID.String()), slog.String("note", note))
	}
	return applied, nil
}

func (s *ProposalService) applyCreate(ctx context.Context, p *SkillProposal) (*Skill, error) {
	dto := CreateSkillDTO{Name: p.Name, Description: p.Description, Content: p.Content}
	if err := ValidateCreateSkill(dto, s.repo.MaxContentSize()); err != nil {
		return nil, err
	}
	if p.AgentName != "" && !strings.HasPrefix(p.Name, AgentSkillPrefix(p.AgentName)) {
		return nil, apperror.ErrValidation.WithMessage("learned skill name must start with the agent prefix")
	}
	meta := &SkillMetadata{
		Source:         SourceAgent,
		CreatedBy:      "agent",
		Author:         p.AgentName,
		LifecycleState: SkillLifecycleActive,
	}
	if p.AgentRunID != nil {
		meta.RunID = p.AgentRunID.String()
	}
	pid := p.ProjectID.String()
	sk := &Skill{
		Name:        p.Name,
		Description: p.Description,
		Content:     p.Content,
		Metadata:    meta,
		ProjectID:   &pid,
	}
	if err := s.repo.Create(ctx, sk); err != nil {
		return nil, err
	}
	return sk, nil
}

func (s *ProposalService) applyPatch(ctx context.Context, p *SkillProposal, auto bool) (*Skill, error) {
	if p.BaseSkillID == nil {
		return nil, apperror.ErrValidation.WithMessage("patch proposal has no target skill")
	}
	base, err := s.repo.FindByID(ctx, *p.BaseSkillID)
	if err != nil {
		return nil, err
	}
	if base.ProjectID == nil || *base.ProjectID != p.ProjectID.String() {
		return nil, apperror.ErrValidation.WithMessage("patch target is not in the proposal's project")
	}
	if auto {
		if !base.IsAgentCreated() || base.IsPinned() || base.IsLocked() ||
			base.LifecycleStateOrDefault() == SkillLifecycleArchived {
			return nil, apperror.ErrForbidden.WithMessage("auto-apply may only patch unpinned, unlocked, agent-created skills")
		}
	}
	if p.BaseContentHash != "" && base.ContentHashOf() != "" && p.BaseContentHash != base.ContentHashOf() {
		return nil, apperror.ErrConflict.WithMessage("skill changed since the proposal was staged; re-review it")
	}

	content, err := s.effectiveContent(ctx, p)
	if err != nil {
		return nil, apperror.ErrValidation.WithMessage(err.Error())
	}
	if strings.TrimSpace(content) == "" {
		return nil, apperror.ErrValidation.WithMessage("patch produced empty content")
	}
	desc := p.Description
	if strings.TrimSpace(desc) == "" {
		desc = base.Description
	}
	meta := SkillMetadata{}
	if base.Metadata != nil {
		meta = *base.Metadata
	}
	meta.LifecycleState = SkillLifecycleActive
	// Provenance of the original author is preserved; a patch does not
	// reassign ownership.
	updated, err := s.repo.Update(ctx, base.ID, &UpdateSkillDTO{
		Description: &desc,
		Content:     &content,
		Metadata:    &meta,
	})
	if err != nil {
		return nil, err
	}
	s.repo.RecordSkillPatch(ctx, base.ID, p.ProjectID.String())
	return updated, nil
}

func (s *ProposalService) applyDelete(ctx context.Context, p *SkillProposal, auto bool) error {
	if auto {
		return apperror.ErrForbidden.WithMessage("delete is never auto-applied")
	}
	if p.BaseSkillID == nil {
		return apperror.ErrValidation.WithMessage("delete proposal has no target skill")
	}
	base, err := s.repo.FindByID(ctx, *p.BaseSkillID)
	if err != nil {
		return err
	}
	if base.ProjectID == nil || *base.ProjectID != p.ProjectID.String() {
		return apperror.ErrValidation.WithMessage("delete target is not in the proposal's project")
	}
	if base.IsLocked() {
		return apperror.ErrForbidden.WithMessage("skill is locked; unlock it before deleting")
	}
	return s.repo.Delete(ctx, base.ID)
}

// agentActor reports whether the call is attributed to an agent run. Approval
// and rejection are human-only; the capture loop applies through Apply, which
// is the one path that may run as the agent.
func agentActor(ctx context.Context) bool {
	actorType, _, ok := auth.ActorFromContext(ctx)
	return ok && strings.EqualFold(actorType, "agent")
}

// staleApplying reports whether a proposal is stuck in `applying` with a claim
// old enough to be reclaimed. The pre-checks use it only to give a clear error;
// the atomic conditional update is the final authority, so a FRESH `applying`
// row is still refused there (the double-review guard). The age threshold is
// proposalClaimStaleAfter, shared with the SQL predicate and the inbox sweep.
func staleApplying(p *SkillProposal) bool {
	if p == nil || p.Status != ProposalStatusApplying {
		return false
	}
	return p.ClaimedAt == nil || p.ClaimedAt.Before(time.Now().Add(-proposalClaimStaleAfter))
}

// effectiveContent returns the bytes the proposal will write. Patch fields win
// over the model's `content`, because applyPatch rewrites the base from them;
// scanning or linting `content` instead would judge different bytes.
func (s *ProposalService) effectiveContent(ctx context.Context, p *SkillProposal) (string, error) {
	if p.Kind == ProposalKindDelete {
		return "", nil
	}
	if p.Kind == ProposalKindPatch && (p.PatchOld != "" || p.PatchNew != "") {
		if p.BaseSkillID == nil {
			return "", apperror.ErrValidation.WithMessage("patch proposal has no target skill")
		}
		base, err := s.repo.FindByID(ctx, *p.BaseSkillID)
		if err != nil {
			return "", err
		}
		return applyPatchContent(base.Content, p.PatchOld, p.PatchNew)
	}
	return p.Content, nil
}
