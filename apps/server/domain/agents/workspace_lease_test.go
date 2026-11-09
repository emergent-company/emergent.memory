package agents

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/sandbox"
)

// sharedTestWorkspace builds a ready workspace row as the owner run would have
// provisioned it.
func sharedTestWorkspace() *sandbox.AgentSandbox {
	repoURL := "https://github.com/example/repo"
	branch := "main"
	return &sandbox.AgentSandbox{
		ID:                  "ws-1",
		Provider:            sandbox.ProviderGVisor,
		ProviderWorkspaceID: "ctr-1",
		RepositoryURL:       &repoURL,
		Branch:              &branch,
		Status:              sandbox.StatusReady,
	}
}

func sandboxedDefinition() *AgentDefinition {
	return &AgentDefinition{
		ID:            "def-1",
		SandboxConfig: map[string]any{"enabled": true},
	}
}

// =============================================================================
// spawn_agents input parsing
// =============================================================================

func spawnArgs(t *testing.T, item map[string]any) map[string]any {
	t.Helper()
	return map[string]any{"agents": []any{item}}
}

func TestParseSpawnRequests_SandboxMode(t *testing.T) {
	base := func(extra map[string]any) map[string]any {
		m := map[string]any{"agent_name": "worker", "task": "do the thing"}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}

	t.Run("omitted defaults to empty (shared at execution time)", func(t *testing.T) {
		reqs, err := parseSpawnRequests(spawnArgs(t, base(nil)))
		require.NoError(t, err)
		require.Len(t, reqs, 1)
		assert.Empty(t, reqs[0].SandboxMode)
		assert.Equal(t, SandboxModeShared, SandboxModeOrDefault(reqs[0].SandboxMode))
	})

	t.Run("shared", func(t *testing.T) {
		reqs, err := parseSpawnRequests(spawnArgs(t, base(map[string]any{"sandbox_mode": "shared"})))
		require.NoError(t, err)
		assert.Equal(t, SandboxModeShared, reqs[0].SandboxMode)
	})

	t.Run("isolated", func(t *testing.T) {
		reqs, err := parseSpawnRequests(spawnArgs(t, base(map[string]any{"sandbox_mode": "isolated"})))
		require.NoError(t, err)
		assert.Equal(t, SandboxModeIsolated, reqs[0].SandboxMode)
	})

	t.Run("unknown mode rejected", func(t *testing.T) {
		_, err := parseSpawnRequests(spawnArgs(t, base(map[string]any{"sandbox_mode": "copy-on-write"})))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown sandbox_mode")
	})

	t.Run("non-string mode rejected", func(t *testing.T) {
		_, err := parseSpawnRequests(spawnArgs(t, base(map[string]any{"sandbox_mode": 1})))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be a string")
	})

	t.Run("null mode treated as omitted", func(t *testing.T) {
		reqs, err := parseSpawnRequests(spawnArgs(t, base(map[string]any{"sandbox_mode": nil})))
		require.NoError(t, err)
		assert.Empty(t, reqs[0].SandboxMode)
	})
}

func TestSandboxModeOrDefault(t *testing.T) {
	assert.Equal(t, SandboxModeShared, SandboxModeOrDefault(""))
	assert.Equal(t, SandboxModeShared, SandboxModeOrDefault(SandboxModeShared))
	assert.Equal(t, SandboxModeIsolated, SandboxModeOrDefault(SandboxModeIsolated))
}

func TestDefinitionSandboxEnabled(t *testing.T) {
	assert.False(t, definitionSandboxEnabled(nil), "nil definition has no sandbox")
	assert.False(t, definitionSandboxEnabled(&AgentDefinition{}), "no sandbox_config has no sandbox")
	assert.False(t, definitionSandboxEnabled(&AgentDefinition{
		SandboxConfig: map[string]any{"enabled": false},
	}), "disabled sandbox_config has no sandbox")
	assert.True(t, definitionSandboxEnabled(sandboxedDefinition()), "enabled sandbox_config has a sandbox")
}

// =============================================================================
// Spawn mode resolution
// =============================================================================

func TestResolveSpawnSandbox(t *testing.T) {
	lease := &sandbox.WorkspaceLease{Workspace: sharedTestWorkspace(), OwnerRunID: "run-parent"}

	t.Run("default is shared when the parent has a lease", func(t *testing.T) {
		deps := &CoordinationToolDeps{WorkspaceLease: lease}
		mode, got, reason := resolveSpawnSandbox(deps, sandboxedDefinition(), "")
		require.Equal(t, SandboxModeShared, mode)
		require.Same(t, lease, got, "the child must borrow the parent's lease")
		assert.Empty(t, reason)
	})

	t.Run("explicit isolated never borrows", func(t *testing.T) {
		deps := &CoordinationToolDeps{WorkspaceLease: lease}
		mode, got, reason := resolveSpawnSandbox(deps, sandboxedDefinition(), SandboxModeIsolated)
		require.Equal(t, SandboxModeIsolated, mode)
		assert.Nil(t, got)
		assert.Empty(t, reason)
	})

	t.Run("shared with no parent workspace falls back to isolated", func(t *testing.T) {
		mode, got, reason := resolveSpawnSandbox(&CoordinationToolDeps{}, sandboxedDefinition(), "")
		assert.Equal(t, SandboxModeIsolated, mode)
		assert.Nil(t, got)
		assert.Contains(t, reason, "no live workspace")
	})

	t.Run("shared with nil deps falls back to isolated", func(t *testing.T) {
		mode, got, _ := resolveSpawnSandbox(nil, sandboxedDefinition(), "")
		assert.Equal(t, SandboxModeIsolated, mode)
		assert.Nil(t, got)
	})

	t.Run("shared with a sandbox-less delegate never borrows", func(t *testing.T) {
		deps := &CoordinationToolDeps{WorkspaceLease: lease}
		mode, got, reason := resolveSpawnSandbox(deps, &AgentDefinition{ID: "def-2"}, "")
		assert.Equal(t, SandboxModeIsolated, mode, "a sandbox-less delegate must not gain workspace tools")
		assert.Nil(t, got)
		assert.Contains(t, reason, "no enabled sandbox config")
	})

	t.Run("explicit isolated with a sandbox-less delegate stays isolated", func(t *testing.T) {
		deps := &CoordinationToolDeps{WorkspaceLease: lease}
		mode, got, _ := resolveSpawnSandbox(deps, &AgentDefinition{ID: "def-2"}, SandboxModeIsolated)
		assert.Equal(t, SandboxModeIsolated, mode)
		assert.Nil(t, got)
	})
}

// =============================================================================
// Lease → provisioning result
// =============================================================================

func TestSharedWorkspaceResult(t *testing.T) {
	t.Run("ready workspace and enabled config yields a borrowed result", func(t *testing.T) {
		ws := sharedTestWorkspace()
		res, reason := sharedWorkspaceResult(&sandbox.WorkspaceLease{
			Workspace:  ws,
			OwnerRunID: "run-parent",
		}, sandboxedDefinition())

		require.NotNil(t, res)
		assert.Empty(t, reason)
		assert.True(t, res.Borrowed, "a leased workspace must be marked borrowed so teardown skips it")
		assert.Equal(t, "run-parent", res.BorrowedFromRunID)
		assert.Same(t, ws, res.Workspace)
		assert.Equal(t, ws.Provider, res.ProviderType)
		assert.Equal(t, "https://github.com/example/repo", res.RepoURL)
		assert.Equal(t, "main", res.Branch)
	})

	t.Run("nil lease means no sharing was requested", func(t *testing.T) {
		res, reason := sharedWorkspaceResult(nil, sandboxedDefinition())
		assert.Nil(t, res)
		assert.Empty(t, reason)
	})

	t.Run("lease without a workspace is rejected", func(t *testing.T) {
		res, reason := sharedWorkspaceResult(&sandbox.WorkspaceLease{OwnerRunID: "run-parent"}, sandboxedDefinition())
		assert.Nil(t, res)
		assert.Contains(t, reason, "no workspace")
	})

	t.Run("delegate without an enabled sandbox config is rejected", func(t *testing.T) {
		res, reason := sharedWorkspaceResult(&sandbox.WorkspaceLease{Workspace: sharedTestWorkspace()}, nil)
		assert.Nil(t, res)
		assert.Contains(t, reason, "no enabled sandbox config")
	})

	t.Run("workspace that is not ready is rejected", func(t *testing.T) {
		ws := sharedTestWorkspace()
		ws.Status = sandbox.StatusStopped
		res, reason := sharedWorkspaceResult(&sandbox.WorkspaceLease{Workspace: ws, OwnerRunID: "run-parent"}, sandboxedDefinition())
		assert.Nil(t, res)
		assert.Contains(t, reason, "not ready")
	})
}

// =============================================================================
// Executor: attach and teardown
// =============================================================================

// newSharedTestExecutor returns an executor whose provisioner would panic on any
// real provisioning call (nil orchestrator/service). Any test that reaches the
// provisioning path therefore fails loudly, which is what makes the borrowed
// short-circuit assertion meaningful.
func newSharedTestExecutor() *AgentExecutor {
	return &AgentExecutor{
		wsEnabled:   true,
		provisioner: sandbox.NewAutoProvisioner(nil, nil, nil, nil, nil, testAgentLogger(), nil),
		log:         testAgentLogger(),
	}
}

func TestProvisionWorkspace_SharedLeaseSkipsProvisioning(t *testing.T) {
	ae := newSharedTestExecutor()
	ws := sharedTestWorkspace()

	var res *sandbox.ProvisioningResult
	require.NotPanics(t, func() {
		var err error
		res, err = ae.provisionWorkspace(context.Background(), "run-child", ExecuteRequest{
			AgentDefinition: sandboxedDefinition(),
			ProjectID:       "proj-1",
			WorkspaceLease:  &sandbox.WorkspaceLease{Workspace: ws, OwnerRunID: "run-parent"},
		})
		require.NoError(t, err)
	})

	require.NotNil(t, res)
	assert.True(t, res.Borrowed)
	assert.Equal(t, "run-parent", res.BorrowedFromRunID)
	assert.Same(t, ws, res.Workspace, "the child must use the parent's container, not a new one")
}

func TestProvisionWorkspace_SharedLeaseRequiresDelegateSandbox(t *testing.T) {
	ae := newSharedTestExecutor()

	res, err := ae.provisionWorkspace(context.Background(), "run-child", ExecuteRequest{
		AgentDefinition: &AgentDefinition{ID: "def-nosandbox"},
		ProjectID:       "proj-1",
		WorkspaceLease:  &sandbox.WorkspaceLease{Workspace: sharedTestWorkspace(), OwnerRunID: "run-parent"},
	})
	require.NoError(t, err)
	assert.Nil(t, res, "a run without sandbox_config gets no workspace even when a lease is offered")
}

func TestTeardownWorkspace_BorrowedSandboxIsNotTornDown(t *testing.T) {
	ae := newSharedTestExecutor()

	// A nil-orchestrator provisioner panics inside TeardownWorkspace, so a
	// surviving call proves the borrowed guard returned before teardown.
	assert.NotPanics(t, func() {
		ae.teardownWorkspace(context.Background(), &sandbox.ProvisioningResult{
			Workspace:         sharedTestWorkspace(),
			Borrowed:          true,
			BorrowedFromRunID: "run-parent",
		}, "")
	}, "a borrowing run must never destroy the owner's container")
}

// =============================================================================
// Context plumbing
// =============================================================================

func TestWorkspaceLeaseContextRoundTrip(t *testing.T) {
	assert.Nil(t, WorkspaceLeaseFromContext(context.Background()), "a context without a lease yields nil")

	lease := &sandbox.WorkspaceLease{Workspace: sharedTestWorkspace(), OwnerRunID: "run-parent"}
	ctx := ContextWithWorkspaceLease(context.Background(), lease)
	assert.Same(t, lease, WorkspaceLeaseFromContext(ctx))

	// nil is a no-op rather than storing a nil value, so a run without a
	// workspace cannot accidentally shadow an inherited lease.
	assert.Nil(t, WorkspaceLeaseFromContext(ContextWithWorkspaceLease(context.Background(), nil)))
}

func TestAugmentInstructionWithWorkspace_SharedNote(t *testing.T) {
	ae := &AgentExecutor{log: testAgentLogger()}
	ws := sharedTestWorkspace()

	shared := ae.augmentInstructionWithWorkspace("base", &sandbox.ProvisioningResult{
		Workspace:         ws,
		Borrowed:          true,
		BorrowedFromRunID: "run-parent",
	})
	assert.True(t, strings.Contains(shared, "Shared:"), "a shared workspace must be announced in the instruction")
	assert.Contains(t, shared, "run-parent")
	assert.Contains(t, shared, "immediately visible")

	owned := ae.augmentInstructionWithWorkspace("base", &sandbox.ProvisioningResult{Workspace: ws})
	assert.NotContains(t, owned, "Shared:", "an owned workspace must not be described as shared")
}
