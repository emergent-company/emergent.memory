package agents

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// TestMarkStaleRunsAsError_HeartbeatSparesActiveRun exercises the idle-timeout
// reaper against a real schema built from the embedded migrations (applied to
// head by internal/testdb):
//
//   - a running run with a fresh heartbeat survives the sweep,
//   - a running run whose start AND heartbeat are stale is reaped,
//   - a running run that never heartbeated falls back to started_at via
//     COALESCE and is reaped,
//   - a never-started queued run is never touched (regression guard for the
//     "queued, not stale" class of false failures).
func TestMarkStaleRunsAsError_HeartbeatSparesActiveRun(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_stale_reaper")
	t.Cleanup(tdb.Close)

	ctx := context.Background()
	db := tdb.DB

	orgID := uuid.NewString()
	_, err := db.ExecContext(ctx,
		`INSERT INTO kb.orgs (id, name) VALUES (?, ?)`,
		orgID, "reaper-test-org")
	require.NoError(t, err)

	projectID := uuid.NewString()
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`,
		projectID, orgID, "reaper-test-project")
	require.NoError(t, err)

	agentID := uuid.NewString()
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id) VALUES (?,?,?,?,?)`,
		agentID, "reaper-test", "graph", "", projectID)
	require.NoError(t, err)

	now := time.Now()
	insertRun := func(status AgentRunStatus, startedAt time.Time, lastStepAt *time.Time) string {
		id := uuid.NewString()
		_, insErr := db.ExecContext(ctx,
			`INSERT INTO kb.agent_runs (id, agent_id, status, started_at, last_step_at) VALUES (?,?,?,?,?)`,
			id, agentID, string(status), startedAt, lastStepAt)
		require.NoError(t, insErr)
		return id
	}
	freshHeartbeat := now.Add(-1 * time.Minute)
	staleHeartbeat := now.Add(-40 * time.Minute)
	longAgo := now.Add(-40 * time.Minute)

	activeID := insertRun(RunStatusRunning, longAgo, &freshHeartbeat)
	stoppedHeartbeatID := insertRun(RunStatusRunning, longAgo, &staleHeartbeat)
	neverHeartbeatedID := insertRun(RunStatusRunning, longAgo, nil)
	queuedID := insertRun(RunStatusQueued, longAgo, nil)

	n, err := NewRepository(db).MarkStaleRunsAsError(ctx, 30*time.Minute)
	require.NoError(t, err)
	require.Equal(t, 2, n, "only the two runs with no recent activity should be reaped")

	statusOf := func(id string) string {
		var s string
		require.NoError(t, db.NewRaw(`SELECT status FROM kb.agent_runs WHERE id = ?`, id).Scan(ctx, &s))
		return s
	}

	require.Equal(t, string(RunStatusRunning), statusOf(activeID),
		"a run heartbeating within the threshold must survive the sweep")
	require.Equal(t, string(RunStatusError), statusOf(stoppedHeartbeatID),
		"a run whose heartbeat stopped must still be reaped (dead-writer case)")
	require.Equal(t, string(RunStatusError), statusOf(neverHeartbeatedID),
		"a run that never heartbeated must fall back to started_at and be reaped")
	require.Equal(t, string(RunStatusQueued), statusOf(queuedID),
		"a never-started queued run must never be terminal-failed")
}
