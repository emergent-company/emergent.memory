// Package api_test — agent_run_orderer_test.go
//
// Over-HTTP proof of provenance v2's read contract (#1079): a manual trigger
// records the signed-in person as the run's ordering user, and the run batch
// read the object surfaces use to resolve that person
// (`GET /api/projects/:projectId/agent-runs?ids=…`) returns it.
//
// The gateway turns the id into a display name and renders "on <name>'s order ·
// via <agent>" (object_provenance.templ; the render itself is covered by the
// gateway unit tests and the hermetic Playwright spec). What no unit test can
// prove is the seam under it: `kb.agent_runs.ordered_by_user_id` actually reaches
// the DTO a client receives, and the batch read that delivers it is scoped to
// the project it was asked about — a foreign run id must not resolve.
//
// The trigger path is the one an in-product caller takes (POST /trigger), so the
// tests drive that rather than writing the column directly: a regression in the
// handler's principal lookup (OrderedByUser(user.ID)) is exactly what would make
// every "on <name>'s order" line disappear without failing a DB test.
package api_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// ─────────────────────────────────────────────────────────────────────────────
// Helpers (package-scoped names are prefixed so they cannot collide with the
// helpers in agents_test.go / helpers_test.go)
// ─────────────────────────────────────────────────────────────────────────────

// ordererTestUserID returns the id of the principal the test token
// authenticates as — the person a manual trigger records as the run's orderer.
func ordererTestUserID(t *testing.T, rl *runLog) string {
	t.Helper()
	resp := doAPILogged(t, rl, "GET", "/api/auth/me", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	userID, ok := result["user_id"].(string)
	if !ok || userID == "" {
		t.Fatalf("/api/auth/me returned no user_id: %s", body)
	}
	return userID
}

// ordererTestTriggeredRun creates an agent in projectID and triggers it,
// returning the run id the trigger response reports. The run row is created
// synchronously before the response, so nothing has to wait for execution.
func ordererTestTriggeredRun(t *testing.T, rl *runLog, projectID, name string) string {
	t.Helper()
	token := e2eTestToken()

	createResp := doAPILogged(t, rl, "POST", agentPath(projectID, ""), token, projectID, jsonBody(map[string]any{
		"name":         name + " " + uuid.New().String()[:8],
		"strategyType": "extraction",
		"cronSchedule": "*/30 * * * *",
	}))
	createBody := mustStatus(t, createResp, http.StatusCreated)
	var createResult map[string]any
	parseBodyJSON(t, createBody, &createResult)
	data, ok := createResult["data"].(map[string]any)
	if !ok {
		t.Fatalf("agent create response has no data object: %s", createBody)
	}
	agentID, _ := data["id"].(string)
	if agentID == "" {
		t.Fatalf("agent create response has no id: %s", createBody)
	}

	triggerResp := doAPILogged(t, rl, "POST", agentPath(projectID, "/"+agentID+"/trigger"), token, projectID, nil)
	triggerBody := mustStatus(t, triggerResp, http.StatusOK)
	var triggerResult map[string]any
	parseBodyJSON(t, triggerBody, &triggerResult)
	runID, _ := triggerResult["runId"].(string)
	if runID == "" {
		t.Fatalf("trigger response carries no runId: %s", triggerBody)
	}
	rl.Printf("triggered agent %s → run %s", agentID, runID)
	return runID
}

// ordererTestRunBatch is one decoded batch read from the run listing.
type ordererTestRunBatch struct {
	Items      []map[string]any
	TotalCount int
}

// ordererTestBatchRead performs the ids batch read for projectID, asserting
// wantStatus, and returns the decoded body for the caller's checks.
func ordererTestBatchRead(t *testing.T, rl *runLog, projectID, ids string, wantStatus int) ordererTestRunBatch {
	t.Helper()
	path := "/api/projects/" + projectID + "/agent-runs?ids=" + ids
	resp := doAPILogged(t, rl, "GET", path, e2eTestToken(), "", nil)
	body := mustStatus(t, resp, wantStatus)
	if wantStatus != http.StatusOK {
		return ordererTestRunBatch{}
	}

	var result struct {
		Success bool `json:"success"`
		Data    struct {
			Items      []map[string]any `json:"items"`
			TotalCount int              `json:"totalCount"`
		} `json:"data"`
	}
	parseBodyJSON(t, body, &result)
	if !result.Success {
		t.Fatalf("batch read reported success=false: %s", body)
	}
	return ordererTestRunBatch{
		Items:      result.Data.Items,
		TotalCount: result.Data.TotalCount,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestAgentRunOrderer_ManualTriggerRecordsOrderer is the end-to-end claim the
// object history depends on: a person triggers an agent by hand, and the run
// they ordered comes back naming them.
func TestAgentRunOrderer_ManualTriggerRecordsOrderer(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	userID := ordererTestUserID(t, rl)
	projectID, _ := setupProjectLogged(t, rl)
	runID := ordererTestTriggeredRun(t, rl, projectID, "Orderer E2E")

	rl.Section("Read the run back through the ids batch")
	batch := ordererTestBatchRead(t, rl, projectID, runID, http.StatusOK)
	if batch.TotalCount != 1 || len(batch.Items) != 1 {
		t.Fatalf("batch read of one run returned totalCount=%d items=%d", batch.TotalCount, len(batch.Items))
	}
	run := batch.Items[0]
	if got, _ := run["id"].(string); got != runID {
		t.Fatalf("batch read returned run %q, want %q", got, runID)
	}

	got, ok := run["orderedByUserId"].(string)
	if !ok || got == "" {
		t.Fatalf("run DTO carries no orderedByUserId, so the gateway cannot name the orderer: %v", run)
	}
	rl.AssertionStep("manual trigger records the ordering person on the run", userID, got, nil)
	if got != userID {
		t.Errorf("orderedByUserId = %q, want the triggering user %q", got, userID)
	}
}

// TestAgentRunOrderer_BatchReadIsProjectScoped pins the containment rule: the
// batch read resolves ids only through the project it is asked about, so a
// caller holding a foreign run id learns nothing from it. The object surfaces
// rely on this — they pass run ids stamped on objects in one project.
func TestAgentRunOrderer_BatchReadIsProjectScoped(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	userID := ordererTestUserID(t, rl)

	rl.Section("Two projects, one ordered run each")
	projectA, _ := setupProjectLogged(t, rl)
	runA := ordererTestTriggeredRun(t, rl, projectA, "Orderer Scope A")
	projectB, _ := setupProjectLogged(t, rl)
	runB := ordererTestTriggeredRun(t, rl, projectB, "Orderer Scope B")

	rl.Section("Project A asked for both ids resolves only its own run")
	batch := ordererTestBatchRead(t, rl, projectA, runA+","+runB, http.StatusOK)
	if batch.TotalCount != 1 || len(batch.Items) != 1 {
		t.Fatalf("project A read of {own,foreign} returned totalCount=%d items=%d, want only its own run",
			batch.TotalCount, len(batch.Items))
	}
	if got, _ := batch.Items[0]["id"].(string); got != runA {
		t.Errorf("project A resolved run %q, want %q (the foreign run must not appear)", got, runA)
	}
	if got, _ := batch.Items[0]["orderedByUserId"].(string); got != userID {
		t.Errorf("project A run orderer = %q, want %q", got, userID)
	}

	rl.Section("Project B asked only for project A's id resolves nothing")
	foreign := ordererTestBatchRead(t, rl, projectB, runA, http.StatusOK)
	if foreign.TotalCount != 0 || len(foreign.Items) != 0 {
		t.Errorf("project B resolved a foreign run id: totalCount=%d items=%v", foreign.TotalCount, foreign.Items)
	}
}

// TestAgentRunOrderer_BatchReadDropsMalformedIDs pins the parse contract: the
// ids filter is a convenience read, so a typo drops that value instead of
// turning the request into a 500 against a uuid column — but a request with no
// usable id at all is a caller bug and is rejected.
func TestAgentRunOrderer_BatchReadDropsMalformedIDs(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	ordererTestUserID(t, rl) // fail fast if the token cannot authenticate
	projectID, _ := setupProjectLogged(t, rl)
	runID := ordererTestTriggeredRun(t, rl, projectID, "Orderer Parse")

	rl.Section("A malformed id beside a real one is dropped")
	batch := ordererTestBatchRead(t, rl, projectID, "not-a-uuid,"+runID, http.StatusOK)
	if batch.TotalCount != 1 || len(batch.Items) != 1 {
		t.Fatalf("mixed {malformed,real} ids returned totalCount=%d items=%d, want the real run only",
			batch.TotalCount, len(batch.Items))
	}
	if got, _ := batch.Items[0]["id"].(string); got != runID {
		t.Errorf("mixed ids resolved run %q, want %q", got, runID)
	}

	rl.Section("A request with no usable id is a bad request")
	ordererTestBatchRead(t, rl, projectID, "not-a-uuid", http.StatusBadRequest)
	ordererTestBatchRead(t, rl, projectID, "%20,%20", http.StatusBadRequest)
}

// TestAgentRunOrderer_BatchReadCapsTheSet pins the documented page cap: the
// batch read is bounded by the listing maximum, so ids beyond it are not
// resolved. The object drawer passes one id per history entry, and the cap is
// what keeps a long timeline from turning into an unbounded query.
func TestAgentRunOrderer_BatchReadCapsTheSet(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	ordererTestUserID(t, rl) // fail fast if the token cannot authenticate
	projectID, _ := setupProjectLogged(t, rl)
	runID := ordererTestTriggeredRun(t, rl, projectID, "Orderer Cap")

	// 100 well-formed ids the server will drop (they name no run), then the real
	// one behind them: the cap keeps the first 100, so the real id never runs.
	filler := make([]string, 100)
	for i := range filler {
		filler[i] = uuid.NewString()
	}
	rl.Section("An id past the cap is not resolved")
	pastCap := append(append([]string{}, filler...), runID)
	if batch := ordererTestBatchRead(t, rl, projectID, strings.Join(pastCap, ","), http.StatusOK); batch.TotalCount != 0 {
		t.Errorf("id beyond the 100-run cap still resolved: totalCount=%d", batch.TotalCount)
	}

	// The same request with the real id first resolves — the request itself is
	// well formed, so the previous read was about the cap and not a parse error.
	rl.Section("The same id inside the cap resolves")
	inCap := ordererTestBatchRead(t, rl, projectID, strings.Join(append([]string{runID}, filler...), ","), http.StatusOK)
	if inCap.TotalCount != 1 || len(inCap.Items) != 1 {
		t.Fatalf("id at the head of the set returned totalCount=%d items=%d, want 1",
			inCap.TotalCount, len(inCap.Items))
	}
	if got, _ := inCap.Items[0]["id"].(string); got != runID {
		t.Errorf("resolved run = %q, want %q", got, runID)
	}
}
