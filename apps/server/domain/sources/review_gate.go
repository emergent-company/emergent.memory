package sources

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

const (
	ReviewGatePending          = "pending"
	ReviewGateApproved         = "approved"
	ReviewGateChangesRequested = "changes_requested"
	// ReviewGateUnapprovable is terminal: no verdict can ever be recorded for
	// the head (for example a pull request with no changed files). The required
	// reviewed check must go red with the recorded reason instead of staying
	// in_progress forever (#1022).
	ReviewGateUnapprovable = "unapprovable"

	// ReviewGateMinReviewersDefault is the quorum for a subject authored by a
	// human: one configured reviewer agent is enough.
	ReviewGateMinReviewersDefault = 1

	// ReviewGateMinReviewersSourceAuthored is the quorum for a subject whose
	// author is a source-trigger run (a GitHub App bot login). One reviewer agent
	// would let the reviewing App's own bot verdict satisfy "all reviewers
	// approved" for a PR another App authored, which is the self-approval gap in
	// #1005. A one-agent required set is a configuration error: the gate stays
	// pending instead of turning the required `reviewed` check green. The quorum
	// is agent-level, not a credential check: the review App must still be
	// separate from the coding App, and an unknown author fails closed to it.
	ReviewGateMinReviewersSourceAuthored = 2
)

var (
	ErrReviewGateNotFound  = errors.New("sources: review gate not found")
	ErrReviewerNotRequired = errors.New("sources: reviewer is not required for this gate")
)

// ReviewReviewer is one reviewer agent required for a gate. The snapshot is
// stored on the gate so removing a label later cannot remove a requirement for
// the same head.
type ReviewReviewer struct {
	AgentID string `json:"agentId"`
	Name    string `json:"name"`
}

// ReviewGate is server-owned review aggregation state. It is deliberately not a
// graph object: agents can submit verdicts, but only this row decides whether a
// subject is approved.
type ReviewGate struct {
	bun.BaseModel `bun:"table:kb.review_gates,alias:rg"`

	ID                uuid.UUID        `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	ProjectID         uuid.UUID        `bun:"project_id,type:uuid,notnull" json:"projectId"`
	SubjectType       string           `bun:"subject_type,notnull" json:"subjectType"`
	SubjectRef        string           `bun:"subject_ref,notnull" json:"subjectRef"`
	HeadSHA           string           `bun:"head_sha,notnull" json:"headSha"`
	RequiredReviewers []ReviewReviewer `bun:"required_reviewers,type:jsonb,notnull" json:"requiredReviewers"`
	// MinReviewers is the head-bound quorum snapshotted by Ensure. It is raised
	// (never lowered) when the subject turns out to be source-authored, so an
	// in-flight single-reviewer gate cannot silently satisfy the aggregate.
	MinReviewers int       `bun:"min_reviewers,notnull,default:1" json:"minReviewers"`
	Status       string    `bun:"status,notnull" json:"status"`
	StatusReason string    `bun:"status_reason,notnull,default:''" json:"statusReason"`
	CreatedAt    time.Time `bun:"created_at,notnull,default:now()" json:"createdAt"`
	UpdatedAt    time.Time `bun:"updated_at,notnull,default:now()" json:"updatedAt"`
}

// minimumReviewers returns the gate's effective quorum, treating a zero value (a
// legacy in-memory gate or a row created before the column existed) as the
// default.
//
// A quorum larger than the configured reviewer set can never be met: the gate
// stays pending forever and the required `reviewed` check never turns green. That
// is the deadlock #1005's two-reviewer rule creates for a source-authored subject
// in a project with a single reviewer agent (emergent.memory#1030). Asking every
// configured reviewer to approve is the most the gate can require, so the
// effective quorum is capped at the configured count. A gate with no reviewer
// agent keeps the stored minimum, so an under-configured gate still reports the
// missing reviewer instead of silently approving nobody.
func (g *ReviewGate) minimumReviewers() int {
	if g == nil {
		return ReviewGateMinReviewersDefault
	}
	minReviewers := g.MinReviewers
	if minReviewers < ReviewGateMinReviewersDefault {
		minReviewers = ReviewGateMinReviewersDefault
	}
	if configured := len(g.RequiredReviewers); configured > 0 && minReviewers > configured {
		minReviewers = configured
	}
	return minReviewers
}

// ReviewVerdict is one reviewer's head-bound verdict.
type ReviewVerdict struct {
	bun.BaseModel `bun:"table:kb.review_verdicts,alias:rv"`

	ID              uuid.UUID `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	GateID          uuid.UUID `bun:"gate_id,type:uuid,notnull" json:"gateId"`
	ProjectID       uuid.UUID `bun:"project_id,type:uuid,notnull" json:"projectId"`
	ReviewerAgentID uuid.UUID `bun:"reviewer_agent_id,type:uuid,notnull" json:"reviewerAgentId"`
	ReviewerName    string    `bun:"reviewer_name,notnull" json:"reviewerName"`
	HeadSHA         string    `bun:"head_sha,notnull" json:"headSha"`
	Verdict         string    `bun:"verdict,notnull" json:"verdict"`
	Evidence        string    `bun:"evidence,notnull" json:"evidence"`
	CreatedAt       time.Time `bun:"created_at,notnull,default:now()" json:"createdAt"`
	UpdatedAt       time.Time `bun:"updated_at,notnull,default:now()" json:"updatedAt"`
}

// ReviewGateService coordinates reviewer selection and verdict aggregation.
// It is server-owned and has no dependency on a specific source provider.
type ReviewGateService struct {
	repo   *Repository
	agents *agents.Repository
	log    *slog.Logger
}

func NewReviewGateService(repo *Repository, agentsRepo *agents.Repository, log *slog.Logger) *ReviewGateService {
	return &ReviewGateService{repo: repo, agents: agentsRepo, log: log}
}

// Ensure snapshots the reviewer agents that match the subject and head. Existing
// required reviewers are preserved (sticky for this head).
//
// The gate also snapshots a minimum reviewer count. A pull request authored by a
// source-trigger run (a bot) needs at least two reviewer agents: otherwise the
// reviewing App's own single agent approval is indistinguishable from "all
// reviewers approved" and a bot can approve and auto-merge another bot's PR
// (#1005). An under-configured set is logged as an error and keeps the gate
// pending; it never publishes a green check.
func (s *ReviewGateService) Ensure(ctx context.Context, projectID uuid.UUID, subjectType, subjectRef, headSHA string, properties map[string]any) (*ReviewGate, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("sources: review gate service is not configured")
	}
	headSHA = strings.TrimSpace(headSHA)
	subjectRef = strings.ToLower(strings.TrimSpace(subjectRef))
	if headSHA == "" || subjectRef == "" || subjectType == "" {
		return nil, errors.New("sources: review gate requires subject type, subject ref and head sha")
	}
	minReviewers := reviewGateMinReviewers(subjectType, properties)
	if reviewGateAuthorUnknown(subjectType, properties) && s.log != nil {
		s.log.Warn("sources: review subject has no author metadata; applying the source-authored reviewer quorum",
			"subject", subjectRef, "head_sha", headSHA)
	}
	required := make([]ReviewReviewer, 0)
	if s.agents != nil {
		matched, err := s.agents.FindReviewerAgentsForObject(ctx, projectID.String(), subjectType, properties)
		if err != nil {
			return nil, err
		}
		for _, agent := range matched {
			if agent != nil && agent.ID != "" {
				required = append(required, ReviewReviewer{AgentID: agent.ID, Name: agent.Name})
			}
		}
	}

	var result *ReviewGate
	err := s.repo.projectTx(ctx, projectID, func(ctx context.Context, tx bun.Tx) error {
		var gate ReviewGate
		err := tx.NewSelect().Model(&gate).
			Where("project_id = ?", projectID).
			Where("subject_type = ?", subjectType).
			Where("subject_ref = ?", subjectRef).
			Where("head_sha = ?", headSHA).
			For("UPDATE").
			Scan(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			gate = ReviewGate{
				ProjectID: projectID, SubjectType: subjectType, SubjectRef: subjectRef,
				HeadSHA: headSHA, RequiredReviewers: required, MinReviewers: minReviewers,
				Status: ReviewGatePending,
			}
			if _, err := tx.NewInsert().Model(&gate).Returning("*").Exec(ctx); err != nil {
				return fmt.Errorf("sources: create review gate: %w", err)
			}
			result = &gate
			return nil
		}
		if err != nil {
			return fmt.Errorf("sources: read review gate: %w", err)
		}
		changed := false
		// The quorum only ever rises for a head. A replay that reveals a
		// source-trigger author must re-open a gate that one agent approved
		// before the author was known.
		if minReviewers > gate.MinReviewers {
			gate.MinReviewers = minReviewers
			changed = true
		}
		for _, reviewer := range required {
			if !containsReviewer(gate.RequiredReviewers, reviewer.AgentID) {
				gate.RequiredReviewers = append(gate.RequiredReviewers, reviewer)
				changed = true
			}
		}
		if changed {
			// Unapprovable is terminal for the head: a configuration replay
			// must not turn a terminal gate back into an indefinitely pending
			// one (#1022).
			if gate.Status != ReviewGateUnapprovable {
				gate.Status = s.evaluate(ctx, tx, &gate)
			}
			gate.UpdatedAt = time.Now().UTC()
			raw, _ := json.Marshal(gate.RequiredReviewers)
			if _, err := tx.NewRaw(`UPDATE kb.review_gates
				SET required_reviewers = ?::jsonb, min_reviewers = ?, status = ?, updated_at = now()
				WHERE id = ?`, string(raw), gate.MinReviewers, gate.Status, gate.ID).Exec(ctx); err != nil {
				return fmt.Errorf("sources: update review gate: %w", err)
			}
		}
		result = &gate
		return nil
	})
	if err != nil {
		return nil, err
	}
	if result != nil && len(result.RequiredReviewers) < result.minimumReviewers() && s.log != nil {
		s.log.Error("sources: review gate is under-configured; the reviewed check cannot turn green",
			"subject", subjectRef,
			"head_sha", headSHA,
			"required_reviewers", len(result.RequiredReviewers),
			"min_reviewers", result.minimumReviewers(),
			"hint", "add another reviewer agent for a source-authored pull request (#1005)")
	}
	return result, nil
}

// reviewGateMinReviewers derives the quorum from the subject's author metadata.
// A GitHub App bot author is a source-trigger run; a human author keeps the
// default single-reviewer gate. Unknown author metadata fails closed: an older
// GitHubPullRequest row or an Ensure caller that does not forward the author
// props must not silently fall back to the one-reviewer rule this change exists
// to remove (#1005). The login suffix is a fallback for payloads where only the
// author login is available.
func reviewGateMinReviewers(subjectType string, properties map[string]any) int {
	if !strings.EqualFold(strings.TrimSpace(subjectType), "GitHubPullRequest") {
		return ReviewGateMinReviewersDefault
	}
	authorType, _ := properties["author_type"].(string)
	author, _ := properties["author"].(string)
	authorType = strings.TrimSpace(authorType)
	author = strings.TrimSpace(author)
	if authorType == "" && author == "" {
		return ReviewGateMinReviewersSourceAuthored
	}
	if strings.EqualFold(authorType, "Bot") {
		return ReviewGateMinReviewersSourceAuthored
	}
	if strings.HasSuffix(strings.ToLower(author), "[bot]") {
		return ReviewGateMinReviewersSourceAuthored
	}
	return ReviewGateMinReviewersDefault
}

// reviewGateAuthorUnknown reports whether a GitHubPullRequest subject carries no
// author identity. Ensure logs it so the stricter fail-closed quorum is visible
// rather than silently chosen.
func reviewGateAuthorUnknown(subjectType string, properties map[string]any) bool {
	if !strings.EqualFold(strings.TrimSpace(subjectType), "GitHubPullRequest") {
		return false
	}
	authorType, _ := properties["author_type"].(string)
	author, _ := properties["author"].(string)
	return strings.TrimSpace(authorType) == "" && strings.TrimSpace(author) == ""
}

// MarkUnapprovable moves a head's gate to the terminal unapprovable state with
// a reason. It is how a review tool reports that the head can never be approved:
// it has no changed files, GitHub withholds a changed file's content, a patch is
// larger than any re-read can render, or the file listing hit GitHub's
// pagination cap. The gate and the required reviewed check then stop waiting
// forever for evidence that can never exist; a rejection is still recordable
// (#1022, #1037).
//
// A missing gate is reported as ErrReviewGateNotFound so legacy sources keep
// their old path. Any other error is a real write failure.
func (s *ReviewGateService) MarkUnapprovable(ctx context.Context, projectID uuid.UUID, subjectType, subjectRef, headSHA, reason string) (*ReviewGate, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("sources: review gate service is not configured")
	}
	subjectRef = strings.ToLower(strings.TrimSpace(subjectRef))
	headSHA = strings.TrimSpace(headSHA)
	reason = strings.TrimSpace(reason)
	if subjectType == "" || subjectRef == "" || headSHA == "" {
		return nil, apperror.NewValidation("sources: unapprovable review gate requires subject type, subject ref and head sha")
	}
	if reason == "" {
		reason = "No verdict can be recorded for this head."
	}
	var result *ReviewGate
	err := s.repo.projectTx(ctx, projectID, func(ctx context.Context, tx bun.Tx) error {
		var gate ReviewGate
		err := tx.NewSelect().Model(&gate).
			Where("project_id = ?", projectID).
			Where("subject_type = ?", subjectType).
			Where("subject_ref = ?", subjectRef).
			Where("head_sha = ?", headSHA).
			For("UPDATE").
			Scan(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrReviewGateNotFound
		}
		if err != nil {
			return fmt.Errorf("sources: read review gate: %w", err)
		}
		gate.Status = ReviewGateUnapprovable
		gate.StatusReason = reason
		gate.UpdatedAt = time.Now().UTC()
		if _, err := tx.NewUpdate().Model(&gate).
			Set("status = ?", gate.Status).
			Set("status_reason = ?", gate.StatusReason).
			Set("updated_at = ?", gate.UpdatedAt).
			Where("id = ?", gate.ID).Exec(ctx); err != nil {
			return fmt.Errorf("sources: mark review gate unapprovable: %w", err)
		}
		result = &gate
		return nil
	})
	if err != nil {
		if result != nil {
			return result, err
		}
		return nil, err
	}
	if s.log != nil {
		s.log.Error("sources: review gate is unapprovable; the reviewed check cannot turn green",
			"subject", subjectRef, "head_sha", headSHA, "reason", reason)
	}
	return result, nil
}

// RecordVerdict writes one reviewer's verdict and recomputes the gate. A
// reviewer not in the gate's required snapshot cannot influence it. A terminal
// unapprovable head accepts no approval: the gate has no fully reviewable diff,
// so a late MERGE cannot flip it back to pending or approved (#1022). A
// rejection authorizes nothing and stays recordable, so a reviewer can still say
// what is wrong with a head whose content GitHub would not return (#1037).
func (s *ReviewGateService) RecordVerdict(ctx context.Context, projectID uuid.UUID, subjectType, subjectRef, headSHA, reviewerAgentID, reviewerName, verdict, evidence string) (*ReviewGate, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("sources: review gate service is not configured")
	}
	subjectRef = strings.ToLower(strings.TrimSpace(subjectRef))
	reviewerID, err := uuid.Parse(strings.TrimSpace(reviewerAgentID))
	if err != nil {
		return nil, apperror.NewValidationf("sources: invalid reviewer agent id: %v", err)
	}
	verdict = strings.ToUpper(strings.TrimSpace(verdict))
	if verdict != "MERGE" && verdict != "CHANGES_REQUESTED" {
		return nil, apperror.NewValidationf("sources: invalid review verdict %q", verdict)
	}
	var result *ReviewGate
	err = s.repo.projectTx(ctx, projectID, func(ctx context.Context, tx bun.Tx) error {
		var gate ReviewGate
		err := tx.NewSelect().Model(&gate).
			Where("project_id = ?", projectID).
			Where("subject_type = ?", subjectType).
			Where("subject_ref = ?", subjectRef).
			Where("head_sha = ?", headSHA).
			For("UPDATE").
			Scan(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrReviewGateNotFound
		}
		if err != nil {
			return fmt.Errorf("sources: read review gate: %w", err)
		}
		// Unapprovable is terminal for the head: only an approval can turn the
		// required check green, so only an approval is refused. The rejection is
		// still recorded for audit and keeps the terminal status and reason.
		terminalUnapprovable := gate.Status == ReviewGateUnapprovable
		if terminalUnapprovable && verdict == "MERGE" {
			result = &gate
			return apperror.NewValidationf("sources: review gate for %s@%s is unapprovable: %s", subjectRef, headSHA, reviewGateUnapprovableSummary(&gate))
		}
		if !containsReviewer(gate.RequiredReviewers, reviewerID.String()) {
			result = &gate
			return fmt.Errorf("%w: %s", ErrReviewerNotRequired, reviewerID)
		}
		if reviewerName == "" {
			reviewerName = reviewerID.String()
		}
		if _, err := tx.NewRaw(`
			INSERT INTO kb.review_verdicts
				(gate_id, project_id, reviewer_agent_id, reviewer_name, head_sha, verdict, evidence)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (gate_id, reviewer_agent_id) DO UPDATE SET
				reviewer_name = EXCLUDED.reviewer_name,
				head_sha = EXCLUDED.head_sha,
				verdict = EXCLUDED.verdict,
				evidence = EXCLUDED.evidence,
				updated_at = now()
		`, gate.ID, projectID, reviewerID, reviewerName, headSHA, verdict, evidence).Exec(ctx); err != nil {
			return fmt.Errorf("sources: record review verdict: %w", err)
		}
		if !terminalUnapprovable {
			gate.Status = s.evaluate(ctx, tx, &gate)
		}
		gate.UpdatedAt = time.Now().UTC()
		if _, err := tx.NewUpdate().Model(&gate).
			Set("status = ?", gate.Status).
			Set("updated_at = ?", gate.UpdatedAt).
			Where("id = ?", gate.ID).Exec(ctx); err != nil {
			return fmt.Errorf("sources: update review gate status: %w", err)
		}
		result = &gate
		return nil
	})
	if err != nil {
		if result != nil {
			return result, err
		}
		return nil, err
	}
	return result, nil
}

func (s *ReviewGateService) evaluate(ctx context.Context, db bun.IDB, gate *ReviewGate) string {
	if gate == nil {
		return ReviewGatePending
	}
	byReviewer := make(map[string]string, len(gate.RequiredReviewers))
	if db != nil && len(gate.RequiredReviewers) > 0 {
		var verdicts []ReviewVerdict
		_ = db.NewSelect().Model(&verdicts).Where("gate_id = ?", gate.ID).Scan(ctx)
		for _, v := range verdicts {
			byReviewer[v.ReviewerAgentID.String()] = strings.ToUpper(v.Verdict)
		}
	}
	return reviewGateStatus(gate.RequiredReviewers, gate.minimumReviewers(), byReviewer)
}

// reviewGateStatus aggregates head-bound verdicts against the required reviewer
// snapshot and its quorum. A rejection wins even when the quorum is unmet (the
// check must go red, not stay pending); approval requires every required
// reviewer plus at least minReviewers distinct agents.
func reviewGateStatus(required []ReviewReviewer, minReviewers int, byReviewer map[string]string) string {
	if minReviewers < ReviewGateMinReviewersDefault {
		minReviewers = ReviewGateMinReviewersDefault
	}
	if len(required) == 0 {
		return ReviewGatePending
	}
	for _, reviewer := range required {
		if byReviewer[reviewer.AgentID] == "CHANGES_REQUESTED" {
			return ReviewGateChangesRequested
		}
	}
	if len(required) < minReviewers {
		return ReviewGatePending
	}
	for _, reviewer := range required {
		if byReviewer[reviewer.AgentID] != "MERGE" {
			return ReviewGatePending
		}
	}
	return ReviewGateApproved
}

func containsReviewer(reviewers []ReviewReviewer, id string) bool {
	for _, reviewer := range reviewers {
		if reviewer.AgentID == id {
			return true
		}
	}
	return false
}

// RequiredReviewersJSON is used by tests and callers that need a stable shape.
func RequiredReviewersJSON(reviewers []ReviewReviewer) []byte {
	raw, _ := json.Marshal(reviewers)
	return raw
}
