// Package api_test — agents_webhooks_test.go
package api_test

import (
	"bytes"
	"fmt"
	"net/http"
	"testing"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
	"github.com/google/uuid"
)

// createWebhookAgent creates an agent with triggerType=webhook and returns its ID.
func createWebhookAgent(t *testing.T, projectID, token, name string) string {
	t.Helper()
	resp := doAPI(t, "POST", agentPath(projectID, ""), token, projectID, jsonBody(map[string]any{
		"projectId":    projectID,
		"name":         name,
		"strategyType": "extraction",
		"cronSchedule": "*/30 * * * *",
	}))
	body := mustStatus(t, resp, http.StatusCreated)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	if data, ok := result["data"].(map[string]any); ok {
		if id, ok := data["id"].(string); ok {
			return id
		}
	}
	if id, ok := result["id"].(string); ok {
		return id
	}
	t.Fatalf("createWebhookAgent: could not extract agent ID from response: %v", result)
	return ""
}

// createWebhookHook creates a hook for an agent and returns (hookID, token).
func createWebhookHook(t *testing.T, projectID, agentToken, agentID, label string) (string, string) {
	t.Helper()
	resp := doAPI(t, "POST", fmt.Sprintf("/api/projects/%s/agents/%s/hooks", projectID, agentID),
		agentToken, projectID, jsonBody(map[string]any{"label": label}))
	body := mustStatus(t, resp, http.StatusCreated)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	data := result["data"].(map[string]any)
	hookID := data["id"].(string)
	var hookToken string
	if t, ok := data["token"].(string); ok {
		hookToken = t
	}
	return hookID, hookToken
}

// doWebhookRequest sends a POST to the webhook endpoint with a raw Bearer token.
func doWebhookRequest(t *testing.T, hookID, rawToken string, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequest("POST", serverURL()+fmt.Sprintf("/api/webhooks/agents/%s", hookID),
		bytes.NewReader(body))
	if err != nil {
		t.Fatalf("doWebhookRequest: create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+rawToken)
	framework.SetAuthHeader(req, "") // clear then set raw
	req.Header.Set("Authorization", "Bearer "+rawToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("doWebhookRequest: do: %v", err)
	}
	return resp
}

// ─────────────────────────────────────────────────────────────────────────────
// Webhook Hook CRUD
// ─────────────────────────────────────────────────────────────────────────────

func TestAgentsWebhooks_CreateHook_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	token := e2eTestToken()
	agentID := createWebhookAgent(t, projectID, token, "Webhook Agent Create "+uuid.New().String()[:8])

	resp := doAPILogged(t, rl, "POST", fmt.Sprintf("/api/projects/%s/agents/%s/hooks", projectID, agentID),
		token, projectID, jsonBody(map[string]any{"label": "My CI Hook"}))
	body := mustStatus(t, resp, http.StatusCreated)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["success"] != true {
		t.Error("expected success true")
	}
	data := result["data"].(map[string]any)
	if data["id"] == "" {
		t.Error("expected non-empty hook ID")
	}
	if data["label"] != "My CI Hook" {
		t.Errorf("expected label 'My CI Hook', got %v", data["label"])
	}
	hookToken, _ := data["token"].(string)
	assertContains(t, hookToken, "whk_")
}

func TestAgentsWebhooks_CreateHook_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	token := e2eTestToken()
	agentID := createWebhookAgent(t, projectID, token, "Webhook Agent Auth "+uuid.New().String()[:8])

	resp := doAPILogged(t, rl, "POST", fmt.Sprintf("/api/projects/%s/agents/%s/hooks", projectID, agentID),
		"", projectID, jsonBody(map[string]any{"label": "Unauthenticated Hook"}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestAgentsWebhooks_CreateHook_MissingLabel(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	token := e2eTestToken()
	agentID := createWebhookAgent(t, projectID, token, "Webhook Agent NoLabel "+uuid.New().String()[:8])

	resp := doAPILogged(t, rl, "POST", fmt.Sprintf("/api/projects/%s/agents/%s/hooks", projectID, agentID),
		token, projectID, jsonBody(map[string]any{}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestAgentsWebhooks_CreateHook_NonExistentAgent(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	fakeID := uuid.New().String()

	resp := doAPILogged(t, rl, "POST", fmt.Sprintf("/api/projects/%s/agents/%s/hooks", projectID, fakeID),
		e2eTestToken(), projectID, jsonBody(map[string]any{"label": "Orphan Hook"}))
	mustStatus(t, resp, http.StatusNotFound)
}

func TestAgentsWebhooks_ListHooks_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	token := e2eTestToken()
	agentID := createWebhookAgent(t, projectID, token, "Webhook Agent List "+uuid.New().String()[:8])

	createWebhookHook(t, projectID, token, agentID, "Hook One")
	createWebhookHook(t, projectID, token, agentID, "Hook Two")

	resp := doAPILogged(t, rl, "GET", fmt.Sprintf("/api/projects/%s/agents/%s/hooks", projectID, agentID), token, projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["success"] != true {
		t.Error("expected success true")
	}
	data := result["data"].([]any)
	if len(data) != 2 {
		t.Errorf("expected 2 hooks, got %d", len(data))
	}
	// Token should NOT be returned in list
	for _, item := range data {
		hook := item.(map[string]any)
		if hook["token"] != nil {
			t.Error("token should not be returned in list response")
		}
	}
}

func TestAgentsWebhooks_ListHooks_Empty(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	token := e2eTestToken()
	agentID := createWebhookAgent(t, projectID, token, "Webhook Agent Empty "+uuid.New().String()[:8])

	resp := doAPILogged(t, rl, "GET", fmt.Sprintf("/api/projects/%s/agents/%s/hooks", projectID, agentID), token, projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	data, _ := result["data"].([]any)
	if len(data) != 0 {
		t.Errorf("expected empty hooks, got %d", len(data))
	}
}

func TestAgentsWebhooks_ListHooks_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	token := e2eTestToken()
	agentID := createWebhookAgent(t, projectID, token, "Webhook Agent ListAuth "+uuid.New().String()[:8])

	resp := doAPILogged(t, rl, "GET", fmt.Sprintf("/api/projects/%s/agents/%s/hooks", projectID, agentID), "", projectID, nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestAgentsWebhooks_DeleteHook_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	token := e2eTestToken()
	agentID := createWebhookAgent(t, projectID, token, "Webhook Agent Delete "+uuid.New().String()[:8])
	hookID, _ := createWebhookHook(t, projectID, token, agentID, "Hook To Delete")

	resp := doAPILogged(t, rl, "DELETE", fmt.Sprintf("/api/projects/%s/agents/%s/hooks/%s", projectID, agentID, hookID),
		token, projectID, nil)
	mustStatus(t, resp, http.StatusOK)

	listResp := doAPILogged(t, rl, "GET", fmt.Sprintf("/api/projects/%s/agents/%s/hooks", projectID, agentID), token, projectID, nil)
	listBody := mustStatus(t, listResp, http.StatusOK)
	var listResult map[string]any
	parseBodyJSON(t, listBody, &listResult)
	data, _ := listResult["data"].([]any)
	if len(data) != 0 {
		t.Errorf("expected empty hooks after delete, got %d", len(data))
	}
}

func TestAgentsWebhooks_DeleteHook_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	token := e2eTestToken()
	agentID := createWebhookAgent(t, projectID, token, "Webhook Agent DelAuth "+uuid.New().String()[:8])
	hookID, _ := createWebhookHook(t, projectID, token, agentID, "Hook To Not Delete")

	resp := doAPILogged(t, rl, "DELETE", fmt.Sprintf("/api/projects/%s/agents/%s/hooks/%s", projectID, agentID, hookID),
		"", projectID, nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestAgentsWebhooks_DeleteHook_NonExistentAgent(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	fakeAgentID := uuid.New().String()
	fakeHookID := uuid.New().String()

	resp := doAPILogged(t, rl, "DELETE", fmt.Sprintf("/api/projects/%s/agents/%s/hooks/%s", projectID, fakeAgentID, fakeHookID),
		e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusNotFound)
}

// ─────────────────────────────────────────────────────────────────────────────
// Public Webhook Receiver
// ─────────────────────────────────────────────────────────────────────────────

func TestAgentsWebhooks_ReceiveWebhook_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	token := e2eTestToken()
	agentID := createWebhookAgent(t, projectID, token, "Webhook Receiver Agent "+uuid.New().String()[:8])
	hookID, hookToken := createWebhookHook(t, projectID, token, agentID, "Receiver Hook")

	resp := doWebhookRequest(t, hookID, hookToken, jsonBody(map[string]any{}))
	mustStatus(t, resp, http.StatusAccepted)
}

func TestAgentsWebhooks_ReceiveWebhook_WithPayload(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	token := e2eTestToken()
	agentID := createWebhookAgent(t, projectID, token, "Webhook Payload Agent "+uuid.New().String()[:8])
	hookID, hookToken := createWebhookHook(t, projectID, token, agentID, "Payload Hook")

	resp := doWebhookRequest(t, hookID, hookToken, jsonBody(map[string]any{
		"prompt":  "Run extraction on new data",
		"context": map[string]any{"source": "CI/CD pipeline", "commit": "abc123"},
	}))
	mustStatus(t, resp, http.StatusAccepted)
}

func TestAgentsWebhooks_ReceiveWebhook_InvalidToken(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	token := e2eTestToken()
	agentID := createWebhookAgent(t, projectID, token, "Webhook Invalid Token Agent "+uuid.New().String()[:8])
	hookID, _ := createWebhookHook(t, projectID, token, agentID, "Invalid Token Hook")

	resp := doWebhookRequest(t, hookID, "whk_definitely_not_a_valid_token_here", jsonBody(map[string]any{}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestAgentsWebhooks_ReceiveWebhook_MissingAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	token := e2eTestToken()
	agentID := createWebhookAgent(t, projectID, token, "Webhook NoAuth Agent "+uuid.New().String()[:8])
	hookID, _ := createWebhookHook(t, projectID, token, agentID, "NoAuth Hook")

	// No auth header
	req, _ := http.NewRequest("POST", serverURL()+fmt.Sprintf("/api/webhooks/agents/%s", hookID),
		bytes.NewReader(jsonBody(map[string]any{})))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestAgentsWebhooks_ReceiveWebhook_NonExistentHook(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	fakeHookID := uuid.New().String()
	resp := doWebhookRequest(t, fakeHookID, "whk_some_token_that_wont_match", jsonBody(map[string]any{}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestAgentsWebhooks_ReceiveWebhook_TokenNotReusableAfterDelete(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	token := e2eTestToken()
	agentID := createWebhookAgent(t, projectID, token, "Webhook Deleted Hook Agent "+uuid.New().String()[:8])
	hookID, hookToken := createWebhookHook(t, projectID, token, agentID, "Temporary Hook")

	// Verify token works initially
	resp := doWebhookRequest(t, hookID, hookToken, jsonBody(map[string]any{}))
	mustStatus(t, resp, http.StatusAccepted)

	// Delete the hook
	delResp := doAPILogged(t, rl, "DELETE", fmt.Sprintf("/api/projects/%s/agents/%s/hooks/%s", projectID, agentID, hookID),
		token, projectID, nil)
	mustStatus(t, delResp, http.StatusOK)

	// Token should no longer work
	resp = doWebhookRequest(t, hookID, hookToken, jsonBody(map[string]any{}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestAgentsWebhooks_ReceiveWebhook_MultipleHooksIndependent(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	token := e2eTestToken()
	agentID := createWebhookAgent(t, projectID, token, "Webhook Multi-Hook Agent "+uuid.New().String()[:8])
	hookID1, token1 := createWebhookHook(t, projectID, token, agentID, "Hook Alpha")
	hookID2, token2 := createWebhookHook(t, projectID, token, agentID, "Hook Beta")

	resp1 := doWebhookRequest(t, hookID1, token1, jsonBody(map[string]any{}))
	mustStatus(t, resp1, http.StatusAccepted)

	resp2 := doWebhookRequest(t, hookID2, token2, jsonBody(map[string]any{}))
	mustStatus(t, resp2, http.StatusAccepted)

	// Cross-using tokens should fail
	respCross := doWebhookRequest(t, hookID1, token2, jsonBody(map[string]any{}))
	mustStatus(t, respCross, http.StatusUnauthorized)
}
