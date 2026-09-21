package agents

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/sandbox"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/adk"
)

// These tests exercise the run-lifetime teardown binding used by Execute,
// ExecuteWithRun, and Resume: each creates one *runCleanup, defers
// Cleanup(), and hands the same idempotent function to callers via
// ExecuteResult.Cleanup. The binding is what makes teardown guaranteed on
// every exit path, including a panic.

// A caller which never invokes Cleanup still gets teardown exactly once,
// because the executor defers Cleanup on the run lifetime.
func TestRunCleanup_DeferredRunsWithoutCallerInvocation(t *testing.T) {
	var calls atomic.Int64

	// Mirrors the executor pattern: create the binding, defer it, and return
	// without any caller invoking Cleanup.
	func() {
		cleanup := newRunCleanup(func() { calls.Add(1) })
		defer cleanup.Cleanup()
	}()

	assert.Equal(t, int64(1), calls.Load(), "deferred teardown must run once")
}

// Teardown survives a panic mid-run: the deferred binding still runs while the
// stack unwinds, before the surrounding handler recovers.
func TestRunCleanup_DeferredSurvivesPanic(t *testing.T) {
	var calls atomic.Int64

	func() {
		defer func() {
			_ = recover()
		}()
		cleanup := newRunCleanup(func() { calls.Add(1) })
		defer cleanup.Cleanup()

		panic("run exploded before returning a result")
	}()

	assert.Equal(t, int64(1), calls.Load(), "teardown must run even when the run panics")
}

// Teardown runs when the run returns early because its context was cancelled.
func TestRunCleanup_DeferredRunsOnContextCancellation(t *testing.T) {
	var calls atomic.Int64
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the run body, forcing an early return

	func() {
		cleanup := newRunCleanup(func() { calls.Add(1) })
		defer cleanup.Cleanup()

		if ctx.Err() != nil {
			return // early return on a cancelled context
		}
	}()

	assert.Equal(t, int64(1), calls.Load(), "teardown must run on a cancelled-context early return")
}

// A caller that also invokes Cleanup after the executor's deferred call must
// not trigger a second destroy: the existing sync.Once semantics are retained.
func TestRunCleanup_ExactlyOnceAcrossRepeatedCalls(t *testing.T) {
	var calls atomic.Int64
	cleanup := newRunCleanup(func() { calls.Add(1) })

	for i := 0; i < 5; i++ {
		cleanup.Cleanup()
	}

	assert.Equal(t, int64(1), calls.Load(), "teardown body must run at most once")
}

// Teardown failure is not surfaced to the caller: Cleanup returns nothing, so a
// failed destroy cannot mask the run's own result.
func TestRunCleanup_DoesNotSurfaceTeardownError(t *testing.T) {
	cleanup := newRunCleanup(func() { /* best-effort teardown, error logged inside */ })

	assert.NotPanics(t, func() { cleanup.Cleanup() })
}

// Cleanup must be safe on a nil binding or when no teardown body is bound, so
// error paths that never provisioned a sandbox cannot panic.
func TestRunCleanup_NilSafe(t *testing.T) {
	assert.NotPanics(t, func() {
		var c *runCleanup
		c.Cleanup()
		newRunCleanup(nil).Cleanup()
	})
}

// =============================================================================
// Ephemeral token revocation (teardownWorkspace)
// =============================================================================

// fakeEphemeralTokenSvc records revocation so tests can assert teardown revoked
// the per-run ephemeral token, even when no workspace was provisioned.
type fakeEphemeralTokenSvc struct {
	revokedTokenIDs []string
}

func (f *fakeEphemeralTokenSvc) CreateEphemeral(_ context.Context, _, _, _ string, _ time.Duration) (string, string, error) {
	return "eph-1", "emt_fake", nil
}

func (f *fakeEphemeralTokenSvc) RevokeEphemeral(_ context.Context, tokenID string) {
	f.revokedTokenIDs = append(f.revokedTokenIDs, tokenID)
}

// The ephemeral token is minted BEFORE provisioning, so it must be revoked even
// when the provisioning result is entirely nil (sandbox disabled/unconfigured).
// Without this, every sandbox-disabled run leaks a live admin-scoped token.
func TestTeardownWorkspace_RevokesTokenWhenResultNil(t *testing.T) {
	svc := &fakeEphemeralTokenSvc{}
	ae := &AgentExecutor{apiTokenSvc: svc, log: testAgentLogger()}

	ae.teardownWorkspace(context.Background(), nil, "eph-1")

	assert.Equal(t, []string{"eph-1"}, svc.revokedTokenIDs,
		"ephemeral token must be revoked even when no workspace result exists")
}

// Same guarantee when the result is present but carries no workspace.
func TestTeardownWorkspace_RevokesTokenWhenWorkspaceNil(t *testing.T) {
	svc := &fakeEphemeralTokenSvc{}
	ae := &AgentExecutor{apiTokenSvc: svc, log: testAgentLogger()}

	ae.teardownWorkspace(context.Background(), &sandbox.ProvisioningResult{Workspace: nil}, "eph-1")

	assert.Equal(t, []string{"eph-1"}, svc.revokedTokenIDs,
		"ephemeral token must be revoked when provisioning produced no workspace")
}

// A non-empty token with no token service must not panic.
func TestTeardownWorkspace_NilTokenServiceIsSafe(t *testing.T) {
	ae := &AgentExecutor{apiTokenSvc: nil, log: testAgentLogger()}

	assert.NotPanics(t, func() {
		ae.teardownWorkspace(context.Background(), nil, "eph-1")
	})
}

// =============================================================================
// Real production entry point: Execute wires deferred teardown
// =============================================================================

// TestExecute_DeferredCleanupRevokesEphemeralTokenOnError exercises the REAL
// Execute entry point (not a synthetic closure): a run is pre-created, the
// executor mints an ephemeral token, the LLM model factory fails so runPipeline
// returns an error, and the deferred cleanup must still revoke the token on the
// error return path. This fails if `defer cleanup.Cleanup()` is removed from
// Execute.
func TestExecute_DeferredCleanupRevokesEphemeralTokenOnError(t *testing.T) {
	repo := newRootCaptureRepository(t)
	svc := &fakeEphemeralTokenSvc{}
	ae := &AgentExecutor{
		repo:         repo,
		modelFactory: adk.NewModelFactory(&config.LLMConfig{}, testAgentLogger(), nil, nil, nil),
		apiTokenSvc:  svc,
		safeguards:   config.AgentSafeguardsConfig{ExecutionEnabled: true},
		log:          testAgentLogger(),
	}

	run := &AgentRun{ID: "run-1", Status: RunStatusRunning}
	result, err := ae.Execute(context.Background(), ExecuteRequest{
		PreCreatedRun: run,
		ProjectID:     "proj-1",
		OrgID:         "org-1",
		UserMessage:   "hello",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, []string{"eph-1"}, svc.revokedTokenIDs,
		"Execute must revoke the minted ephemeral token on the error return path")
}
