package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	sdkerrors "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/errors"
	mcpsdk "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/mcp"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/testutil"
)

// mcpCreateKeyReq builds the create-key request used across these tests.
func mcpCreateKeyReq(label string) mcpsdk.CreateAgentMCPKeyRequest {
	return mcpsdk.CreateAgentMCPKeyRequest{Label: label}
}

const (
	testProjectID  = "11111111-1111-1111-1111-111111111111"
	testAgentID    = "22222222-2222-2222-2222-222222222222"
	testEndpointID = "33333333-3333-3333-3333-333333333333"
	testKeyID      = "44444444-4444-4444-4444-444444444444"
)

func newAgentMCPClient(t *testing.T, mock *testutil.MockServer) *sdk.Client {
	t.Helper()
	client, err := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
		ProjectID: testProjectID,
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	return client
}

func TestCreateAgentEndpoint(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.OnJSON(http.MethodPost, "/api/projects/"+testProjectID+"/agents/"+testAgentID+"/mcp-endpoint", http.StatusCreated, map[string]any{
		"id":      testEndpointID,
		"agentId": testAgentID,
		"status":  "active",
		"mcpUrl":  "https://api.example.com/api/mcp/agents/" + testAgentID,
	})

	endpoint, err := newAgentMCPClient(t, mock).MCP.CreateAgentEndpoint(context.Background(), testProjectID, testAgentID)
	if err != nil {
		t.Fatalf("CreateAgentEndpoint() error = %v", err)
	}
	if endpoint.ID != testEndpointID {
		t.Errorf("id = %q, want %q", endpoint.ID, testEndpointID)
	}
	if endpoint.Status != "active" {
		t.Errorf("status = %q, want active", endpoint.Status)
	}
	if endpoint.MCPURL == "" {
		t.Error("expected non-empty MCP URL")
	}
}

func TestGetAgentEndpoint(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.OnJSON(http.MethodGet, "/api/projects/"+testProjectID+"/agents/"+testAgentID+"/mcp-endpoint", http.StatusOK, map[string]any{
		"id":      testEndpointID,
		"agentId": testAgentID,
		"status":  "active",
	})

	endpoint, err := newAgentMCPClient(t, mock).MCP.GetAgentEndpoint(context.Background(), testProjectID, testAgentID)
	if err != nil {
		t.Fatalf("GetAgentEndpoint() error = %v", err)
	}
	if endpoint.ID != testEndpointID {
		t.Errorf("id = %q, want %q", endpoint.ID, testEndpointID)
	}
}

func TestGetAgentEndpoint_NotFound(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.OnJSON(http.MethodGet, "/api/projects/"+testProjectID+"/agents/"+testAgentID+"/mcp-endpoint", http.StatusNotFound, map[string]any{
		"error": map[string]any{"code": "not_found", "message": "Agent MCP endpoint not found"},
	})

	_, err := newAgentMCPClient(t, mock).MCP.GetAgentEndpoint(context.Background(), testProjectID, testAgentID)
	if err == nil {
		t.Fatal("expected an error for 404")
	}
	var apiErr *sdkerrors.Error
	if !sdkerrors.IsNotFound(err) || !errors.As(err, &apiErr) {
		t.Fatalf("expected typed 404 error, got %T: %v", err, err)
	}
	if apiErr.Message == "" {
		t.Error("expected the server message to be preserved")
	}
}

func TestRevokeAgentEndpoint(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.OnJSON(http.MethodDelete, "/api/projects/"+testProjectID+"/agent-mcp-endpoints/"+testEndpointID, http.StatusOK, map[string]string{"status": "revoked"})

	if err := newAgentMCPClient(t, mock).MCP.RevokeAgentEndpoint(context.Background(), testProjectID, testEndpointID); err != nil {
		t.Fatalf("RevokeAgentEndpoint() error = %v", err)
	}
}

func TestCreateAgentKey(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	var gotBody map[string]any
	mock.On(http.MethodPost, "/api/projects/"+testProjectID+"/agent-mcp-endpoints/"+testEndpointID+"/keys", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         testKeyID,
			"endpointId": testEndpointID,
			"label":      "ci",
			"status":     "active",
			"createdAt":  time.Now().UTC(),
			"updatedAt":  time.Now().UTC(),
			"token":      "emt_secret_value",
			"mcpUrl":     "https://api.example.com/api/mcp/agents/" + testAgentID,
		})
	})

	secret, err := newAgentMCPClient(t, mock).MCP.CreateAgentKey(context.Background(), testProjectID, testEndpointID, mcpCreateKeyReq("ci"))
	if err != nil {
		t.Fatalf("CreateAgentKey() error = %v", err)
	}
	if secret.Token != "emt_secret_value" {
		t.Errorf("token = %q, want emt_secret_value", secret.Token)
	}
	if secret.Label != "ci" {
		t.Errorf("label = %q, want ci", secret.Label)
	}
	if gotBody["label"] != "ci" {
		t.Errorf("request label = %v, want ci", gotBody["label"])
	}
}

func TestCreateAgentKey_Conflict(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.OnJSON(http.MethodPost, "/api/projects/"+testProjectID+"/agent-mcp-endpoints/"+testEndpointID+"/keys", http.StatusConflict, map[string]any{
		"error": map[string]any{"code": "agent_mcp_key_label_exists", "message": "An active key with this label already exists"},
	})

	_, err := newAgentMCPClient(t, mock).MCP.CreateAgentKey(context.Background(), testProjectID, testEndpointID, mcpCreateKeyReq("ci"))
	if !sdkerrors.IsConflict(err) {
		t.Fatalf("expected 409 conflict, got %T: %v", err, err)
	}
}

func TestListAgentKeys(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.OnJSON(http.MethodGet, "/api/projects/"+testProjectID+"/agent-mcp-endpoints/"+testEndpointID+"/keys", http.StatusOK, map[string]any{
		"keys": []map[string]any{
			{"id": testKeyID, "endpointId": testEndpointID, "label": "ci", "status": "active"},
		},
		"total": 1,
	})

	list, err := newAgentMCPClient(t, mock).MCP.ListAgentKeys(context.Background(), testProjectID, testEndpointID)
	if err != nil {
		t.Fatalf("ListAgentKeys() error = %v", err)
	}
	if list.Total != 1 || len(list.Keys) != 1 {
		t.Fatalf("expected 1 key, got total=%d len=%d", list.Total, len(list.Keys))
	}
	if list.Keys[0].Label != "ci" {
		t.Errorf("label = %q, want ci", list.Keys[0].Label)
	}
}

func TestRevokeAgentKey(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.OnJSON(http.MethodDelete, "/api/projects/"+testProjectID+"/agent-mcp-keys/"+testKeyID, http.StatusOK, map[string]string{"status": "revoked"})

	if err := newAgentMCPClient(t, mock).MCP.RevokeAgentKey(context.Background(), testProjectID, testKeyID); err != nil {
		t.Fatalf("RevokeAgentKey() error = %v", err)
	}
}

func TestRotateAgentKey(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.OnJSON(http.MethodPost, "/api/projects/"+testProjectID+"/agent-mcp-keys/"+testKeyID+"/rotate", http.StatusOK, map[string]any{
		"id":     testKeyID,
		"label":  "ci",
		"status": "active",
		"token":  "emt_rotated_value",
	})

	secret, err := newAgentMCPClient(t, mock).MCP.RotateAgentKey(context.Background(), testProjectID, testKeyID)
	if err != nil {
		t.Fatalf("RotateAgentKey() error = %v", err)
	}
	if secret.Token != "emt_rotated_value" {
		t.Errorf("token = %q, want emt_rotated_value", secret.Token)
	}
	if secret.ID != testKeyID {
		t.Errorf("id = %q, want %q (key identity preserved)", secret.ID, testKeyID)
	}
}

func TestListAgentSessions(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	var gotStatus string
	mock.On(http.MethodGet, "/api/projects/"+testProjectID+"/agent-mcp-endpoints/"+testEndpointID+"/sessions", func(w http.ResponseWriter, r *http.Request) {
		gotStatus = r.URL.Query().Get("status")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"sessions": []map[string]any{
				{"session_id": "sess-1", "key_id": testKeyID, "key_label": "ci", "status": "active", "turn_count": 2, "total_steps": 5},
			},
		})
	})

	list, err := newAgentMCPClient(t, mock).MCP.ListAgentSessions(context.Background(), testProjectID, testEndpointID, "active")
	if err != nil {
		t.Fatalf("ListAgentSessions() error = %v", err)
	}
	if gotStatus != "active" {
		t.Errorf("status query = %q, want active", gotStatus)
	}
	if len(list.Sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(list.Sessions))
	}
	if list.Sessions[0].SessionID != "sess-1" || list.Sessions[0].KeyLabel != "ci" {
		t.Errorf("unexpected session: %+v", list.Sessions[0])
	}
}

func TestListAgentSessions_NoFilter(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.OnJSON(http.MethodGet, "/api/projects/"+testProjectID+"/agent-mcp-endpoints/"+testEndpointID+"/sessions", http.StatusOK, map[string]any{"sessions": []any{}})

	list, err := newAgentMCPClient(t, mock).MCP.ListAgentSessions(context.Background(), testProjectID, testEndpointID, "")
	if err != nil {
		t.Fatalf("ListAgentSessions() error = %v", err)
	}
	if len(list.Sessions) != 0 {
		t.Fatalf("expected no sessions, got %d", len(list.Sessions))
	}
}
