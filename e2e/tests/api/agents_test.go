// Package api_test — agents_test.go
package api_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// agentPath builds the project-scoped agent URL path.
func agentPath(projectID, suffix string) string {
	return "/api/projects/" + projectID + "/agents" + suffix
}

// defPath builds the project-scoped agent-definition URL path.
func defPath(projectID, suffix string) string {
	return "/api/projects/" + projectID + "/agent-definitions" + suffix
}

// ─────────────────────────────────────────────────────────────────────────────
// Agent CRUD
// ─────────────────────────────────────────────────────────────────────────────

func TestAgents_CreateAgent_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	token := e2eTestToken()

	resp := doAPILogged(t, rl, "POST", agentPath(projectID, ""), token, projectID, jsonBody(map[string]any{
		"name":         "Test Agent",
		"strategyType": "extraction",
		"cronSchedule": "*/30 * * * *",
	}))
	body := mustStatus(t, resp, http.StatusCreated)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["success"] != true {
		t.Error("expected success true")
	}
	data := result["data"].(map[string]any)
	if data["id"] == "" {
		t.Error("expected non-empty agent ID")
	}
	if data["name"] != "Test Agent" {
		t.Errorf("expected name 'Test Agent', got %v", data["name"])
	}
	if data["strategyType"] != "extraction" {
		t.Errorf("expected strategyType 'extraction', got %v", data["strategyType"])
	}
}

func TestAgents_CreateAgent_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", agentPath(projectID, ""), "", projectID, jsonBody(map[string]any{
		"name":         "Test Agent",
		"strategyType": "extraction",
		"cronSchedule": "*/30 * * * *",
	}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestAgents_CreateAgent_MissingName(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	token := e2eTestToken()

	resp := doAPILogged(t, rl, "POST", agentPath(projectID, ""), token, projectID, jsonBody(map[string]any{
		"strategyType": "extraction",
		"cronSchedule": "*/30 * * * *",
	}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestAgents_CreateAgent_MissingProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	// With project-scoped routing, a non-existent project UUID in the URL → 404.
	fakeProjectID := uuid.New().String()
	token := e2eTestToken()

	resp := doAPILogged(t, rl, "POST", agentPath(fakeProjectID, ""), token, fakeProjectID, jsonBody(map[string]any{
		"name":         "Test Agent",
		"strategyType": "extraction",
		"cronSchedule": "*/30 * * * *",
	}))
	mustStatus(t, resp, http.StatusNotFound)
}

func TestAgents_ListAgents_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	token := e2eTestToken()

	// Create an agent first
	doAPILogged(t, rl, "POST", agentPath(projectID, ""), token, projectID, jsonBody(map[string]any{
		"name":         "List Agent " + uuid.New().String()[:8],
		"strategyType": "extraction",
		"cronSchedule": "*/30 * * * *",
	}))

	resp := doAPILogged(t, rl, "GET", agentPath(projectID, ""), token, projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["success"] != true {
		t.Error("expected success true")
	}
	data := result["data"].([]any)
	if len(data) < 1 {
		t.Error("expected at least 1 agent")
	}
}

func TestAgents_ListAgents_NonexistentProject_NotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	// Project-scoped routes now enforce membership: a non-existent project
	// addressed by an authenticated caller is a 404 (no existence oracle).
	fakeProjectID := uuid.New().String()
	resp := doAPILogged(t, rl, "GET", agentPath(fakeProjectID, ""), e2eTestToken(), fakeProjectID, nil)
	mustStatus(t, resp, http.StatusNotFound)
}

func TestAgents_ListAgents_NonMember_Forbidden(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	// Project owned by e2e-test-user.
	projectID, _ := setupProjectLogged(t, rl)

	// "all-scopes" is a distinct user with no membership in the project's org:
	// the project-scoped route is denied with 403.
	resp := doAPILogged(t, rl, "GET", agentPath(projectID, ""), "all-scopes", projectID, nil)
	mustStatus(t, resp, http.StatusForbidden)
}

func TestAgents_GetAgent_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	token := e2eTestToken()

	createResp := doAPILogged(t, rl, "POST", agentPath(projectID, ""), token, projectID, jsonBody(map[string]any{
		"name":         "Get Agent " + uuid.New().String()[:8],
		"strategyType": "extraction",
		"cronSchedule": "*/30 * * * *",
	}))
	createBody := mustStatus(t, createResp, http.StatusCreated)
	var createResult map[string]any
	parseBodyJSON(t, createBody, &createResult)
	agentID := createResult["data"].(map[string]any)["id"].(string)

	resp := doAPILogged(t, rl, "GET", agentPath(projectID, "/"+agentID), token, projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["success"] != true {
		t.Error("expected success true")
	}
	if result["data"].(map[string]any)["id"] != agentID {
		t.Error("agent ID mismatch")
	}
}

func TestAgents_GetAgent_NotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", agentPath(projectID, "/"+uuid.New().String()), e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusNotFound)
}

func TestAgents_UpdateAgent_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	token := e2eTestToken()

	createResp := doAPILogged(t, rl, "POST", agentPath(projectID, ""), token, projectID, jsonBody(map[string]any{
		"name":         "Update Agent " + uuid.New().String()[:8],
		"strategyType": "extraction",
		"cronSchedule": "*/30 * * * *",
	}))
	createBody := mustStatus(t, createResp, http.StatusCreated)
	var createResult map[string]any
	parseBodyJSON(t, createBody, &createResult)
	agentID := createResult["data"].(map[string]any)["id"].(string)

	resp := doAPILogged(t, rl, "PATCH", agentPath(projectID, "/"+agentID), token, projectID, jsonBody(map[string]any{
		"name":    "Updated Agent Name",
		"enabled": false,
	}))
	body := mustStatus(t, resp, http.StatusOK)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["data"].(map[string]any)["name"] != "Updated Agent Name" {
		t.Error("expected name to be updated")
	}
}

func TestAgents_UpdateAgent_NotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "PATCH", agentPath(projectID, "/"+uuid.New().String()), e2eTestToken(), projectID,
		jsonBody(map[string]any{"name": "Updated Name"}))
	mustStatus(t, resp, http.StatusNotFound)
}

func TestAgents_DeleteAgent_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	token := e2eTestToken()

	createResp := doAPILogged(t, rl, "POST", agentPath(projectID, ""), token, projectID, jsonBody(map[string]any{
		"name":         "Delete Agent " + uuid.New().String()[:8],
		"strategyType": "extraction",
		"cronSchedule": "*/30 * * * *",
	}))
	createBody := mustStatus(t, createResp, http.StatusCreated)
	var createResult map[string]any
	parseBodyJSON(t, createBody, &createResult)
	agentID := createResult["data"].(map[string]any)["id"].(string)

	resp := doAPILogged(t, rl, "DELETE", agentPath(projectID, "/"+agentID), token, projectID, nil)
	mustStatus(t, resp, http.StatusOK)

	getRec := doAPILogged(t, rl, "GET", agentPath(projectID, "/"+agentID), token, projectID, nil)
	mustStatus(t, getRec, http.StatusNotFound)
}

func TestAgents_DeleteAgent_NotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "DELETE", agentPath(projectID, "/"+uuid.New().String()), e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusNotFound)
}

// ─────────────────────────────────────────────────────────────────────────────
// Agent Trigger
// ─────────────────────────────────────────────────────────────────────────────

func TestAgents_TriggerAgent_StubMode(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	token := e2eTestToken()

	createResp := doAPILogged(t, rl, "POST", agentPath(projectID, ""), token, projectID, jsonBody(map[string]any{
		"name":         "Trigger Agent " + uuid.New().String()[:8],
		"strategyType": "extraction",
		"cronSchedule": "*/30 * * * *",
	}))
	createBody := mustStatus(t, createResp, http.StatusCreated)
	var createResult map[string]any
	parseBodyJSON(t, createBody, &createResult)
	agentID := createResult["data"].(map[string]any)["id"].(string)

	resp := doAPILogged(t, rl, "POST", agentPath(projectID, "/"+agentID+"/trigger"), token, projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["success"] != true {
		t.Error("expected success true")
	}
}

func TestAgents_TriggerAgent_NotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", agentPath(projectID, "/"+uuid.New().String()+"/trigger"), e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusNotFound)
}

// ─────────────────────────────────────────────────────────────────────────────
// Agent Runs
// ─────────────────────────────────────────────────────────────────────────────

func TestAgents_GetAgentRuns_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	token := e2eTestToken()

	createResp := doAPILogged(t, rl, "POST", agentPath(projectID, ""), token, projectID, jsonBody(map[string]any{
		"name":         "Runs Agent " + uuid.New().String()[:8],
		"strategyType": "extraction",
		"cronSchedule": "*/30 * * * *",
	}))
	createBody := mustStatus(t, createResp, http.StatusCreated)
	var createResult map[string]any
	parseBodyJSON(t, createBody, &createResult)
	agentID := createResult["data"].(map[string]any)["id"].(string)

	// Trigger to create a run
	doAPILogged(t, rl, "POST", agentPath(projectID, "/"+agentID+"/trigger"), token, projectID, nil)

	resp := doAPILogged(t, rl, "GET", agentPath(projectID, "/"+agentID+"/runs"), token, projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["success"] != true {
		t.Error("expected success true")
	}
	data := result["data"].([]any)
	if len(data) < 1 {
		t.Error("expected at least 1 run")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Agent Definition CRUD
// ─────────────────────────────────────────────────────────────────────────────

func TestAgents_CreateDefinition_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	token := e2eTestToken()

	resp := doAPILogged(t, rl, "POST", defPath(projectID, ""), token, projectID, jsonBody(map[string]any{
		"name":         "Test Definition",
		"description":  "A test agent definition",
		"systemPrompt": "You are a helpful assistant.",
		"tools":        []string{"search", "read_file"},
		"flowType":     "single",
		"visibility":   "project",
	}))
	body := mustStatus(t, resp, http.StatusCreated)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["success"] != true {
		t.Error("expected success true")
	}
	data := result["data"].(map[string]any)
	if data["id"] == "" {
		t.Error("expected non-empty ID")
	}
	if data["name"] != "Test Definition" {
		t.Errorf("expected name 'Test Definition', got %v", data["name"])
	}
}

func TestAgents_CreateDefinition_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", defPath(projectID, ""), "", projectID, jsonBody(map[string]any{
		"name": "Test Definition",
	}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestAgents_CreateDefinition_NonexistentProject_NotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	// Project-scoped routes now enforce membership: a non-existent project
	// addressed by an authenticated caller is a 404 (no existence oracle).
	fakeProjectID := uuid.New().String()
	resp := doAPILogged(t, rl, "POST", defPath(fakeProjectID, ""), e2eTestToken(), fakeProjectID, jsonBody(map[string]any{
		"name": "Test Definition " + fakeProjectID[:8],
	}))
	mustStatus(t, resp, http.StatusNotFound)
}

func TestAgents_CreateDefinition_MissingName(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", defPath(projectID, ""), e2eTestToken(), projectID, jsonBody(map[string]any{
		"flowType": "single",
	}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestAgents_ListDefinitions_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	token := e2eTestToken()

	doAPILogged(t, rl, "POST", defPath(projectID, ""), token, projectID, jsonBody(map[string]any{
		"name": "List Def " + uuid.New().String()[:8],
	}))

	resp := doAPILogged(t, rl, "GET", defPath(projectID, ""), token, projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["success"] != true {
		t.Error("expected success true")
	}
	data := result["data"].([]any)
	if len(data) < 1 {
		t.Error("expected at least 1 definition")
	}
}

func TestAgents_ListDefinitions_NonexistentProject_NotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	// Project-scoped routes now enforce membership: a non-existent project
	// addressed by an authenticated caller is a 404 (no existence oracle).
	fakeProjectID := uuid.New().String()
	resp := doAPILogged(t, rl, "GET", defPath(fakeProjectID, ""), e2eTestToken(), fakeProjectID, nil)
	mustStatus(t, resp, http.StatusNotFound)
}

func TestAgents_GetDefinition_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	token := e2eTestToken()

	createResp := doAPILogged(t, rl, "POST", defPath(projectID, ""), token, projectID, jsonBody(map[string]any{
		"name":         "Get Def " + uuid.New().String()[:8],
		"systemPrompt": "System prompt here",
		"tools":        []string{"tool_a", "tool_b"},
	}))
	createBody := mustStatus(t, createResp, http.StatusCreated)
	var createResult map[string]any
	parseBodyJSON(t, createBody, &createResult)
	defID := createResult["data"].(map[string]any)["id"].(string)

	resp := doAPILogged(t, rl, "GET", defPath(projectID, "/"+defID), token, projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["data"].(map[string]any)["id"] != defID {
		t.Error("definition ID mismatch")
	}
}

func TestAgents_GetDefinition_NotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", defPath(projectID, "/"+uuid.New().String()), e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusNotFound)
}

func TestAgents_UpdateDefinition_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	token := e2eTestToken()

	createResp := doAPILogged(t, rl, "POST", defPath(projectID, ""), token, projectID, jsonBody(map[string]any{
		"name":       "Update Def " + uuid.New().String()[:8],
		"visibility": "project",
	}))
	createBody := mustStatus(t, createResp, http.StatusCreated)
	var createResult map[string]any
	parseBodyJSON(t, createBody, &createResult)
	defID := createResult["data"].(map[string]any)["id"].(string)

	resp := doAPILogged(t, rl, "PATCH", defPath(projectID, "/"+defID), token, projectID, jsonBody(map[string]any{
		"name":       "Updated Def Name",
		"visibility": "external",
		"tools":      []string{"new_tool"},
		"isDefault":  true,
		"maxSteps":   100,
	}))
	body := mustStatus(t, resp, http.StatusOK)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["data"].(map[string]any)["name"] != "Updated Def Name" {
		t.Error("expected name to be updated")
	}
}

func TestAgents_UpdateDefinition_NotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "PATCH", defPath(projectID, "/"+uuid.New().String()), e2eTestToken(), projectID,
		jsonBody(map[string]any{"name": "Updated Name"}))
	mustStatus(t, resp, http.StatusNotFound)
}

func TestAgents_DeleteDefinition_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	token := e2eTestToken()

	createResp := doAPILogged(t, rl, "POST", defPath(projectID, ""), token, projectID, jsonBody(map[string]any{
		"name": "Delete Def " + uuid.New().String()[:8],
	}))
	createBody := mustStatus(t, createResp, http.StatusCreated)
	var createResult map[string]any
	parseBodyJSON(t, createBody, &createResult)
	defID := createResult["data"].(map[string]any)["id"].(string)

	resp := doAPILogged(t, rl, "DELETE", defPath(projectID, "/"+defID), token, projectID, nil)
	mustStatus(t, resp, http.StatusOK)

	getRec := doAPILogged(t, rl, "GET", defPath(projectID, "/"+defID), token, projectID, nil)
	mustStatus(t, getRec, http.StatusNotFound)
}

func TestAgents_DeleteDefinition_NotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "DELETE", defPath(projectID, "/"+uuid.New().String()), e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusNotFound)
}

// ─────────────────────────────────────────────────────────────────────────────
// Project-Scoped Run History
// ─────────────────────────────────────────────────────────────────────────────

func TestAgents_ListProjectRuns_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	token := e2eTestToken()

	createResp := doAPILogged(t, rl, "POST", agentPath(projectID, ""), token, projectID, jsonBody(map[string]any{
		"name":         "Project Runs Agent " + uuid.New().String()[:8],
		"strategyType": "extraction",
		"cronSchedule": "*/30 * * * *",
	}))
	createBody := mustStatus(t, createResp, http.StatusCreated)
	var createResult map[string]any
	parseBodyJSON(t, createBody, &createResult)
	agentID := createResult["data"].(map[string]any)["id"].(string)

	doAPILogged(t, rl, "POST", agentPath(projectID, "/"+agentID+"/trigger"), token, projectID, nil)

	resp := doAPILogged(t, rl, "GET", "/api/projects/"+projectID+"/agent-runs", token, "", nil)
	body := mustStatus(t, resp, http.StatusOK)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["success"] != true {
		t.Error("expected success true")
	}
}

func TestAgents_ListProjectRuns_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/projects/"+projectID+"/agent-runs", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}
