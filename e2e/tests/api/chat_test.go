// Package api_test — chat_test.go
//
// Tests for the chat API endpoints (/api/chat/...).
// Ported from emergent.memory/apps/server/tests/e2e/chat_test.go
package api_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

// createTestConversation creates a conversation and returns the parsed response body.
func createTestConversation(t *testing.T, projectID, title, message string) map[string]any {
	t.Helper()
	resp := doAPI(t, "POST", "/api/chat/conversations", e2eTestToken(), projectID,
		jsonBody(map[string]any{"title": title, "message": message}))
	body := mustStatus(t, resp, http.StatusCreated)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	return result
}

// =============================================================================
// Test: Authentication & Authorization
// =============================================================================

func TestChat_ListConversations_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/chat/conversations", "", projectID, nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestChat_ListConversations_RequiresChatUseScope(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/chat/conversations", "no-scope", projectID, nil)
	mustStatus(t, resp, http.StatusForbidden)
}

func TestChat_ListConversations_RequiresProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/chat/conversations", e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusBadRequest)
}

// =============================================================================
// Test: List Conversations
// =============================================================================

func TestChat_ListConversations_Empty(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/chat/conversations", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)

	convs, ok := result["conversations"].([]any)
	if !ok {
		t.Fatal("expected 'conversations' array in response")
	}
	if len(convs) != 0 {
		t.Errorf("expected 0 conversations for fresh project, got %d", len(convs))
	}
	if result["total"] != float64(0) {
		t.Errorf("expected total=0, got %v", result["total"])
	}
}

func TestChat_ListConversations_ReturnsConversations(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	conv := createTestConversation(t, projectID, "Test Conversation", "Hello world")

	resp := doAPILogged(t, rl, "GET", "/api/chat/conversations", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)

	convs, ok := result["conversations"].([]any)
	if !ok || len(convs) < 1 {
		t.Fatal("expected at least 1 conversation")
	}
	firstConv := convs[0].(map[string]any)
	if firstConv["id"] != conv["id"] {
		t.Errorf("expected conv id %v, got %v", conv["id"], firstConv["id"])
	}
	if firstConv["title"] != "Test Conversation" {
		t.Errorf("expected title 'Test Conversation', got %v", firstConv["title"])
	}
}

func TestChat_ListConversations_Pagination(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	for i := 0; i < 5; i++ {
		createTestConversation(t, projectID, "Conversation", "Message")
	}

	resp := doAPILogged(t, rl, "GET", "/api/chat/conversations?limit=3", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	convs := result["conversations"].([]any)
	if len(convs) != 3 {
		t.Errorf("expected 3 conversations with limit=3, got %d", len(convs))
	}
	if result["total"] != float64(5) {
		t.Errorf("expected total=5, got %v", result["total"])
	}

	// Test offset
	resp2 := doAPILogged(t, rl, "GET", "/api/chat/conversations?limit=3&offset=3", e2eTestToken(), projectID, nil)
	body2 := mustStatus(t, resp2, http.StatusOK)
	var result2 map[string]any
	parseBodyJSON(t, body2, &result2)
	convs2 := result2["conversations"].([]any)
	if len(convs2) != 2 {
		t.Errorf("expected 2 conversations at offset=3, got %d", len(convs2))
	}
}

// =============================================================================
// Test: Create Conversation
// =============================================================================

func TestChat_CreateConversation_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/chat/conversations", e2eTestToken(), projectID,
		jsonBody(map[string]any{
			"title":   "New Conversation",
			"message": "Hello, this is my first message",
		}))
	body := mustStatus(t, resp, http.StatusCreated)

	var result map[string]any
	parseBodyJSON(t, body, &result)

	if result["id"] == "" || result["id"] == nil {
		t.Error("expected non-empty id")
	}
	if result["title"] != "New Conversation" {
		t.Errorf("expected title 'New Conversation', got %v", result["title"])
	}
	if result["projectId"] != projectID {
		t.Errorf("expected projectId=%s, got %v", projectID, result["projectId"])
	}

	messages, ok := result["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("expected 1 initial message, got %v", result["messages"])
	}
	msg := messages[0].(map[string]any)
	if msg["role"] != "user" {
		t.Errorf("expected role=user, got %v", msg["role"])
	}
	if msg["content"] != "Hello, this is my first message" {
		t.Errorf("unexpected message content: %v", msg["content"])
	}
}

func TestChat_CreateConversation_RequiresTitle(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/chat/conversations", e2eTestToken(), projectID,
		jsonBody(map[string]any{"message": "Hello"}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestChat_CreateConversation_RequiresMessage(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/chat/conversations", e2eTestToken(), projectID,
		jsonBody(map[string]any{"title": "Test"}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestChat_CreateConversation_WithCanonicalID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	canonicalID := uuid.New().String()
	resp := doAPILogged(t, rl, "POST", "/api/chat/conversations", e2eTestToken(), projectID,
		jsonBody(map[string]any{
			"title":       "Refinement Chat",
			"message":     "Let's refine this object",
			"canonicalId": canonicalID,
		}))
	body := mustStatus(t, resp, http.StatusCreated)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["canonicalId"] != canonicalID {
		t.Errorf("expected canonicalId=%s, got %v", canonicalID, result["canonicalId"])
	}
}

// =============================================================================
// Test: Get Conversation
// =============================================================================

func TestChat_GetConversation_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	conv := createTestConversation(t, projectID, "Test Conversation", "Initial message")

	resp := doAPILogged(t, rl, "GET", "/api/chat/"+conv["id"].(string), e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["id"] != conv["id"] {
		t.Errorf("expected id=%v, got %v", conv["id"], result["id"])
	}
	if result["title"] != "Test Conversation" {
		t.Errorf("expected title 'Test Conversation', got %v", result["title"])
	}
	messages := result["messages"].([]any)
	if len(messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(messages))
	}
}

func TestChat_GetConversation_NotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/chat/00000000-0000-0000-0000-000000000999", e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusNotFound)
}

func TestChat_GetConversation_InvalidID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/chat/invalid-id", e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusBadRequest)
}

// =============================================================================
// Test: Update Conversation
// =============================================================================

func TestChat_UpdateConversation_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	conv := createTestConversation(t, projectID, "Original Title", "Message")

	resp := doAPILogged(t, rl, "PATCH", "/api/chat/"+conv["id"].(string), e2eTestToken(), projectID,
		jsonBody(map[string]any{"title": "Updated Title"}))
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["title"] != "Updated Title" {
		t.Errorf("expected title 'Updated Title', got %v", result["title"])
	}
}

func TestChat_UpdateConversation_RequiresChatAdminScope(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	projectID, _ := setupProjectLogged(t, rl)
	conv := createTestConversation(t, projectID, "Test", "Message")

	resp := doAPILogged(t, rl, "PATCH", "/api/chat/"+conv["id"].(string), "no-scope", projectID,
		jsonBody(map[string]any{"title": "New Title"}))
	mustStatus(t, resp, http.StatusForbidden)
}

func TestChat_UpdateConversation_NotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "PATCH", "/api/chat/00000000-0000-0000-0000-000000000999", e2eTestToken(), projectID,
		jsonBody(map[string]any{"title": "New Title"}))
	mustStatus(t, resp, http.StatusNotFound)
}

// =============================================================================
// Test: Delete Conversation
// =============================================================================

func TestChat_DeleteConversation_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	conv := createTestConversation(t, projectID, "To Delete", "Message")

	resp := doAPILogged(t, rl, "DELETE", "/api/chat/"+conv["id"].(string), e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["status"] != "deleted" {
		t.Errorf("expected status=deleted, got %v", result["status"])
	}

	// Verify it's gone
	resp2 := doAPILogged(t, rl, "GET", "/api/chat/"+conv["id"].(string), e2eTestToken(), projectID, nil)
	mustStatus(t, resp2, http.StatusNotFound)
}

func TestChat_DeleteConversation_RequiresChatAdminScope(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	projectID, _ := setupProjectLogged(t, rl)
	conv := createTestConversation(t, projectID, "Test", "Message")

	resp := doAPILogged(t, rl, "DELETE", "/api/chat/"+conv["id"].(string), "no-scope", projectID, nil)
	mustStatus(t, resp, http.StatusForbidden)
}

func TestChat_DeleteConversation_NotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "DELETE", "/api/chat/00000000-0000-0000-0000-000000000999", e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusNotFound)
}

// =============================================================================
// Test: Add Message
// =============================================================================

func TestChat_AddMessage_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	conv := createTestConversation(t, projectID, "Test", "First message")

	resp := doAPILogged(t, rl, "POST", "/api/chat/"+conv["id"].(string)+"/messages", e2eTestToken(), projectID,
		jsonBody(map[string]any{
			"role":    "assistant",
			"content": "Hello! How can I help you?",
		}))
	body := mustStatus(t, resp, http.StatusCreated)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["id"] == "" || result["id"] == nil {
		t.Error("expected non-empty id")
	}
	if result["role"] != "assistant" {
		t.Errorf("expected role=assistant, got %v", result["role"])
	}
	if result["content"] != "Hello! How can I help you?" {
		t.Errorf("unexpected content: %v", result["content"])
	}
	if result["conversationId"] != conv["id"] {
		t.Errorf("expected conversationId=%v, got %v", conv["id"], result["conversationId"])
	}
}

func TestChat_AddMessage_RequiresValidRole(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	conv := createTestConversation(t, projectID, "Test", "Message")

	resp := doAPILogged(t, rl, "POST", "/api/chat/"+conv["id"].(string)+"/messages", e2eTestToken(), projectID,
		jsonBody(map[string]any{"role": "invalid-role", "content": "Test message"}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestChat_AddMessage_ConversationNotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/chat/00000000-0000-0000-0000-000000000999/messages", e2eTestToken(), projectID,
		jsonBody(map[string]any{"role": "user", "content": "Test message"}))
	mustStatus(t, resp, http.StatusNotFound)
}

// =============================================================================
// Test: Project Isolation
// =============================================================================

func TestChat_ProjectIsolation(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, orgID := setupProjectLogged(t, rl)
	project2ID := createProject(t, orgID, uniqueName("e2e-project-2"))

	conv := createTestConversation(t, projectID, "Project 1 Chat", "Message")

	// Access from project 2 should not find it
	resp := doAPILogged(t, rl, "GET", "/api/chat/"+conv["id"].(string), e2eTestToken(), project2ID, nil)
	mustStatus(t, resp, http.StatusNotFound)

	// List in project 2 should be empty
	resp2 := doAPILogged(t, rl, "GET", "/api/chat/conversations", e2eTestToken(), project2ID, nil)
	body2 := mustStatus(t, resp2, http.StatusOK)
	var result map[string]any
	parseBodyJSON(t, body2, &result)
	convs := result["conversations"].([]any)
	if len(convs) != 0 {
		t.Errorf("expected 0 conversations in project 2, got %d", len(convs))
	}
}

// =============================================================================
// Test: Cross-tenant membership enforcement (issue #864)
// =============================================================================

// TestChat_NonMemberForbidden proves a caller who is not a member of the owning
// organization cannot reach a project's conversations by sending its id in the
// X-Project-ID header. The attacker is a distinct authenticated principal
// ("e2e-attacker" — resolved to a fresh user with no memberships) that has no
// membership in the org that owns the project. Before the fix these returned
// 200/201 because the handlers scoped by the header-derived project ID without
// any membership check.
func TestChat_NonMemberForbidden(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	conv := createTestConversation(t, projectID, "Secret Conversation", "secret message")
	convID := conv["id"].(string)

	resp := doAPILogged(t, rl, "GET", "/api/chat/"+convID, "e2e-attacker", projectID, nil)
	mustStatus(t, resp, http.StatusForbidden)

	resp = doAPILogged(t, rl, "GET", "/api/chat/conversations", "e2e-attacker", projectID, nil)
	mustStatus(t, resp, http.StatusForbidden)

	resp = doAPILogged(t, rl, "POST", "/api/chat/conversations", "e2e-attacker", projectID,
		jsonBody(map[string]any{"title": "stolen", "message": "hi"}))
	mustStatus(t, resp, http.StatusForbidden)

	resp = doAPILogged(t, rl, "POST", "/api/chat/"+convID+"/messages", "e2e-attacker", projectID,
		jsonBody(map[string]any{"role": "user", "content": "hi"}))
	mustStatus(t, resp, http.StatusForbidden)
}

// =============================================================================
// Test: Stream Chat
// =============================================================================

func TestChat_StreamChat_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/chat/stream", "", projectID,
		jsonBody(map[string]any{"message": "Hello"}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestChat_StreamChat_RequiresChatUseScope(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/chat/stream", "no-scope", projectID,
		jsonBody(map[string]any{"message": "Hello"}))
	mustStatus(t, resp, http.StatusForbidden)
}

func TestChat_StreamChat_RequiresProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/chat/stream", e2eTestToken(), "",
		jsonBody(map[string]any{"message": "Hello"}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestChat_StreamChat_RequiresMessage(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/chat/stream", e2eTestToken(), projectID,
		jsonBody(map[string]any{}))
	body := mustStatus(t, resp, http.StatusBadRequest)
	// Should return JSON error, not SSE
	assertContains(t, resp.Header.Get("Content-Type"), "application/json")
	_ = body
}

func TestChat_StreamChat_CreatesNewConversation(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfNoLLMProvider(t)

	projectID, _ := setupProjectLogged(t, rl)
	configureProjectModel(t, projectID)
	resp := doAPILogged(t, rl, "POST", "/api/chat/stream", e2eTestToken(), projectID,
		jsonBody(map[string]any{"message": "Hello, this is my first message!"}))
	body := mustStatus(t, resp, http.StatusOK)

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected Content-Type=text/event-stream, got %s", ct)
	}

	events := parseSSEEvents(body)
	if len(events) < 2 {
		t.Fatalf("expected at least 2 events (meta and done), got %d", len(events))
	}

	metaEvent := events[0]
	if metaEvent["type"] != "meta" {
		t.Errorf("expected first event type=meta, got %v", metaEvent["type"])
	}
	convID, _ := metaEvent["conversationId"].(string)
	if convID == "" {
		t.Fatal("expected non-empty conversationId in meta event")
	}

	lastEvent := events[len(events)-1]
	if lastEvent["type"] != "done" {
		t.Errorf("expected last event type=done, got %v", lastEvent["type"])
	}

	// Verify conversation was created
	getResp := doAPILogged(t, rl, "GET", "/api/chat/"+convID, e2eTestToken(), projectID, nil)
	convBody := mustStatus(t, getResp, http.StatusOK)

	var conv map[string]any
	parseBodyJSON(t, convBody, &conv)
	if conv["title"] != "Hello, this is my first message!" {
		t.Errorf("expected title='Hello, this is my first message!', got %v", conv["title"])
	}

	messages := conv["messages"].([]any)
	if len(messages) < 1 {
		t.Fatal("expected at least 1 message")
	}
	firstMsg := messages[0].(map[string]any)
	if firstMsg["role"] != "user" {
		t.Errorf("expected role=user, got %v", firstMsg["role"])
	}
	if firstMsg["content"] != "Hello, this is my first message!" {
		t.Errorf("unexpected message content: %v", firstMsg["content"])
	}
}

func TestChat_StreamChat_UsesExistingConversation(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfNoLLMProvider(t)

	projectID, _ := setupProjectLogged(t, rl)
	configureProjectModel(t, projectID)
	conv := createTestConversation(t, projectID, "Existing Conversation", "First message")
	convID := conv["id"].(string)

	resp := doAPILogged(t, rl, "POST", "/api/chat/stream", e2eTestToken(), projectID,
		jsonBody(map[string]any{
			"message":        "Follow up message",
			"conversationId": convID,
		}))
	body := mustStatus(t, resp, http.StatusOK)

	events := parseSSEEvents(body)
	if len(events) < 2 {
		t.Fatalf("expected at least 2 events, got %d", len(events))
	}

	metaEvent := events[0]
	if metaEvent["type"] != "meta" {
		t.Errorf("expected first event type=meta, got %v", metaEvent["type"])
	}
	if metaEvent["conversationId"] != convID {
		t.Errorf("expected conversationId=%s, got %v", convID, metaEvent["conversationId"])
	}

	// Verify message was added
	getResp := doAPILogged(t, rl, "GET", "/api/chat/"+convID, e2eTestToken(), projectID, nil)
	updatedBody := mustStatus(t, getResp, http.StatusOK)
	var updated map[string]any
	parseBodyJSON(t, updatedBody, &updated)
	messages := updated["messages"].([]any)
	if len(messages) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(messages))
	}
	secondMsg := messages[1].(map[string]any)
	if secondMsg["role"] != "user" {
		t.Errorf("expected role=user, got %v", secondMsg["role"])
	}
	if secondMsg["content"] != "Follow up message" {
		t.Errorf("expected 'Follow up message', got %v", secondMsg["content"])
	}
}

func TestChat_StreamChat_ConversationNotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfNoLLMProvider(t)

	projectID, _ := setupProjectLogged(t, rl)
	configureProjectModel(t, projectID)
	resp := doAPILogged(t, rl, "POST", "/api/chat/stream", e2eTestToken(), projectID,
		jsonBody(map[string]any{
			"message":        "Hello",
			"conversationId": "00000000-0000-0000-0000-000000000999",
		}))
	mustStatus(t, resp, http.StatusNotFound)
	assertContains(t, resp.Header.Get("Content-Type"), "application/json")
}

func TestChat_StreamChat_InvalidConversationID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/chat/stream", e2eTestToken(), projectID,
		jsonBody(map[string]any{
			"message":        "Hello",
			"conversationId": "not-a-uuid",
		}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestChat_StreamChat_WithCanonicalID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfNoLLMProvider(t)

	projectID, _ := setupProjectLogged(t, rl)
	configureProjectModel(t, projectID)
	canonicalID := fmt.Sprintf("00000000-0000-0000-0000-%012d", time.Now().UnixNano()%1000000000000)

	resp := doAPILogged(t, rl, "POST", "/api/chat/stream", e2eTestToken(), projectID,
		jsonBody(map[string]any{
			"message":     "Refine this object",
			"canonicalId": canonicalID,
		}))
	body := mustStatus(t, resp, http.StatusOK)

	events := parseSSEEvents(body)
	if len(events) < 2 {
		t.Fatalf("expected at least 2 events, got %d", len(events))
	}
	convID, _ := events[0]["conversationId"].(string)

	// Verify conversation has canonical ID
	getResp := doAPILogged(t, rl, "GET", "/api/chat/"+convID, e2eTestToken(), projectID, nil)
	convBody := mustStatus(t, getResp, http.StatusOK)
	var conv map[string]any
	parseBodyJSON(t, convBody, &conv)
	if conv["canonicalId"] != canonicalID {
		t.Errorf("expected canonicalId=%s, got %v", canonicalID, conv["canonicalId"])
	}
}

func TestChat_StreamChat_LLMNotConfigured(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	// Server returns 503 before opening SSE stream when no LLM is configured.
	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/chat/stream", e2eTestToken(), projectID,
		jsonBody(map[string]any{"message": "Hello"}))
	mustStatus(t, resp, http.StatusServiceUnavailable)
}

func TestChat_StreamChat_TitleTruncation(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfNoLLMProvider(t)

	projectID, _ := setupProjectLogged(t, rl)
	configureProjectModel(t, projectID)
	longMessage := "This is a very long message that should be truncated when used as the conversation title because titles have a maximum length"

	resp := doAPILogged(t, rl, "POST", "/api/chat/stream", e2eTestToken(), projectID,
		jsonBody(map[string]any{"message": longMessage}))
	body := mustStatus(t, resp, http.StatusOK)

	events := parseSSEEvents(body)
	convID, _ := events[0]["conversationId"].(string)

	getResp := doAPILogged(t, rl, "GET", "/api/chat/"+convID, e2eTestToken(), projectID, nil)
	convBody := mustStatus(t, getResp, http.StatusOK)
	var conv map[string]any
	parseBodyJSON(t, convBody, &conv)

	title, _ := conv["title"].(string)
	if len(title) > 54 {
		t.Errorf("title should be at most 54 chars (50+...), got %d: %s", len(title), title)
	}
	assertContains(t, title, "...")
}

func TestChat_StreamChat_ProjectIsolation(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfNoLLMProvider(t)

	projectID, orgID := setupProjectLogged(t, rl)
	configureProjectModel(t, projectID)
	project2ID := createProject(t, orgID, uniqueName("e2e-project-stream-2"))
	configureProjectModel(t, project2ID)

	conv := createTestConversation(t, projectID, "Project 1 Chat", "Message")
	convID := conv["id"].(string)

	resp := doAPILogged(t, rl, "POST", "/api/chat/stream", e2eTestToken(), project2ID,
		jsonBody(map[string]any{
			"message":        "Follow up from wrong project",
			"conversationId": convID,
		}))
	mustStatus(t, resp, http.StatusNotFound)
}
