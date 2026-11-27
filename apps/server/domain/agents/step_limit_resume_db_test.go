package agents

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/graph"
)

// stepLimitWorkConfig makes a reaction agent pick up a ready ResearchRequest
// with a prompt, so a resumed dispatch has a TriggerMessage to carry the
// continuation note.
const stepLimitWorkConfig = `{
	"status": {"ready": "ready", "inProgress": "in_progress"},
	"objectRules": [{
		"objectType": "ResearchRequest",
		"triggers": [{"status": "ready", "prompt": "Carry on with the research."}]
	}]
}`

// insertStepLimitExhaustedRun seeds a terminal run that stopped at its step
// budget. CountStepLimitExhaustions reads suspend_context, and the reconciler
// only considers the item because the run is not live.
func insertStepLimitExhaustedRun(t *testing.T, db *bun.DB, ctx context.Context, agentID, canonicalID string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.ExecContext(ctx, `
		INSERT INTO kb.agent_runs
			(id, agent_id, status, started_at, completed_at, subject_object_id, suspend_context)
		VALUES (?, ?, ?, now() - interval '2 hours', now(), ?::uuid, '{"reason":"step_limit"}'::jsonb)`,
		id, agentID, string(RunStatusCancelled), canonicalID)
	require.NoError(t, err)
	return id
}

// seedWorkItemState leaves the failure/claim sidecar behind, which a resume must
// clear: the dispatch CASes from it, so a stale row leaves the item busy.
func seedWorkItemState(t *testing.T, db *bun.DB, ctx context.Context, projectID, canonicalID string) {
	t.Helper()
	_, err := db.ExecContext(ctx,
		`INSERT INTO kb.work_item_state (project_id, canonical_id, failure_count, updated_at)
		 VALUES (?::uuid, ?::uuid, 1, now())`,
		projectID, canonicalID)
	require.NoError(t, err)
}

func seedStepLimitBlock(t *testing.T, repo *Repository, ctx context.Context, projectID, canonicalID string) {
	t.Helper()
	class := string(FailureClassStepLimit)
	require.NoError(t, repo.UpsertWorkBlock(ctx, &WorkBlock{
		CanonicalID:  canonicalID,
		ProjectID:    projectID,
		Reason:       "abandoned paused run (step limit, 120/120 steps)",
		FailureClass: &class,
	}))
}

func countWorkEscalations(t *testing.T, db *bun.DB, ctx context.Context, canonicalID string) int {
	t.Helper()
	var count int
	require.NoError(t, db.NewRaw(
		`SELECT COUNT(*) FROM kb.tasks WHERE source_id = ? AND type = 'work-escalation'`,
		canonicalID,
	).Scan(ctx, &count))
	return count
}

// TestWorkReconciler_StepLimitParkResumes pins the resume wiring end to end: a
// step-limit block whose exhaustion count is inside the budget is cleared along
// with the claim state, and exactly one fresh run is queued carrying the
// continuation note. Reverting resumeStepLimitPark, its switch case, or
// CountStepLimitExhaustions turns this red.
func TestWorkReconciler_StepLimitParkResumes(t *testing.T) {
	ctx, db, repo, projectID := setupWorkObjectDispatch(t)
	defID := insertWorkDefinition(t, db, ctx, projectID, "researcher", stepLimitWorkConfig)
	agentID := insertWorkAgent(t, db, ctx, projectID, "researcher", defID)
	_, err := db.ExecContext(ctx,
		`UPDATE kb.agents SET trigger_type = 'reaction', reaction_config = '{"objectTypes":["ResearchRequest"],"events":["created","status_changed"]}'::jsonb WHERE id = ?`,
		agentID)
	require.NoError(t, err)

	canonicalID := uuid.NewString()
	insertWorkObject(t, db, ctx, projectID, canonicalID, "step-limit-resume-1", "ready")
	insertStepLimitExhaustedRun(t, db, ctx, agentID, canonicalID)
	seedWorkItemState(t, db, ctx, projectID, canonicalID)
	seedStepLimitBlock(t, repo, ctx, projectID, canonicalID)

	store := &fakeListStore{heads: []*graph.WorkObjectHead{{
		ProjectID: projectID, CanonicalID: canonicalID, Status: "ready",
		Type: "ResearchRequest", Assignee: "researcher",
	}}}
	rc := NewWorkReconciler(repo, testAgentLogger(), time.Minute)
	rc.SetWorkObjectStore(store)

	rc.reconcile(ctx)

	require.Equal(t, 1, countQueuedRuns(t, db, ctx, agentID, canonicalID),
		"a step-limit park inside the resume budget must dispatch a continuation run")

	block, err := repo.GetWorkBlock(ctx, canonicalID)
	require.NoError(t, err)
	require.Nil(t, block, "the park must be cleared before dispatching")

	state, err := repo.GetWorkItemState(ctx, projectID, canonicalID)
	require.NoError(t, err)
	require.Nil(t, state, "the claim state must be cleared or the dispatch stays busy")

	var msg string
	require.NoError(t, db.NewRaw(
		`SELECT trigger_message FROM kb.agent_runs
		  WHERE agent_id = ? AND subject_object_id = ?::uuid AND status = ?`,
		agentID, canonicalID, string(RunStatusQueued),
	).Scan(ctx, &msg))
	require.Contains(t, msg, "continuation attempt 1 of 2",
		"a resumed run must re-orient from the current state instead of starting over")

	require.Zero(t, countWorkEscalations(t, db, ctx, canonicalID),
		"a resumable park must not escalate to a human")
}

// TestWorkReconciler_StepLimitParkEscalatesPastTheLimit pins the decision the
// reconciler makes once the automatic-resume budget is spent: the block stays,
// no run is dispatched, and exactly one human escalation task is minted (and
// not re-minted on later passes).
func TestWorkReconciler_StepLimitParkEscalatesPastTheLimit(t *testing.T) {
	ctx, db, repo, projectID := setupWorkObjectDispatch(t)
	defID := insertWorkDefinition(t, db, ctx, projectID, "researcher", stepLimitWorkConfig)
	agentID := insertWorkAgent(t, db, ctx, projectID, "researcher", defID)
	_, err := db.ExecContext(ctx,
		`UPDATE kb.agents SET trigger_type = 'reaction', reaction_config = '{"objectTypes":["ResearchRequest"],"events":["created","status_changed"]}'::jsonb WHERE id = ?`,
		agentID)
	require.NoError(t, err)

	canonicalID := uuid.NewString()
	insertWorkObject(t, db, ctx, projectID, canonicalID, "step-limit-exhausted-1", "ready")
	for i := 0; i < stepLimitAutoResumeLimit+1; i++ {
		insertStepLimitExhaustedRun(t, db, ctx, agentID, canonicalID)
	}
	seedWorkItemState(t, db, ctx, projectID, canonicalID)
	seedStepLimitBlock(t, repo, ctx, projectID, canonicalID)

	store := &fakeListStore{heads: []*graph.WorkObjectHead{{
		ProjectID: projectID, CanonicalID: canonicalID, Status: "ready",
		Type: "ResearchRequest", Assignee: "researcher",
	}}}
	rc := NewWorkReconciler(repo, testAgentLogger(), time.Minute)
	rc.SetWorkObjectStore(store)

	rc.reconcile(ctx)

	require.Zero(t, countQueuedRuns(t, db, ctx, agentID, canonicalID),
		"past the resume budget the item must wait for a human, not loop")
	block, err := repo.GetWorkBlock(ctx, canonicalID)
	require.NoError(t, err)
	require.NotNil(t, block, "the block must stay in place for a human")
	require.NotNil(t, block.BlockedBy)
	require.Equal(t, stepLimitExhaustedBlockedBy, *block.BlockedBy)
	require.Equal(t, 1, countWorkEscalations(t, db, ctx, canonicalID),
		"the exhausted park must be discoverable off the board, like a budget park")

	// A second pass must not re-count, re-notify, or mint a duplicate. The
	// marker is what makes a permanently parked item cost nothing per minute.
	rc.reconcile(ctx)
	require.Zero(t, countQueuedRuns(t, db, ctx, agentID, canonicalID))
	require.Equal(t, 1, countWorkEscalations(t, db, ctx, canonicalID),
		"a parked-and-escalated item must not pile up escalation tasks every tick")
}

// TestWorkReconciler_HumanBlockStillNeedsAnUnblock pins the other half of the
// claim: a human (or any non-step-limit) block is a deliberate stop, so the
// reconciler must clear nothing, queue nothing, and escalate nothing.
func TestWorkReconciler_HumanBlockStillNeedsAnUnblock(t *testing.T) {
	ctx, db, repo, projectID := setupWorkObjectDispatch(t)
	defID := insertWorkDefinition(t, db, ctx, projectID, "researcher", stepLimitWorkConfig)
	agentID := insertWorkAgent(t, db, ctx, projectID, "researcher", defID)
	_, err := db.ExecContext(ctx,
		`UPDATE kb.agents SET trigger_type = 'reaction', reaction_config = '{"objectTypes":["ResearchRequest"],"events":["created","status_changed"]}'::jsonb WHERE id = ?`,
		agentID)
	require.NoError(t, err)

	canonicalID := uuid.NewString()
	insertWorkObject(t, db, ctx, projectID, canonicalID, "human-blocked-1", "ready")
	// Even with a step-limit exhaustion on record, the block's class decides:
	// a human stop must never be auto-cleared.
	insertStepLimitExhaustedRun(t, db, ctx, agentID, canonicalID)
	seedWorkItemState(t, db, ctx, projectID, canonicalID)
	class := string(FailureClassHuman)
	require.NoError(t, repo.UpsertWorkBlock(ctx, &WorkBlock{
		CanonicalID:  canonicalID,
		ProjectID:    projectID,
		Reason:       "cancelled by a human",
		FailureClass: &class,
	}))

	store := &fakeListStore{heads: []*graph.WorkObjectHead{{
		ProjectID: projectID, CanonicalID: canonicalID, Status: "ready",
		Type: "ResearchRequest", Assignee: "researcher",
	}}}
	rc := NewWorkReconciler(repo, testAgentLogger(), time.Minute)
	rc.SetWorkObjectStore(store)

	rc.reconcile(ctx)

	require.Zero(t, countQueuedRuns(t, db, ctx, agentID, canonicalID),
		"a human block must not be dispatched until someone unblocks it")
	block, err := repo.GetWorkBlock(ctx, canonicalID)
	require.NoError(t, err)
	require.NotNil(t, block, "the reconciler must leave a human block in place")
	require.NotNil(t, block.FailureClass)
	require.Equal(t, string(FailureClassHuman), *block.FailureClass)
	state, err := repo.GetWorkItemState(ctx, projectID, canonicalID)
	require.NoError(t, err)
	require.NotNil(t, state, "the reconciler must not clear the claim state of a human-blocked item")
	require.Zero(t, countWorkEscalations(t, db, ctx, canonicalID))
}
