package agents

import (
	"context"
	"fmt"
	"time"
)

// pausedWorkRunCandidate is a paused (input-required) work run that nothing can
// wake: it has no pending question, is not waiting on a child or a tool
// confirmation, and has been idle past the park threshold. The reaper cancels
// it and blocks its work item so the run stops holding the subject's claim.
type pausedWorkRunCandidate struct {
	RunID           string
	ProjectID       string
	SubjectObjectID string
	StepCount       int
	MaxSteps        *int
	// PauseReason is the persisted suspend marker (empty when the executor never
	// wrote one). It distinguishes a step-limit park from an unidentified pause
	// in the block reason; both are reaped because nothing is pending.
	PauseReason string
}

// ListAbandonedPausedWorkRuns returns paused work runs whose last activity is
// older than olderThan and that nothing can resume on their own (issue #993).
//
// A paused run is deliberately live for claim purposes: a human answering an
// ask_user question or approving a tool call must not have the item reclaimed
// and run twice. But a pause with *nothing pending* has no owner at all — the
// step-limit pause is the production case — and because it stays non-terminal it
// suppresses the dispatch reconciler for ever. This query finds exactly those
// runs:
//
//   - the run is paused and linked to a work subject;
//   - its last activity is past the threshold;
//   - no pending question exists (an ask_user pause is a human's to answer);
//   - its suspend marker is not one of the waiting reasons (child run, tool
//     confirmation, client tool), so a pause that does expect a resume is
//     spared.
//
// A run with no suspend marker and no pending question is included: nothing can
// wake it, and the marker being missing is exactly the failure mode that would
// otherwise leak for ever (issue #867).
func (r *Repository) ListAbandonedPausedWorkRuns(ctx context.Context, olderThan time.Duration, limit int) ([]*pausedWorkRunCandidate, error) {
	if olderThan <= 0 {
		return nil, fmt.Errorf("ListAbandonedPausedWorkRuns: olderThan must be positive")
	}
	if limit <= 0 {
		limit = 500
	}
	cutoff := time.Now().Add(-olderThan)
	var rows []struct {
		RunID           string `bun:"run_id"`
		ProjectID       string `bun:"project_id"`
		SubjectObjectID string `bun:"subject_object_id"`
		StepCount       int    `bun:"step_count"`
		MaxSteps        *int   `bun:"max_steps"`
		PauseReason     string `bun:"pause_reason"`
	}
	err := r.db.NewRaw(`
		SELECT ar.id                                       AS run_id,
		       a.project_id                                AS project_id,
		       ar.subject_object_id                        AS subject_object_id,
		       ar.step_count                               AS step_count,
		       ar.max_steps                                AS max_steps,
		       COALESCE(ar.suspend_context->>'reason', '') AS pause_reason
		  FROM kb.agent_runs ar
		  JOIN kb.agents a ON a.id = ar.agent_id
		 WHERE ar.status = ?
		   AND ar.subject_object_id IS NOT NULL
		   AND COALESCE(ar.completed_at, ar.last_step_at, ar.started_at) < ?
		   AND NOT EXISTS (
		       SELECT 1
		         FROM kb.agent_questions q
		        WHERE q.run_id = ar.id
		          AND q.status = ?
		   )
		   AND COALESCE(ar.suspend_context->>'reason', '') NOT IN (?, ?, ?, ?)
		 ORDER BY COALESCE(ar.completed_at, ar.last_step_at, ar.started_at) ASC
		 LIMIT ?`,
		string(RunStatusPaused),
		cutoff,
		string(QuestionStatusPending),
		string(SuspendReasonAwaitingHuman),
		string(SuspendReasonAwaitingToolConfirm),
		string(SuspendReasonAwaitingClientTool),
		string(SuspendReasonAwaitingChild),
		limit,
	).Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	out := make([]*pausedWorkRunCandidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, &pausedWorkRunCandidate{
			RunID:           row.RunID,
			ProjectID:       row.ProjectID,
			SubjectObjectID: row.SubjectObjectID,
			StepCount:       row.StepCount,
			MaxSteps:        row.MaxSteps,
			PauseReason:     row.PauseReason,
		})
	}
	return out, nil
}

// abandonedPausedWorkRunBlockReason is the human-facing reason recorded on a
// work item whose paused run was reaped. It names the condition (step budget
// exhaustion where known) so the board says why the item stopped rather than
// showing it as untouched (issue #993).
func abandonedPausedWorkRunBlockReason(c *pausedWorkRunCandidate, idleFor time.Duration) string {
	if c.PauseReason == string(SuspendReasonStepLimit) {
		if c.MaxSteps != nil && *c.MaxSteps > 0 {
			return fmt.Sprintf(
				"Agent run %s exhausted its step limit (%d/%d steps) and was not resumed within %s; unblock or retry to run the item again.",
				c.RunID, c.StepCount, *c.MaxSteps, idleFor,
			)
		}
		return fmt.Sprintf(
			"Agent run %s exhausted its step limit after %d steps and was not resumed within %s; unblock or retry to run the item again.",
			c.RunID, c.StepCount, idleFor,
		)
	}
	return fmt.Sprintf(
		"Agent run %s paused with nothing pending and was not resumed within %s; unblock or retry to run the item again.",
		c.RunID, idleFor,
	)
}
