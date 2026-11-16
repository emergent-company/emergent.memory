package agents

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// TestReleasePausedRunClaim_DBGuardCoversBothClaimStates is the eligibility
// half of the round-4 release fix, against the real schema. The fake-driver
// tests assert the CASE write, but a narrowed guard (WHERE status = 'working',
// dropping 'cancelling' from the IN list) leaves the statement text intact and
// keeps them green — while reintroducing exactly the stranded-cancel bug
// (issue #1166): a CancelTask landing in the claim window moves the row to
// 'cancelling', and if the release cannot match it, the task reports WORKING
// until the 30-minute orphan sweep.
//
// The four cases are the whole state table: both claim states release, a child
// run makes the release a no-op, and a terminal checkpoint is never rewritten.
func TestReleasePausedRunClaim_DBGuardCoversBothClaimStates(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_a2a_release_claim")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	agentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "a2a-release-agent")
	repo := NewRepository(tdb.DB)

	readRun := func(t *testing.T, runID string) (status string, completed bool) {
		t.Helper()
		var completedAt *string
		require.NoError(t, tdb.DB.NewRaw(
			`SELECT status, completed_at::text FROM kb.agent_runs WHERE id = ?`, runID,
		).Scan(ctx, &status, &completedAt))
		return status, completedAt != nil
	}

	t.Run("working claim is restored to the pause", func(t *testing.T) {
		runID := insertAgentRun(t, tdb.DB, ctx, agentID, string(RunStatusRunning), true)

		released, err := repo.ReleasePausedRunClaim(ctx, runID)
		require.NoError(t, err)
		require.True(t, released)

		status, completed := readRun(t, runID)
		require.Equal(t, string(RunStatusPaused), status)
		require.False(t, completed, "a restored pause must not carry completed_at")
	})

	t.Run("cancelling claim is finalized as cancelled", func(t *testing.T) {
		runID := insertAgentRun(t, tdb.DB, ctx, agentID, string(RunStatusCancelling), true)

		released, err := repo.ReleasePausedRunClaim(ctx, runID)
		require.NoError(t, err)
		require.True(t, released,
			"a cancel that landed in the claim window must be finalized, not left for the orphan sweep")

		status, completed := readRun(t, runID)
		require.Equal(t, string(RunStatusCancelled), status)
		require.True(t, completed, "the cancelled branch is terminal and must set completed_at")
	})

	t.Run("a claim with a child run is left alone", func(t *testing.T) {
		runID := insertAgentRun(t, tdb.DB, ctx, agentID, string(RunStatusCancelling), true)
		childID := insertAgentRun(t, tdb.DB, ctx, agentID, string(RunStatusRunning), true)
		_, err := tdb.DB.ExecContext(ctx,
			`UPDATE kb.agent_runs SET resumed_from = ? WHERE id = ?`, runID, childID)
		require.NoError(t, err)

		released, err := repo.ReleasePausedRunClaim(ctx, runID)
		require.NoError(t, err)
		require.False(t, released, "once the child exists the claim is real and must not be released")

		status, completed := readRun(t, runID)
		require.Equal(t, string(RunStatusCancelling), status)
		require.False(t, completed)
	})

	t.Run("a terminal checkpoint is never rewritten", func(t *testing.T) {
		runID := insertAgentRun(t, tdb.DB, ctx, agentID, string(RunStatusSuccess), true)

		released, err := repo.ReleasePausedRunClaim(ctx, runID)
		require.NoError(t, err)
		require.False(t, released)

		status, _ := readRun(t, runID)
		require.Equal(t, string(RunStatusSuccess), status)
	})
}
