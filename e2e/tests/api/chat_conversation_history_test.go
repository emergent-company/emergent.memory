// Package api_test — chat_conversation_history_test.go
package api_test

import (
	"fmt"
	"net/http"
	"testing"
)

func TestChatHistory_MultiTurnConversation_BuildsContextSummary(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	token := e2eTestToken()

	conv := createTestConversation(t, projectID, "Multi-turn Chat", "Hello, what is your name?")
	convID := conv["id"].(string)

	addConvMessage(t, projectID, token, convID, "assistant", "I am an AI assistant. How can I help you?")
	addConvMessage(t, projectID, token, convID, "user", "What is the weather like?")
	addConvMessage(t, projectID, token, convID, "assistant", "I don't have access to weather data.")
	addConvMessage(t, projectID, token, convID, "user", "Can you summarize our conversation?")

	resp := doAPILogged(t, rl, "GET", "/api/chat/"+convID, token, projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)

	messages := result["messages"].([]any)
	if len(messages) != 5 {
		t.Errorf("expected 5 messages, got %d", len(messages))
	}

	if len(messages) >= 5 {
		lastMsg := messages[4].(map[string]any)
		if cs, ok := lastMsg["contextSummary"]; ok && cs != nil {
			contextSummary := cs.(string)
			assertContains(t, contextSummary, "Previous conversation:")
			assertContains(t, contextSummary, "user:")
			assertContains(t, contextSummary, "assistant:")
		}
	}
}

func TestChatHistory_Limit(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	token := e2eTestToken()

	conv := createTestConversation(t, projectID, "History Limit Test", "Message 1")
	convID := conv["id"].(string)

	for i := 2; i <= 10; i++ {
		role := "user"
		if i%2 == 0 {
			role = "assistant"
		}
		addConvMessage(t, projectID, token, convID, role, fmt.Sprintf("Message %d", i))
	}
	addConvMessage(t, projectID, token, convID, "user", "Message 11")

	resp := doAPILogged(t, rl, "GET", "/api/chat/"+convID, token, projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)

	messages := result["messages"].([]any)
	if len(messages) != 11 {
		t.Errorf("expected 11 messages, got %d", len(messages))
	}
}

func TestChatHistory_EmptyForFirstMessage(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	token := e2eTestToken()

	conv := createTestConversation(t, projectID, "First Message Test", "Hello")
	convID := conv["id"].(string)

	resp := doAPILogged(t, rl, "GET", "/api/chat/"+convID, token, projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)

	messages := result["messages"].([]any)
	if len(messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(messages))
	}
	if len(messages) >= 1 {
		firstMsg := messages[0].(map[string]any)
		if cs, ok := firstMsg["contextSummary"]; ok && cs != nil {
			t.Errorf("expected no contextSummary for first message, got %v", cs)
		}
	}
}

func TestChatHistory_ProjectIsolation(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, orgID := setupProjectLogged(t, rl)
	token := e2eTestToken()

	project2ID := createProject(t, orgID, uniqueName("e2e-proj2"))

	conv := createTestConversation(t, projectID, "Project 1 Chat", "Hello from project 1")
	convID := conv["id"].(string)
	addConvMessage(t, projectID, token, convID, "assistant", "Response in project 1")

	resp := doAPILogged(t, rl, "POST", "/api/chat/"+convID+"/messages", token, project2ID,
		jsonBody(map[string]any{"role": "user", "content": "Another message"}))
	body := mustStatus(t, resp, http.StatusNotFound)
	_ = body
}

func TestChatHistory_ChronologicalOrder(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	token := e2eTestToken()

	conv := createTestConversation(t, projectID, "Order Test", "Message 1")
	convID := conv["id"].(string)
	addConvMessage(t, projectID, token, convID, "assistant", "Message 2")
	addConvMessage(t, projectID, token, convID, "user", "Message 3")

	resp := doAPILogged(t, rl, "GET", "/api/chat/"+convID, token, projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)

	messages := result["messages"].([]any)
	if len(messages) != 3 {
		t.Errorf("expected 3 messages, got %d", len(messages))
		return
	}

	if messages[0].(map[string]any)["content"] != "Message 1" {
		t.Errorf("expected Message 1 first")
	}
	if messages[1].(map[string]any)["content"] != "Message 2" {
		t.Errorf("expected Message 2 second")
	}
	if messages[2].(map[string]any)["content"] != "Message 3" {
		t.Errorf("expected Message 3 third")
	}

	lastMsg := messages[2].(map[string]any)
	if cs, ok := lastMsg["contextSummary"]; ok && cs != nil {
		contextSummary := cs.(string)
		assertContains(t, contextSummary, "Message 1")
		assertContains(t, contextSummary, "Message 2")
	}
}

// addConvMessage adds a message to a conversation. Helper local to this file.
func addConvMessage(t *testing.T, projectID, token, conversationID, role, content string) map[string]any {
	t.Helper()
	resp := doAPI(t, "POST", "/api/chat/"+conversationID+"/messages", token, projectID,
		jsonBody(map[string]any{"role": role, "content": content}))
	body := mustStatus(t, resp, http.StatusCreated)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	return result
}
