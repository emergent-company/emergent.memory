package agents

import (
	"context"
	"fmt"
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

// fakeEphemeralTokenSvc records mint and revocation so tests can assert the
// entry point minted exactly the tokens it later revoked, even when no
// workspace was provisioned.
type fakeEphemeralTokenSvc struct {
	mintedTokenIDs  []string
	revokedTokenIDs []string
}

func (f *fakeEphemeralTokenSvc) CreateEphemeral(_ context.Context, _, _, _ string, _ time.Duration) (string, string, error) {
	f.mintedTokenIDs = append(f.mintedTokenIDs, "eph-1")
	return "eph-1", "emt_fake", nil
}

func (f *fakeEphemeralTokenSvc) RevokeEphemeral(_ context.Context, tokenID string) {
	f.revokedTokenIDs = append(f.revokedTokenIDs, tokenID)
}

// failingImageResolver reports the configured base image as errored, which makes
// WaitForImageReady fail and forces the fatal pre-provisioning path.
type failingImageResolver struct{}

func (failingImageResolver) ResolveImage(context.Context, string, string) (*sandbox.ResolvedImage, error) {
	return nil, fmt.Errorf("image not found")
}

func (failingImageResolver) GetImageStatus(context.Context, string, string) (string, error) {
	return "error", nil
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

// TestExecuteWithRun_MintsAndRevokesEphemeralTokenOnError drives the REAL
// ExecuteWithRun entry point (the worker-pool / async-A2A path) with no token in
// the context: it must mint an ephemeral token and revoke it on the error return.
func TestExecuteWithRun_MintsAndRevokesEphemeralTokenOnError(t *testing.T) {
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
	result, err := ae.ExecuteWithRun(context.Background(), run, ExecuteRequest{
		ProjectID:   "proj-1",
		OrgID:       "org-1",
		UserMessage: "hello",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, []string{"eph-1"}, svc.mintedTokenIDs,
		"ExecuteWithRun must mint when no token is present in a background context")
	assert.Equal(t, []string{"eph-1"}, svc.revokedTokenIDs,
		"ExecuteWithRun must revoke the minted ephemeral token on the error return path")
}

// TestExecuteWithRun_ProvisioningFailureRevokesMintedToken is the regression
// test for the review finding that a provisioning failure returns before the
// teardown binding was installed: the token is minted before provisioning, so
// the fatal pre-provisioning path must still revoke it.
func TestExecuteWithRun_ProvisioningFailureRevokesMintedToken(t *testing.T) {
	repo := newRootCaptureRepository(t)
	svc := &fakeEphemeralTokenSvc{}
	ae := &AgentExecutor{
		repo:        repo,
		apiTokenSvc: svc,
		safeguards:  config.AgentSafeguardsConfig{ExecutionEnabled: true},
		wsEnabled:   true,
		provisioner: sandbox.NewAutoProvisioner(nil, nil, nil, nil, nil, testAgentLogger(), failingImageResolver{}),
		log:         testAgentLogger(),
	}

	run := &AgentRun{ID: "run-1", Status: RunStatusRunning}
	def := &AgentDefinition{
		ID:            "def-1",
		SandboxConfig: map[string]any{"enabled": true, "base_image": "missing-image"},
	}
	result, err := ae.ExecuteWithRun(context.Background(), run, ExecuteRequest{
		AgentDefinition: def,
		ProjectID:       "proj-1",
		OrgID:           "org-1",
		UserMessage:     "hello",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, RunStatusError, result.Status, "provisioning failure must fail the run")
	require.Equal(t, []string{"eph-1"}, svc.mintedTokenIDs, "token must be minted before provisioning")
	assert.Equal(t, []string{"eph-1"}, svc.revokedTokenIDs,
		"a provisioning failure must not leak the minted ephemeral token")
}

// TestExecuteWithRun_DisableAuthMintDoesNotMint asserts the guard added with the
// hoist: a DisableAuthMint request never mints (or revokes) an ephemeral token.
func TestExecuteWithRun_DisableAuthMintDoesNotMint(t *testing.T) {
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
	_, err := ae.ExecuteWithRun(context.Background(), run, ExecuteRequest{
		ProjectID:       "proj-1",
		OrgID:           "org-1",
		UserMessage:     "hello",
		DisableAuthMint: true,
	})

	require.NoError(t, err)
	assert.Empty(t, svc.mintedTokenIDs, "DisableAuthMint must suppress the ephemeral mint")
	assert.Empty(t, svc.revokedTokenIDs, "nothing was minted, so nothing may be revoked")
}
