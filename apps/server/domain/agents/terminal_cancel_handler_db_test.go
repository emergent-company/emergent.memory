package agents

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// callCancelRun drives Handler.CancelRun through a real Echo context with the
// given run id, so the endpoint's response and the persisted row can be
// asserted together.
func callCancelRun(t *testing.T, h *Handler, projectID, agentID, runID string) (int, map[string]any) {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost,
		"/api/projects/"+projectID+"/agents/"+agentID+"/runs/"+runID+"/cancel", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id", "runId")
	c.SetParamValues(agentID, runID)
	c.Set(string(auth.UserContextKey), &auth.AuthUser{ID: "user-1", ProjectID: projectID})
	if err := h.CancelRun(c); err != nil {
		t.Fatalf("CancelRun returned error: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("cancel response is not JSON: %v (%s)", err, rec.Body.String())
	}
	return rec.Code, body
}

// TestCancelRunHandler_AlreadyCompletedStaysSuccess is the BLOCKING 2 /
// endpoint-consistency regression: cancelling a run that already completed must
// not clobber the row, and the endpoint must report cancelled:false rather than
// claiming a cancel it did not perform.
func TestCancelRunHandler_AlreadyCompletedStaysSuccess(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_cancel_handler_completed")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	agentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "handler-agent")
	runID := insertAgentRun(t, tdb.DB, ctx, agentID, string(RunStatusSuccess), true)

	h := &Handler{repo: NewRepository(tdb.DB), executor: &AgentExecutor{}}

	code, body := callCancelRun(t, h, projectID, agentID, runID)
	require.Equal(t, http.StatusOK, code)

	data, _ := body["data"].(map[string]any)
	require.NotNil(t, data)
	require.Equal(t, false, data["cancelled"], "an already-terminal run must not be reported cancelled")
	require.Equal(t, string(RunStatusSuccess), data["status"])
	require.Equal(t, string(RunStatusSuccess), runStatus(t, tdb, runID),
		"the completed row must be left untouched")
}

// TestCancelRunHandler_RunningRunIsCancelling is the positive path for durable
// cancellation (issue #1166): a running run is moved to the intermediate
// "cancelling" status — the durable cancel intent any instance can observe —
// and the endpoint reports cancelled:true with that status. The run is not
// finalised to "cancelled" here because the executing instance owns that
// transition (see TestCancelRunHandler_DurableCancelSurvivesExecutorCompletion).
func TestCancelRunHandler_RunningRunIsCancelling(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_cancel_handler_running")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	agentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "handler-agent")
	runID := insertAgentRun(t, tdb.DB, ctx, agentID, string(RunStatusRunning), true)

	h := &Handler{repo: NewRepository(tdb.DB), executor: &AgentExecutor{}}

	code, body := callCancelRun(t, h, projectID, agentID, runID)
	require.Equal(t, http.StatusOK, code)

	data, _ := body["data"].(map[string]any)
	require.NotNil(t, data)
	require.Equal(t, true, data["cancelled"], "a running run must be reported cancelled")
	require.Equal(t, string(RunStatusCancelling), data["status"])
	require.Equal(t, string(RunStatusCancelling), runStatus(t, tdb, runID),
		"the cancel endpoint must commit the durable cancelling intent")
}

// TestCancelRunHandler_DurableCancelSurvivesExecutorCompletion proves the
// endpoint's cancelled:true promise holds even when the executing instance
// finalizes the run afterwards: once the row is "cancelling", the executor's
// completion write resolves it to "cancelled", not "completed" (issue #1166).
func TestCancelRunHandler_DurableCancelSurvivesExecutorCompletion(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_cancel_handler_finalize")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	agentID := insertRuntimeAgent(t, tdb.DB, ctx, projectID, "handler-agent")
	runID := insertAgentRun(t, tdb.DB, ctx, agentID, string(RunStatusRunning), true)

	repo := NewRepository(tdb.DB)
	h := &Handler{repo: repo, executor: &AgentExecutor{}}

	code, body := callCancelRun(t, h, projectID, agentID, runID)
	require.Equal(t, http.StatusOK, code)
	data, _ := body["data"].(map[string]any)
	require.Equal(t, true, data["cancelled"])

	// The executing instance reaches its completion write after the cancel. It
	// must not turn the run into a success the endpoint never promised.
	require.NoError(t, repo.CompleteRunWithSteps(ctx, runID, map[string]any{}, 5, 1000))
	require.Equal(t, string(RunStatusCancelled), runStatus(t, tdb, runID),
		"a committed cancel must win over a later completion on another instance")
}
