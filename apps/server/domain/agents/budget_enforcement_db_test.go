package agents

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func countRunsForAgent(t *testing.T, ctx context.Context, repo *Repository, agentID string) int {
	t.Helper()
	var count int
	require.NoError(t, repo.db.NewRaw(
		`SELECT COUNT(*) FROM kb.agent_runs WHERE agent_id = ?`, agentID,
	).Scan(ctx, &count))
	return count
}

// TestCreateRunQueued_BudgetBlockedCreatesNoRun verifies the shared queued-run
// surface refuses a capped project before inserting the run or dispatch job.
func TestCreateRunQueued_BudgetBlockedCreatesNoRun(t *testing.T) {
	ctx, db, repo, projectID := setupWorkObjectDispatch(t)
	defID := insertWorkDefinition(t, db, ctx, projectID, "researcher", `{}`)
	agentID := insertWorkAgent(t, db, ctx, projectID, "researcher", defID)

	repo.SetBudgetGuard(&fakeBudgetChecker{exceeded: true}, true, testAgentLogger())

	run, err := repo.CreateRunQueued(ctx, agentID, 1)
	require.Nil(t, run)
	require.True(t, isBudgetExceededError(err))
	require.Zero(t, countRunsForAgent(t, ctx, repo, agentID), "a budget block must not create a run row")

	var jobs int
	require.NoError(t, repo.db.NewRaw(
		`SELECT COUNT(*) FROM kb.agent_run_jobs arj JOIN kb.agent_runs ar ON ar.id = arj.run_id WHERE ar.agent_id = ?`,
		agentID,
	).Scan(ctx, &jobs))
	require.Zero(t, jobs, "a budget block must not create a dispatch job")
}

// TestCreateRunWithOptions_BudgetBlockedCreatesNoRun verifies the pre-created
// run surfaces (HTTP trigger/respond, A2A, chat) cannot insert a row either.
func TestCreateRunWithOptions_BudgetBlockedCreatesNoRun(t *testing.T) {
	ctx, db, repo, projectID := setupWorkObjectDispatch(t)
	defID := insertWorkDefinition(t, db, ctx, projectID, "researcher", `{}`)
	agentID := insertWorkAgent(t, db, ctx, projectID, "researcher", defID)

	repo.SetBudgetGuard(&fakeBudgetChecker{exceeded: true}, true, testAgentLogger())

	run, err := repo.CreateRunWithOptions(ctx, CreateRunOptions{AgentID: agentID})
	require.Nil(t, run)
	require.True(t, isBudgetExceededError(err))
	require.Zero(t, countRunsForAgent(t, ctx, repo, agentID))
}

// TestEnqueueWorkRunDeduped_BudgetBlockedLeavesNoClaim verifies the object-driven
// dispatch enqueue neither claims the dedup slot nor creates a run when the
// project is capped, so a later delivery is not permanently suppressed.
func TestEnqueueWorkRunDeduped_BudgetBlockedLeavesNoClaim(t *testing.T) {
	ctx, db, repo, projectID := setupWorkObjectDispatch(t)
	defID := insertWorkDefinition(t, db, ctx, projectID, "researcher", `{}`)
	agentID := insertWorkAgent(t, db, ctx, projectID, "researcher", defID)

	canonicalID := uuid.NewString()
	insertWorkObject(t, db, ctx, projectID, canonicalID, "budget-1", "ready")
	objType := "ResearchRequest"
	opts := CreateRunQueuedOptions{SubjectObjectID: &canonicalID, SubjectObjectType: &objType}
	logEntry := &AgentProcessingLog{
		AgentID:       agentID,
		GraphObjectID: canonicalID,
		ObjectVersion: 1,
		EventType:     EventTypeCreated,
		Status:        ProcessingStatusPending,
	}

	repo.SetBudgetGuard(&fakeBudgetChecker{exceeded: true}, true, testAgentLogger())

	run, claimed, err := repo.EnqueueWorkRunDeduped(ctx, logEntry, agentID, 1, opts)
	require.Nil(t, run)
	require.False(t, claimed)
	require.True(t, isBudgetExceededError(err))
	require.Zero(t, countRunsForAgent(t, ctx, repo, agentID))

	var logs int
	require.NoError(t, repo.db.NewRaw(
		`SELECT COUNT(*) FROM kb.agent_processing_log WHERE agent_id = ? AND graph_object_id = ?`,
		agentID, canonicalID,
	).Scan(ctx, &logs))
	require.Zero(t, logs, "a budget block must not burn the dedup slot")
}

// TestCreateRunQueued_BudgetQueryErrorFailsOpen verifies a broken budget query
// does not halt dispatch.
func TestCreateRunQueued_BudgetQueryErrorFailsOpen(t *testing.T) {
	ctx, db, repo, projectID := setupWorkObjectDispatch(t)
	defID := insertWorkDefinition(t, db, ctx, projectID, "researcher", `{}`)
	agentID := insertWorkAgent(t, db, ctx, projectID, "researcher", defID)

	checker := &fakeBudgetChecker{err: errors.New("usage query unavailable")}
	repo.SetBudgetGuard(checker, true, testAgentLogger())

	run, err := repo.CreateRunQueued(ctx, agentID, 1)
	require.NoError(t, err)
	require.NotNil(t, run)
	require.Equal(t, 1, checker.calls)
	require.Equal(t, 1, countRunsForAgent(t, ctx, repo, agentID))
}
