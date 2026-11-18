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
	Status            string           `bun:"status,notnull" json:"status"`
	CreatedAt         time.Time        `bun:"created_at,notnull,default:now()" json:"createdAt"`
	UpdatedAt         time.Time        `bun:"updated_at,notnull,default:now()" json:"updatedAt"`
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
func (s *ReviewGateService) Ensure(ctx context.Context, projectID uuid.UUID, subjectType, subjectRef, headSHA string, properties map[string]any) (*ReviewGate, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("sources: review gate service is not configured")
	}
	headSHA = strings.TrimSpace(headSHA)
	subjectRef = strings.ToLower(strings.TrimSpace(subjectRef))
	if headSHA == "" || subjectRef == "" || subjectType == "" {
		return nil, errors.New("sources: review gate requires subject type, subject ref and head sha")
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
				HeadSHA: headSHA, RequiredReviewers: required, Status: ReviewGatePending,
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
		for _, reviewer := range required {
			if !containsReviewer(gate.RequiredReviewers, reviewer.AgentID) {
				gate.RequiredReviewers = append(gate.RequiredReviewers, reviewer)
				changed = true
			}
		}
		if changed {
			gate.Status = s.evaluate(ctx, tx, &gate)
			gate.UpdatedAt = time.Now().UTC()
			raw, _ := json.Marshal(gate.RequiredReviewers)
			if _, err := tx.NewRaw(`UPDATE kb.review_gates
				SET required_reviewers = ?::jsonb, status = ?, updated_at = now()
				WHERE id = ?`, string(raw), gate.Status, gate.ID).Exec(ctx); err != nil {
				return fmt.Errorf("sources: update review gate: %w", err)
			}
		}
		result = &gate
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// RecordVerdict writes one reviewer's verdict and recomputes the gate. A
// reviewer not in the gate's required snapshot cannot influence it.
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
		gate.Status = s.evaluate(ctx, tx, &gate)
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
	if gate == nil || len(gate.RequiredReviewers) == 0 {
		return ReviewGatePending
	}
	var verdicts []ReviewVerdict
	if db != nil {
		_ = db.NewSelect().Model(&verdicts).Where("gate_id = ?", gate.ID).Scan(ctx)
	}
	byReviewer := make(map[string]string, len(verdicts))
	for _, v := range verdicts {
		byReviewer[v.ReviewerAgentID.String()] = strings.ToUpper(v.Verdict)
	}
	allApproved := true
	for _, reviewer := range gate.RequiredReviewers {
		switch byReviewer[reviewer.AgentID] {
		case "CHANGES_REQUESTED":
			return ReviewGateChangesRequested
		case "MERGE":
		default:
			allApproved = false
		}
	}
	if allApproved {
		return ReviewGateApproved
	}
	return ReviewGatePending
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
