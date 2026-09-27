package agents

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// gateToolPool is a fake toolPool whose StripOperatorTools seam blocks until the
// run's context is cancelled or the test releases it. runPipeline reaches this
// seam after the run is created and registered, so tests can hold a live run
// open and then either drop the request context (issue #1149 detach) or issue
// an explicit cancel, and observe the outcome deterministically.
type gateToolPool struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newGateToolPool() *gateToolPool {
	return &gateToolPool{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (p *gateToolPool) ResolveTools(string, *AgentDefinition, int, int) ([]tool.Tool, error) {
	return nil, nil
}

func (p *gateToolPool) StripOperatorTools(ctx context.Context, tools []tool.Tool) []tool.Tool {
	p.once.Do(func() { close(p.entered) })
	select {
	case <-ctx.Done():
	case <-p.release:
	}
	return tools
}

func (p *gateToolPool) ToolScopes() map[string]string { return nil }

func (p *gateToolPool) CallTool(context.Context, string, string, map[string]any) (map[string]any, error) {
	return map[string]any{"ok": true}, nil
}

type executeOutcome struct {
	res *ExecuteResult
	err error
}

// startGateRun launches ae.Execute for agentID on runCtx and returns a channel
// that yields its outcome. It waits for the run to reach the tool-dispatch gate
// before returning, so the caller knows the run is live and registered.
func startGateRun(t *testing.T, ae *AgentExecutor, gate *gateToolPool, runCtx context.Context, agentID, projectID string) <-chan executeOutcome {
	t.Helper()
	done := make(chan executeOutcome, 1)
	go func() {
		res, err := ae.Execute(runCtx, ExecuteRequest{
			Agent:           &Agent{ID: agentID, ProjectID: projectID, Name: "detach-agent"},
			ProjectID:       projectID,
			UserMessage:     "hello",
			TrustedInternal: true,
		})
		done <- executeOutcome{res: res, err: err}
	}()
	select {
	case <-gate.entered:
	case <-time.After(30 * time.Second):
		t.Fatal("run never reached the tool-dispatch gate")
	}
	return done
}

func waitOutcome(t *testing.T, done <-chan executeOutcome) executeOutcome {
	t.Helper()
	select {
	case o := <-done:
		return o
	case <-time.After(30 * time.Second):
		t.Fatal("run did not finish")
		return executeOutcome{}
	}
}

func runStatusOf(t *testing.T, tdb *testdb.TestDB, agentID string) string {
	t.Helper()
	var status string
	require.NoError(t, tdb.DB.NewRaw(
		`SELECT status FROM kb.agent_runs WHERE agent_id = ? ORDER BY started_at DESC LIMIT 1`,
		agentID,
	).Scan(context.Background(), &status))
	return status
}

// TestDetachedRunSurvivesRequestContextCancellation is the fail-first regression
// for issue #1149 at the executor seam: a run started on DetachedRunContext must
// complete even though the triggering request context is cancelled mid-run
// (browser reload / navigation / network blip). With the old binding — passing
// the request context straight into Execute, as exercised by the companion test
// below — the same cancellation hard-fails the run with "context canceled".
func TestDetachedRunSurvivesRequestContextCancellation(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_run_detach")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	agentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "detach-agent")

	ae := transportEnforcedExecutor(NewRepository(tdb.DB), newGateToolPool(), session.InMemoryService())
	gate := ae.toolPool.(*gateToolPool)

	requestCtx, cancelRequest := context.WithCancel(context.Background())
	done := startGateRun(t, ae, gate, DetachedRunContext(requestCtx), agentID, projectID)

	// The browser↔gateway SSE connection drops.
	cancelRequest()
	close(gate.release)

	o := waitOutcome(t, done)
	require.NoError(t, o.err)
	require.NotNil(t, o.res)
	require.Equal(t, RunStatusSuccess, o.res.Status,
		"a run detached from the request context must not fail when the request is cancelled")
	require.Equal(t, string(RunStatusSuccess), runStatusOf(t, tdb, agentID))
}

// TestRequestBoundRunFailsOnRequestContextCancellation documents the pre-fix
// binding that issue #1149 is about: a run executed directly on the request
// context is aborted with "context canceled" when that context is cancelled.
// It is the RED counterpart to TestDetachedRunSurvivesRequestContextCancellation
// and fails if a run is ever bound to the request context again.
func TestRequestBoundRunFailsOnRequestContextCancellation(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_run_bound")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	agentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "bound-agent")

	ae := transportEnforcedExecutor(NewRepository(tdb.DB), newGateToolPool(), session.InMemoryService())
	gate := ae.toolPool.(*gateToolPool)

	requestCtx, cancelRequest := context.WithCancel(context.Background())
	done := startGateRun(t, ae, gate, requestCtx, agentID, projectID)

	cancelRequest()
	close(gate.release)

	o := waitOutcome(t, done)
	require.NoError(t, o.err)
	require.NotNil(t, o.res)
	require.Equal(t, RunStatusError, o.res.Status,
		"a request-bound run is expected to abort when its request context is cancelled")
	require.Contains(t, o.res.Summary["error"], "context cancel")
}

// TestExplicitCancelStopsDetachedRun proves the other half of issue #1149: a run
// detached from its request context must still be stoppable by an explicit
// cancel routed by run id (POST …/runs/:runId/cancel), and that stop must be
// recorded honestly as cancelled — not as a server "context canceled" fault.
func TestExplicitCancelStopsDetachedRun(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_run_explicit_cancel")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	agentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "cancel-agent")

	ae := transportEnforcedExecutor(NewRepository(tdb.DB), newGateToolPool(), session.InMemoryService())
	gate := ae.toolPool.(*gateToolPool)

	requestCtx, cancelRequest := context.WithCancel(context.Background())
	defer cancelRequest()
	done := startGateRun(t, ae, gate, DetachedRunContext(requestCtx), agentID, projectID)

	runID := newestRunningRunID(t, tdb, agentID)
	require.NotEmpty(t, runID)
	require.True(t, ae.Cancel(runID, userCancelReason),
		"an explicit cancel must reach the detached, in-flight run by id")

	// The gate unblocks on ctx.Done(); release is deliberately not closed so
	// this proves the cancel itself stopped the run.
	o := waitOutcome(t, done)
	require.NoError(t, o.err)
	require.NotNil(t, o.res)
	require.Equal(t, RunStatusCancelled, o.res.Status,
		"a user stop must be reported as cancelled, not error")
	require.Equal(t, "user_cancelled", o.res.Summary["reason"])

	require.Equal(t, string(RunStatusCancelled), runStatusOf(t, tdb, agentID))
	var errMsg string
	require.NoError(t, tdb.DB.NewRaw(
		`SELECT COALESCE(error_message, '') FROM kb.agent_runs WHERE id = ?`, runID,
	).Scan(ctx, &errMsg))
	require.Equal(t, userCancelReason, errMsg,
		"the run row must explain the stop as a user cancel")
}

func newestRunningRunID(t *testing.T, tdb *testdb.TestDB, agentID string) string {
	t.Helper()
	var id string
	require.NoError(t, tdb.DB.NewRaw(
		`SELECT id FROM kb.agent_runs WHERE agent_id = ? AND status = ? ORDER BY started_at DESC LIMIT 1`,
		agentID, string(RunStatusRunning),
	).Scan(context.Background(), &id))
	return id
}
