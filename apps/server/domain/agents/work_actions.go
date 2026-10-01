package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/domain/tasks"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// WorkActionService implements the human-action write path for object-driven
// work items (P3): approve, request-changes, retry, reassign, and cancel. Every
// status move flows through the graph single-work-status writer; the service
// only adds the surrounding bookkeeping (review columns, append-only feedback,
// rework/retry enqueue, escalation tasks, and in-flight-run cancellation).
type WorkActionService struct {
	repo        *Repository
	workObjects WorkObjectStore
	log         *slog.Logger
}

// NewWorkActionService creates the human-action service.
func NewWorkActionService(repo *Repository, log *slog.Logger) *WorkActionService {
	return &WorkActionService{repo: repo, log: log.With(slog.String("component", "work-actions"))}
}

// SetWorkObjectStore injects the graph work-object surface after construction.
func (s *WorkActionService) SetWorkObjectStore(store WorkObjectStore) {
	s.workObjects = store
}

// WorkActionResult is the outcome of a human action, returned to the handler
// for response shaping. Round is the new feedback round (request-changes);
// Escalated reports that the revision cap was reached and no rework was
// re-enqueued; RunID is the rework/retry run when one was enqueued.
type WorkActionResult struct {
	Head      *graph.WorkObjectHead
	Round     int
	Escalated bool
	RunID     string
}

// resolveWorkContext resolves the work object's HEAD and its owning agent and
// definition (so the action uses the agent's configured status values). The
// agent is resolved from the latest subject run, falling back to the assignee.
func (s *WorkActionService) resolveWorkContext(ctx context.Context, projectID, canonicalID string) (*graph.WorkObjectHead, *Agent, *AgentDefinition, error) {
	head, err := s.workObjects.GetHeadObject(ctx, projectID, canonicalID)
	if err != nil {
		return nil, nil, nil, err
	}
	if head == nil {
		return nil, nil, nil, apperror.NewNotFound("WorkItem", canonicalID)
	}
	var agent *Agent
	if run, err := s.repo.FindLatestRunForSubject(ctx, canonicalID); err != nil {
		return nil, nil, nil, err
	} else if run != nil {
		agent, _ = s.repo.FindByID(ctx, run.AgentID, &projectID)
	}
	if agent == nil && head.Assignee != "" {
		agent, _ = s.repo.FindByName(ctx, projectID, head.Assignee)
	}
	var def *AgentDefinition
	if agent != nil {
		if d, err := s.repo.ResolveDefinitionForAgent(ctx, agent); err != nil {
			return nil, nil, nil, err
		} else {
			def = d
		}
	}
	return head, agent, def, nil
}

// Approve finalizes a review: review→done, sets reviewed_by/reviewed_at, clears
// needs_review, and clears the failure ledger. Returns a conflict when the item
// is not in the review status.
func (s *WorkActionService) Approve(ctx context.Context, projectID, canonicalID, reviewerID string) (*WorkActionResult, error) {
	_, _, def, err := s.resolveWorkContext(ctx, projectID, canonicalID)
	if err != nil {
		return nil, err
	}
	wc := workConfigOf(def)
	ok, err := s.workObjects.ApproveWorkObject(ctx, projectID, canonicalID, wc.ReviewStatus(), wc.DoneStatus(), reviewerID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, apperror.New(http.StatusConflict, "conflict", "work item is not in the review status")
	}
	_ = s.repo.ClearWorkItemState(ctx, projectID, canonicalID)
	head, err := s.workObjects.GetHeadObject(ctx, projectID, canonicalID)
	if err != nil {
		return nil, err
	}
	return &WorkActionResult{Head: head}, nil
}

// RequestChanges sends a review back for rework: review→revision, records an
// append-only feedback round (non-empty required), and enqueues a rework run
// carrying all prior feedback. Once the revision cap is reached it escalates to
// a human (kb.tasks) and does NOT re-enqueue.
func (s *WorkActionService) RequestChanges(ctx context.Context, projectID, canonicalID, author, text string, runID *string) (*WorkActionResult, error) {
	head, agent, def, err := s.resolveWorkContext(ctx, projectID, canonicalID)
	if err != nil {
		return nil, err
	}
	if text == "" {
		return nil, apperror.New(http.StatusBadRequest, "bad_request", "feedback is required")
	}
	wc := workConfigOf(def)
	ok, err := s.workObjects.RequestChangesWorkObject(ctx, projectID, canonicalID, wc.ReviewStatus(), wc.RevisionStatus())
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, apperror.New(http.StatusConflict, "conflict", "work item is not in the review status")
	}

	round, err := s.repo.AppendWorkFeedback(ctx, projectID, canonicalID, author, text, runID)
	if err != nil {
		return nil, apperror.NewInternal("failed to record work feedback", err)
	}
	feedback, err := s.repo.ListWorkFeedback(ctx, projectID, canonicalID)
	if err != nil {
		return nil, apperror.NewInternal("failed to load work feedback", err)
	}
	feedbackList := feedbackToMaps(feedback)

	res := &WorkActionResult{Round: round}
	res.Head, err = s.workObjects.GetHeadObject(ctx, projectID, canonicalID)
	if err != nil {
		return nil, err
	}

	if round >= wc.RevisionLimitValue() {
		s.escalateRevisionCap(ctx, projectID, canonicalID, feedbackList)
		res.Escalated = true
		return res, nil
	}

	if agent == nil {
		s.log.Warn("request-changes: no agent resolved to rework the item; escalating",
			slog.String("canonical_id", canonicalID))
		s.escalateRevisionCap(ctx, projectID, canonicalID, feedbackList)
		res.Escalated = true
		return res, nil
	}
	runIDEnqueued, err := s.enqueueWorkRun(ctx, head, agent, def, feedbackList)
	if err != nil {
		return nil, apperror.NewInternal("failed to enqueue rework run", err)
	}
	res.RunID = runIDEnqueued
	return res, nil
}

// Retry returns a blocked work item to ready and enqueues it, clearing the
// failure ledger. Returns a conflict when the item is not blocked.
func (s *WorkActionService) Retry(ctx context.Context, projectID, canonicalID string) (*WorkActionResult, error) {
	head, agent, def, err := s.resolveWorkContext(ctx, projectID, canonicalID)
	if err != nil {
		return nil, err
	}
	wc := workConfigOf(def)
	ok, err := s.workObjects.TransitionWorkObject(ctx, projectID, canonicalID, graph.WorkObjectTransition{
		FromStatus: wc.BlockedStatus(),
		ToStatus:   wc.ReadyStatus(),
	})
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, apperror.New(http.StatusConflict, "conflict", "work item is not in the blocked status")
	}
	_ = s.repo.ClearWorkItemState(ctx, projectID, canonicalID)

	res := &WorkActionResult{}
	if agent != nil {
		if runID, err := s.enqueueWorkRun(ctx, head, agent, def, nil); err != nil {
			s.log.Warn("retry: enqueue failed; reconciler will retry", slog.String("canonical_id", canonicalID), slog.String("error", err.Error()))
		} else {
			res.RunID = runID
		}
	}
	res.Head, err = s.workObjects.GetHeadObject(ctx, projectID, canonicalID)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// Reassign sets or clears the assignee on a work item without a status move. An
// empty assignee clears it. A ready item is subsequently routed by the
// reconciler to the new assignee (or any listener).
func (s *WorkActionService) Reassign(ctx context.Context, projectID, canonicalID string, assignee *string) (*WorkActionResult, error) {
	ok, err := s.workObjects.ReassignWorkObject(ctx, projectID, canonicalID, assignee)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, apperror.New(http.StatusConflict, "conflict", "work item not found or missing key")
	}
	head, err := s.workObjects.GetHeadObject(ctx, projectID, canonicalID)
	if err != nil {
		return nil, err
	}
	return &WorkActionResult{Head: head}, nil
}

// Cancel closes a work item: it transitions the item to blocked and cancels any
// in-flight (live) run. Done items are not cancellable.
func (s *WorkActionService) Cancel(ctx context.Context, projectID, canonicalID string) (*WorkActionResult, error) {
	head, _, def, err := s.resolveWorkContext(ctx, projectID, canonicalID)
	if err != nil {
		return nil, err
	}
	wc := workConfigOf(def)
	if head.Status == wc.DoneStatus() {
		return nil, apperror.New(http.StatusConflict, "conflict", "work item is already done")
	}
	ok, err := s.workObjects.CancelWorkObject(ctx, projectID, canonicalID, wc.BlockedStatus())
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, apperror.New(http.StatusConflict, "conflict", "work item not found or missing key")
	}
	if runs, err := s.repo.FindLiveRunsForSubject(ctx, canonicalID); err != nil {
		s.log.Warn("cancel: failed to find live runs", slog.String("canonical_id", canonicalID), slog.String("error", err.Error()))
	} else {
		for _, r := range runs {
			if _, err := s.repo.RequestRunCancellation(ctx, r.ID); err != nil {
				s.log.Warn("cancel: failed to cancel run", slog.String("run_id", r.ID), slog.String("error", err.Error()))
			}
		}
	}
	res := &WorkActionResult{}
	res.Head, err = s.workObjects.GetHeadObject(ctx, projectID, canonicalID)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// enqueueWorkRun enqueues a fresh run for the subject work object, optionally
// carrying the full rework feedback history in the trigger metadata.
func (s *WorkActionService) enqueueWorkRun(ctx context.Context, head *graph.WorkObjectHead, agent *Agent, agentDef *AgentDefinition, feedback []map[string]any) (string, error) {
	if agent == nil {
		return "", fmt.Errorf("no agent to enqueue work run")
	}
	maxAttempts := 1
	if agentDef != nil && agentDef.WorkConfig.RetryPolicy.MaxAttempts > 0 {
		maxAttempts = agentDef.WorkConfig.RetryPolicy.MaxAttempts
	}
	opts := CreateRunQueuedOptions{
		SubjectObjectID:   &head.CanonicalID,
		SubjectObjectType: &head.Type,
		TrustedInternal:   true,
	}
	if len(feedback) > 0 {
		opts.TriggerMetadata = map[string]any{"work_feedback": feedback}
	}
	run, err := s.repo.CreateRunQueued(ctx, agent.ID, maxAttempts, opts)
	if err != nil {
		return "", err
	}
	return run.ID, nil
}

// escalateRevisionCap creates a human-facing kb.tasks escalation carrying the
// item reference and the full feedback history.
func (s *WorkActionService) escalateRevisionCap(ctx context.Context, projectID, canonicalID string, feedback []map[string]any) {
	desc := "Work item reached the revision cap and was escalated for human review."
	meta, _ := json.Marshal(map[string]any{
		"canonical_id": canonicalID,
		"reason":       "revision_cap_reached",
		"feedback":     feedback,
	})
	t := &tasks.Task{
		ProjectID:   projectID,
		Title:       "Work item needs review (revision cap reached)",
		Description: &desc,
		Type:        "work-escalation",
		Status:      "pending",
		SourceType:  strPtr("work_object"),
		SourceID:    &canonicalID,
		Metadata:    meta,
	}
	if _, err := s.repo.CreateWorkTask(ctx, t); err != nil {
		s.log.Warn("failed to create revision-cap escalation task", slog.String("canonical_id", canonicalID), slog.String("error", err.Error()))
	}
}

// feedbackToMaps converts the stored feedback history into the wire shape
// carried in rework metadata and escalation tasks.
func feedbackToMaps(feedback []*WorkItemFeedback) []map[string]any {
	out := make([]map[string]any, 0, len(feedback))
	for _, f := range feedback {
		if f == nil {
			continue
		}
		m := map[string]any{
			"round": f.Round,
			"text":  f.Text,
		}
		if f.Author != nil {
			m["author"] = *f.Author
		}
		if f.RunID != nil {
			m["runId"] = *f.RunID
		}
		out = append(out, m)
	}
	return out
}
