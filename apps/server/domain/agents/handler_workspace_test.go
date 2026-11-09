package agents

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/sandbox"
	"github.com/emergent-company/emergent.memory/internal/testdb"
)

func insertSandboxRow(t *testing.T, tdb *testdb.TestDB, ctx context.Context, sessionRunID, containerID string) {
	t.Helper()
	_, err := tdb.DB.ExecContext(ctx, `
		INSERT INTO kb.agent_sandboxes
			(id, agent_session_id, container_type, provider, provider_workspace_id,
			 deployment_mode, lifecycle, status, base_image)
		VALUES (?, ?, 'agent_sandbox', 'gvisor', ?, 'self-hosted', 'ephemeral', 'ready', 'test-image')`,
		uuid.NewString(), sessionRunID, containerID)
	require.NoError(t, err)
}

// TestGetWorkspaceInfo_SharedChildFallsBackToParent pins that a delegated child
// with no workspace row of its own reports the ancestor's workspace and marks
// it shared, instead of an empty workspace.
func TestGetWorkspaceInfo_SharedChildFallsBackToParent(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_shared_workspace")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	agentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "parent-agent")
	parentRunID := insertAgentRun(t, tdb.DB, ctx, agentID, "", true)
	insertSandboxRow(t, tdb, ctx, parentRunID, "container-parent")

	childRunID := uuid.NewString()
	_, err := tdb.DB.ExecContext(ctx, `
		INSERT INTO kb.agent_runs (id, agent_id, status, started_at, parent_run_id)
		VALUES (?, ?, 'running', now(), ?)`, childRunID, agentID, parentRunID)
	require.NoError(t, err)

	h := &Handler{repo: NewRepository(tdb.DB), sandboxStore: sandbox.NewStore(tdb.DB)}

	got := h.getWorkspaceInfo(ctx, childRunID)
	require.NotNil(t, got)
	assert.True(t, got.Shared)
	assert.Equal(t, parentRunID, got.OwnerRunID)
	assert.Equal(t, "container-parent", got.ContainerID)
	assert.Equal(t, "test-image", got.BaseImage)
}

// TestGetWorkspaceInfo_OwnWorkspaceIsNotShared pins the direct case.
func TestGetWorkspaceInfo_OwnWorkspaceIsNotShared(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_own_workspace")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	agentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "solo-agent")
	runID := insertAgentRun(t, tdb.DB, ctx, agentID, "", true)
	insertSandboxRow(t, tdb, ctx, runID, "container-own")

	h := &Handler{repo: NewRepository(tdb.DB), sandboxStore: sandbox.NewStore(tdb.DB)}

	got := h.getWorkspaceInfo(ctx, runID)
	require.NotNil(t, got)
	assert.False(t, got.Shared)
	assert.Empty(t, got.OwnerRunID)
	assert.Equal(t, "container-own", got.ContainerID)
}

// TestGetWorkspaceInfo_NoWorkspaceReturnsNil pins the no-sandbox case.
func TestGetWorkspaceInfo_NoWorkspaceReturnsNil(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_no_workspace")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	agentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "no-ws-agent")
	runID := insertAgentRun(t, tdb.DB, ctx, agentID, "", true)

	h := &Handler{repo: NewRepository(tdb.DB), sandboxStore: sandbox.NewStore(tdb.DB)}

	assert.Nil(t, h.getWorkspaceInfo(ctx, runID))
}
