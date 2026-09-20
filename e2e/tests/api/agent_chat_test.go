// Package api_test — agent_chat_test.go
// Migrated from agent_chat_test.go. DB-dependent tests are skipped.
package api_test

import (
	"net/http"
	"testing"
)

func TestAgentChat_StreamChat_InvalidAgentDefinition(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfNoLLMProvider(t)

	projectID, _ := setupProjectLogged(t, rl)
	configureProjectModel(t, projectID)
	resp := doAPILogged(t, rl, "POST", "/api/chat/stream", e2eTestToken(), projectID, jsonBody(map[string]any{
		"message":           "Hello",
		"agentDefinitionId": "00000000-0000-0000-0000-000000000000",
	}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestAgentChat_StreamChat_AgentBacked_SkipsWithoutDB(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	rl.Skipf("requires direct DB access to fetch graph-query-agent definition ID")
}
