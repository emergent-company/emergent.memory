package agents

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func budgetBlocked(err error) bool {
	if err == nil {
		return false
	}
	_, ok := errors.AsType[*BudgetExceededError](err)
	return ok
}

func countRunsForAgent(t *testing.T, ctx context.Context, repo *Repository, agentID string) int {
	t.Helper()
	var count int
	require.NoError(t, repo.db.NewRaw(
		`SELECT COUNT(*) FROM kb.agent_runs WHERE agent_id = ?`, agentID,
	).Scan(ctx, &count))
	return count
}

// TestCreateRunWithOptions_BudgetBlockedCreatesNoRun verifies the pre-created
// run surfaces (HTTP trigger/respond, A2A, chat) cannot insert a row either.
func TestCreateRunWithOptions_BudgetBlockedCreatesNoRun(t *testing.T) {
	ctx, db, repo, projectID := setupWorkObjectDispatch(t)
	defID := insertWorkDefinition(t, db, ctx, projectID, "researcher", `{}`)
	agentID := insertWorkAgent(t, db, ctx, projectID, "researcher", defID)

	repo.SetBudgetGate(&fakeBudgetChecker{exceeded: true}, true)

	run, err := repo.CreateRunWithOptions(ctx, CreateRunOptions{AgentID: agentID})
	require.Nil(t, run)
	require.True(t, budgetBlocked(err))
	require.Zero(t, countRunsForAgent(t, ctx, repo, agentID))
}

// TestCreateRunQueued_BudgetQueryErrorFailsOpen verifies a broken budget query
// does not halt dispatch.
func TestCreateRunQueued_BudgetQueryErrorFailsOpen(t *testing.T) {
	ctx, db, repo, projectID := setupWorkObjectDispatch(t)
	defID := insertWorkDefinition(t, db, ctx, projectID, "researcher", `{}`)
	agentID := insertWorkAgent(t, db, ctx, projectID, "researcher", defID)

	checker := &fakeBudgetChecker{err: errors.New("usage query unavailable")}
	repo.SetBudgetGate(checker, true)

	run, err := repo.CreateRunQueued(ctx, agentID, 1)
	require.NoError(t, err)
	require.NotNil(t, run)
	require.Equal(t, 1, checker.calls)
	require.Equal(t, 1, countRunsForAgent(t, ctx, repo, agentID))
}
