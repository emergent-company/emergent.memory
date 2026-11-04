// Package api_test — tasks_test.go
//
// Tests for the tasks API endpoints (/api/tasks).
// Ported from emergent.memory/apps/server/tests/e2e/tasks_test.go
//
// Note: Tests that require direct DB access (createTaskViaDB) are skipped.
package api_test

import (
	"net/http"
	"testing"
)

// =============================================================================
// GET /api/tasks — List Tasks
// =============================================================================

func TestTasks_List_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/tasks", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestTasks_List_RequiresProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/tasks", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusBadRequest)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	errObj, _ := result["error"].(map[string]any)
	msg, _ := errObj["message"].(string)
	assertContains(t, msg, "project_id")
}

func TestTasks_List_AcceptsProjectIDHeader(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/tasks", e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusOK)
}

func TestTasks_List_AcceptsProjectIDQueryParam(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/tasks?project_id="+projectID, e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusOK)
}

func TestTasks_List_ReturnsEmptyArrayWhenNoTasks(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/tasks", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	if _, ok := result["data"]; !ok {
		t.Error("response should have 'data' field")
	}
	if _, ok := result["total"]; !ok {
		t.Error("response should have 'total' field")
	}
	if result["total"] != float64(0) {
		t.Errorf("expected total=0 for empty project, got %v", result["total"])
	}
	rl.Printf("empty task list returned for new project")
}

func TestTasks_List_ReturnsTasks(t *testing.T) {
	t.Skip("requires direct DB access")
}

func TestTasks_List_FiltersByStatus(t *testing.T) {
	t.Skip("requires direct DB access")
}

func TestTasks_List_FiltersByType(t *testing.T) {
	t.Skip("requires direct DB access")
}

func TestTasks_List_SupportsPagination(t *testing.T) {
	t.Skip("requires direct DB access")
}

// =============================================================================
// GET /api/tasks/counts
// =============================================================================

func TestTasks_GetCounts_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/tasks/counts", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestTasks_GetCounts_RequiresProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/tasks/counts", e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestTasks_GetCounts_ReturnsZeroCounts(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/tasks/counts", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	for _, field := range []string{"pending", "accepted", "rejected", "cancelled"} {
		if result[field] != float64(0) {
			t.Errorf("expected %s=0 for empty project, got %v", field, result[field])
		}
	}
	rl.Printf("zero counts returned for new project")
}

func TestTasks_GetCounts_ReturnsCorrectCounts(t *testing.T) {
	t.Skip("requires direct DB access")
}

// =============================================================================
// GET /api/tasks/all
// =============================================================================

func TestTasks_ListAll_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/tasks/all", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestTasks_ListAll_ReturnsTasksAcrossProjects(t *testing.T) {
	t.Skip("requires direct DB access")
}

// =============================================================================
// GET /api/tasks/all/counts
// =============================================================================

func TestTasks_GetAllCounts_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/tasks/all/counts", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestTasks_GetAllCounts_ReturnsCountsAcrossProjects(t *testing.T) {
	t.Skip("requires direct DB access")
}

// =============================================================================
// GET /api/tasks/:id
// =============================================================================

func TestTasks_GetByID_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/tasks/some-id", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestTasks_GetByID_RequiresProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/tasks/some-id", e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestTasks_GetByID_ReturnsNotFoundForInvalidID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/tasks/00000000-0000-0000-0000-000000000000",
		e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusNotFound)
}

func TestTasks_GetByID_ReturnsTask(t *testing.T) {
	t.Skip("requires direct DB access")
}

// =============================================================================
// POST /api/tasks/:id/resolve
// =============================================================================

func TestTasks_Resolve_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/tasks/some-id/resolve", "", "",
		jsonBody(map[string]any{"resolution": "accepted"}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestTasks_Resolve_RequiresProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/tasks/some-id/resolve", e2eTestToken(), "",
		jsonBody(map[string]any{"resolution": "accepted"}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestTasks_Resolve_RequiresValidResolution(t *testing.T) {
	t.Skip("requires direct DB access")
}

func TestTasks_Resolve_AcceptsTask(t *testing.T) {
	t.Skip("requires direct DB access")
}

func TestTasks_Resolve_RejectsTask(t *testing.T) {
	t.Skip("requires direct DB access")
}

func TestTasks_Resolve_CannotResolveAlreadyResolved(t *testing.T) {
	t.Skip("requires direct DB access")
}

// =============================================================================
// POST /api/tasks/:id/cancel
// =============================================================================

func TestTasks_Cancel_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/tasks/some-id/cancel", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestTasks_Cancel_RequiresProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/tasks/some-id/cancel", e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestTasks_Cancel_CancelsTask(t *testing.T) {
	t.Skip("requires direct DB access")
}

func TestTasks_Cancel_CannotCancelAlreadyResolved(t *testing.T) {
	t.Skip("requires direct DB access")
}
