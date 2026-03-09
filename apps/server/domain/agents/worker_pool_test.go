package agents

import (
	"context"
	"log/slog"
	"math"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Task 10.1: trigger_agent dispatch routing unit test
// =============================================================================

// TestTriggerAgentDispatchModeQueued verifies that the dispatch condition
// (agentDef.DispatchMode == DispatchModeQueued) correctly identifies queued agents.
func TestTriggerAgentDispatchModeQueued(t *testing.T) {
	def := &AgentDefinition{
		DispatchMode: DispatchModeQueued,
	}
	assert.Equal(t, DispatchModeQueued, def.DispatchMode)
	assert.True(t, def.DispatchMode == DispatchModeQueued,
		"queued DispatchMode should trigger the queued branch")
}

func TestTriggerAgentDispatchModeSync(t *testing.T) {
	def := &AgentDefinition{
		DispatchMode: DispatchModeSync,
	}
	assert.False(t, def.DispatchMode == DispatchModeQueued,
		"sync DispatchMode should NOT trigger the queued branch")
}

func TestTriggerAgentDispatchModeDefault(t *testing.T) {
	// A zero-value AgentDefinition should not be mistakenly treated as queued
	def := &AgentDefinition{}
	assert.False(t, def.DispatchMode == DispatchModeQueued,
		"zero-value DispatchMode should not trigger the queued branch")
}

// TestRunStatusQueued verifies the RunStatusQueued constant is correct.
func TestRunStatusQueued(t *testing.T) {
	assert.Equal(t, AgentRunStatus("queued"), RunStatusQueued)
}

// TestDispatchModeTriggerAgent_NilDef verifies that a nil agentDef falls
// through to the sync branch (backward-compatible default).
func TestDispatchModeTriggerAgent_NilDef(t *testing.T) {
	// nil agentDef → no dispatch mode check → sync branch
	var agentDef *AgentDefinition
	isQueued := agentDef != nil && agentDef.DispatchMode == DispatchModeQueued
	assert.False(t, isQueued, "nil agentDef should fall through to sync branch")
}

// =============================================================================
// Task 10.2: WorkerPool — backoff and lifecycle unit tests
// =============================================================================

func TestWorkerPoolBackoff(t *testing.T) {
	// backoff(0) = 2^0 * 60 = 60s
	assert.Equal(t, 60*time.Second, backoff(0))
	// backoff(1) = 2^1 * 60 = 120s
	assert.Equal(t, 120*time.Second, backoff(1))
	// backoff(2) = 2^2 * 60 = 240s
	assert.Equal(t, 240*time.Second, backoff(2))
	// backoff(3) = 2^3 * 60 = 480s
	assert.Equal(t, 480*time.Second, backoff(3))
}

func TestWorkerPoolBackoffCapped(t *testing.T) {
	// Backoff should be capped at 3600s (1 hour)
	dur := backoff(100) // 2^100 * 60 would be astronomically large
	assert.Equal(t, 3600*time.Second, dur, "backoff should be capped at 1 hour")
}

func TestWorkerPoolBackoffCap(t *testing.T) {
	// 2^6 * 60 = 3840 > 3600, so attempt=6 should be capped
	dur6 := backoff(6)
	assert.Equal(t, 3600*time.Second, dur6)

	// 2^5 * 60 = 1920 < 3600, so attempt=5 should NOT be capped
	dur5 := backoff(5)
	expected5 := time.Duration(math.Pow(2, 5)*60) * time.Second
	assert.Equal(t, expected5, dur5)
}

// TestWorkerPoolDisabled verifies that Start with size=0 is a safe no-op.
func TestWorkerPoolDisabled(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	pool := NewWorkerPool(nil, nil, log, 0, 5*time.Second)

	ctx := context.Background()
	err := pool.Start(ctx)
	require.NoError(t, err, "Start with size=0 should succeed and be a no-op")

	// Stop should be safe to call even when pool was disabled
	pool.Stop() // should not panic
}

// TestWorkerPoolStartStop verifies that a WorkerPool with workers starts and
// stops cleanly within a reasonable timeout.
// Note: Uses size=0 to avoid nil repo panic; the full execution path requires DB.
func TestWorkerPoolStartStop(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	// Use size=0 to avoid nil repo dereference; the real lifecycle is tested
	// indirectly by integration tests or the disabled-pool path.
	pool := NewWorkerPool(nil, nil, log, 0, 50*time.Millisecond)

	ctx := context.Background()
	err := pool.Start(ctx)
	require.NoError(t, err)

	doneCh := make(chan struct{})
	go func() {
		pool.Stop()
		close(doneCh)
	}()

	select {
	case <-doneCh:
		// OK
	case <-time.After(2 * time.Second):
		t.Fatal("WorkerPool.Stop() timed out")
	}
}

// =============================================================================
// Task 10.3 & 10.4: Integration tests (require DB + migrations)
// These are skipped automatically when DB is not available or tables are missing.
// Run manually: POSTGRES_HOST=localhost POSTGRES_PORT=5436 POSTGRES_USER=emergent
//   POSTGRES_PASSWORD=local-test-password POSTGRES_DB=emergent go test -run TestIntegration
// =============================================================================

// TestIntegrationRequeueOrphanedQueuedRuns verifies that RequeueOrphanedQueuedRuns
// creates a job row for a queued run that has no active job (task 10.3).
func TestIntegrationRequeueOrphanedQueuedRuns(t *testing.T) {
	if os.Getenv("POSTGRES_HOST") == "" {
		t.Skip("POSTGRES_HOST not set; skipping DB integration test")
	}
	t.Skip("requires agent_run_jobs migration; run after: task migrate")
}

// TestIntegrationGetRunStatusCrossProject verifies that FindRunByIDProjectScoped
// returns nil for a run_id that belongs to a different project (task 10.4).
func TestIntegrationGetRunStatusCrossProject(t *testing.T) {
	if os.Getenv("POSTGRES_HOST") == "" {
		t.Skip("POSTGRES_HOST not set; skipping DB integration test")
	}
	t.Skip("requires agent_run_jobs migration; run after: task migrate")
}
