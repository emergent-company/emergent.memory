package agents

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// subagentUpdateRecorder is a concurrency-safe SubagentEmitter used to observe
// child-run lifecycle projections in spawn tests.
type subagentUpdateRecorder struct {
	mu      sync.Mutex
	updates []SubagentUpdate
}

func (r *subagentUpdateRecorder) emit(u SubagentUpdate) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.updates = append(r.updates, u)
}

func (r *subagentUpdateRecorder) snapshot() []SubagentUpdate {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]SubagentUpdate, len(r.updates))
	copy(out, r.updates)
	return out
}

// TestExecuteSingleSpawn_EmitsRunningThenTerminalSubagent pins the subagent
// lifecycle projection: a spawned child emits a "running" row as soon as its
// run row exists (with the child run id as the stable id), then a terminal row
// when execution finishes. The model factory fails by design, so the terminal
// status is "failed" with the execution error.
func TestExecuteSingleSpawn_EmitsRunningThenTerminalSubagent(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_subagent_emission")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	insertAgentDefinition(t, tdb.DB, ctx, projectID, "emitting-child", string(VisibilityProject))
	insertRuntimeAgent(t, tdb.DB, ctx, projectID, "emitting-child")
	parentAgentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "parent-agent")
	parentRunID := insertAgentRun(t, tdb.DB, ctx, parentAgentID, "", true)

	repo := NewRepository(tdb.DB)
	ae := trustTestExecutor(repo)
	rec := &subagentUpdateRecorder{}
	deps := &CoordinationToolDeps{
		Executor:        ae,
		Repo:            repo,
		Logger:          testAgentLogger(),
		ProjectID:       projectID,
		ParentRunID:     parentRunID,
		RootRunID:       parentRunID,
		TrustedInternal: true,
		MaxDepth:        5,
		EmitSubagent:    rec.emit,
	}

	res := executeSingleSpawn(ctx, deps, SpawnRequest{AgentName: "emitting-child", Task: "do work"})
	require.NotEmpty(t, res.RunID)

	updates := rec.snapshot()
	require.GreaterOrEqual(t, len(updates), 2, "must emit running and terminal updates: %+v", updates)

	running := updates[0]
	require.Equal(t, "running", running.Status)
	require.Equal(t, res.RunID, running.ID, "child run id must be the stable subagent id")
	require.Equal(t, res.RunID, running.RunID)
	require.Equal(t, parentRunID, running.ParentRunID)
	require.Equal(t, "emitting-child", running.AgentName)
	require.Equal(t, "do work", running.Task)

	terminal := updates[len(updates)-1]
	require.Equal(t, "failed", terminal.Status)
	require.Equal(t, res.RunID, terminal.ID, "terminal update must reuse the child run id")
	require.Equal(t, res.RunID, terminal.RunID)
	require.NotEmpty(t, terminal.Error, "terminal failure must carry the execution error")
}

// TestExecuteSingleSpawn_EmitsFailedForPreRunRejection pins the pre-run failure
// projection: a spawn rejected before any child run exists (unknown agent) still
// emits a failed row keyed by a stable synthetic id.
func TestExecuteSingleSpawn_EmitsFailedForPreRunRejection(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_subagent_emission_reject")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	parentAgentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "parent-agent")
	parentRunID := insertAgentRun(t, tdb.DB, ctx, parentAgentID, "", true)

	repo := NewRepository(tdb.DB)
	ae := trustTestExecutor(repo)
	rec := &subagentUpdateRecorder{}
	deps := &CoordinationToolDeps{
		Executor:        ae,
		Repo:            repo,
		Logger:          testAgentLogger(),
		ProjectID:       projectID,
		ParentRunID:     parentRunID,
		RootRunID:       parentRunID,
		TrustedInternal: true,
		MaxDepth:        5,
		EmitSubagent:    rec.emit,
	}

	res := executeSingleSpawn(ctx, deps, SpawnRequest{AgentName: "missing-agent", Task: "do work"})
	require.Empty(t, res.RunID)

	updates := rec.snapshot()
	require.Len(t, updates, 1, "a pre-run rejection emits exactly one failed row: %+v", updates)
	u := updates[0]
	require.Equal(t, "failed", u.Status)
	require.Contains(t, u.ID, "spawn-", "pre-run failure must use a synthetic stable id")
	require.Empty(t, u.RunID, "no child run exists")
	require.Equal(t, parentRunID, u.ParentRunID)
	require.Equal(t, "missing-agent", u.AgentName)
	require.Contains(t, u.Error, "not found")
}
