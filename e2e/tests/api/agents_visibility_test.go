// Package api_test — agents_visibility_test.go
// Migrated from agents_visibility_test.go. Tests that require direct DB access are skipped.
package api_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// ─────────────────────────────────────────────────────────────────────────────
// Cancel Run
// ─────────────────────────────────────────────────────────────────────────────

func TestAgentsVisibility_CancelRun_NotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	fakeAgentID := uuid.New().String()
	fakeRunID := uuid.New().String()

	resp := doAPILogged(t, rl, "POST",
		fmt.Sprintf("/api/admin/agents/%s/runs/%s/cancel", fakeAgentID, fakeRunID),
		e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusNotFound)
}

func TestAgentsVisibility_CancelRun_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	fakeAgentID := uuid.New().String()
	fakeRunID := uuid.New().String()
	fakeProjectID := uuid.New().String()

	resp := doAPILogged(t, rl, "POST",
		fmt.Sprintf("/api/projects/%s/agents/%s/runs/%s/cancel", fakeProjectID, fakeAgentID, fakeRunID),
		"", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

// ─────────────────────────────────────────────────────────────────────────────
// List Project Runs
// ─────────────────────────────────────────────────────────────────────────────

func TestAgentsVisibility_ListProjectRuns_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET",
		fmt.Sprintf("/api/projects/%s/agent-runs", projectID),
		"", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestAgentsVisibility_ListProjectRuns_ReturnsArray(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET",
		fmt.Sprintf("/api/projects/%s/agent-runs", projectID),
		e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["data"] == nil {
		t.Error("expected 'data' key in response")
	}
}

func TestAgentsVisibility_ListProjectRuns_InvalidStatusFilter(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET",
		fmt.Sprintf("/api/projects/%s/agent-runs?status=bogus-status", projectID),
		e2eTestToken(), projectID, nil)
	// Should return 400 or 200 with empty list; either is acceptable depending on server implementation
	sc := resp.StatusCode
	if sc != http.StatusOK && sc != http.StatusBadRequest {
		t.Errorf("expected 200 or 400, got %d", sc)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Get Run Messages / Tool Calls
// ─────────────────────────────────────────────────────────────────────────────

func TestAgentsVisibility_GetRunMessages_NotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	fakeRunID := uuid.New().String()
	resp := doAPILogged(t, rl, "GET",
		fmt.Sprintf("/api/projects/%s/agent-runs/%s/messages", projectID, fakeRunID),
		e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusNotFound)
}

func TestAgentsVisibility_GetRunToolCalls_NotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	fakeRunID := uuid.New().String()
	resp := doAPILogged(t, rl, "GET",
		fmt.Sprintf("/api/projects/%s/agent-runs/%s/tool-calls", projectID, fakeRunID),
		e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusNotFound)
}

func TestAgentsVisibility_GetRun_NotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	fakeRunID := uuid.New().String()
	resp := doAPILogged(t, rl, "GET",
		fmt.Sprintf("/api/projects/%s/agent-runs/%s", projectID, fakeRunID),
		e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusNotFound)
}

// ─────────────────────────────────────────────────────────────────────────────
// DB-dependent tests are skipped
// ─────────────────────────────────────────────────────────────────────────────

func TestAgentsVisibility_CancelRun_Success_SkipsWithoutDB(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	rl.Skipf("requires direct DB access to create test run")
}

func TestAgentsVisibility_ListProjectRuns_WithFiltering_SkipsWithoutDB(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	rl.Skipf("requires direct DB access to create test runs with specific statuses")
}

func TestAgentsVisibility_ListProjectRuns_VerifyMetrics_SkipsWithoutDB(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	rl.Skipf("requires direct DB access to create test runs with metrics")
}

func TestAgentsVisibility_GetRunMessages_Success_SkipsWithoutDB(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	rl.Skipf("requires direct DB access to create test messages")
}

func TestAgentsVisibility_GetRunToolCalls_Success_SkipsWithoutDB(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	rl.Skipf("requires direct DB access to create test tool calls")
}
