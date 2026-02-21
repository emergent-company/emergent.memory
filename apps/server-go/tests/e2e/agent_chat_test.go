package e2e

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent/internal/testutil"
)

type AgentChatTestSuite struct {
	testutil.BaseSuite
}

func TestAgentChatSuite(t *testing.T) {
	suite.Run(t, new(AgentChatTestSuite))
}

func (s *AgentChatTestSuite) SetupSuite() {
	s.SetDBSuffix("agentchat")
	s.BaseSuite.SetupSuite()
}

// =============================================================================
// Test: Install Default Agents
// =============================================================================

func (s *AgentChatTestSuite) TestInstallDefaultAgents() {
	// 4.5 Install default agents and verify
	resp := s.Client.POST("/api/admin/projects/"+s.ProjectID+"/install-default-agents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusCreated, resp.StatusCode)

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)
	s.True(result["success"].(bool))

	agentDef, ok := result["data"].(map[string]any)
	s.True(ok)
	s.Equal("graph-query-agent", agentDef["name"])

	// 4.6 Call install endpoint twice to verify idempotency
	resp2 := s.Client.POST("/api/admin/projects/"+s.ProjectID+"/install-default-agents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusOK, resp2.StatusCode) // Returns 200 OK on existing

	var result2 map[string]any
	err2 := json.Unmarshal(resp2.Body, &result2)
	s.NoError(err2)
	s.True(result2["success"].(bool))
	agentDef2 := result2["data"].(map[string]any)
	s.Equal(agentDef["id"], agentDef2["id"]) // Same ID means no duplicate created
}

// =============================================================================
// Test: Agent-Backed Chat Flow
// =============================================================================

func (s *AgentChatTestSuite) TestStreamChat_AgentBacked() {
	// First, install the default agent
	installResp := s.Client.POST("/api/admin/projects/"+s.ProjectID+"/install-default-agents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	// Either 201 Created or 200 OK if already installed
	s.True(installResp.StatusCode == http.StatusCreated || installResp.StatusCode == http.StatusOK)

	var result map[string]any
	json.Unmarshal(installResp.Body, &result)
	agentDef := result["data"].(map[string]any)
	agentDefID := agentDef["id"].(string)

	// 5.1 & 5.2 & 5.3 & 5.4 Send stream request with agentDefinitionId
	req := map[string]any{
		"message":           "Hello graph agent",
		"agentDefinitionId": agentDefID,
	}

	resp := s.Client.POST("/api/chat/stream",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(req),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	// Parse SSE events
	events := parseSSEEvents(resp.String())

	// Verify we got the meta event with conversation ID
	s.Greater(len(events), 5, "Should have meta, tool start, tool end, tokens, and done events")
	metaEvent := events[0]
	s.Equal("meta", metaEvent["type"])
	convID := metaEvent["conversationId"].(string)

	// Check tool and token events
	hasToolStart := false
	hasToolEnd := false
	var tokenText string
	for _, evt := range events {
		if evt["type"] == "mcp_tool" {
			if evt["status"] == "started" {
				hasToolStart = true
				s.Equal("search_entities", evt["tool"])
			} else if evt["status"] == "completed" {
				hasToolEnd = true
				s.Equal("search_entities", evt["tool"])
			}
		} else if evt["type"] == "token" {
			tokenText += evt["token"].(string)
		}
	}
	s.True(hasToolStart)
	s.True(hasToolEnd)
	s.Equal("I found it.", tokenText)

	// Allow some time for async DB writes to complete
	time.Sleep(100 * time.Millisecond)

	// 5.3 Verify conversation has agent_definition_id and assistant response is persisted
	getConvResp := s.Client.GET("/api/chat/"+convID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusOK, getConvResp.StatusCode)

	var conv map[string]any
	json.Unmarshal(getConvResp.Body, &conv)
	s.Equal(agentDefID, conv["agentDefinitionId"])

	messages := conv["messages"].([]any)
	s.Len(messages, 2) // user msg + assistant msg

	assistantMsg := messages[1].(map[string]any)
	s.Equal("assistant", assistantMsg["role"])
	s.Equal("I found it.", assistantMsg["content"])

	// Check retrieval context has agent_run_id
	retrievalCtx, ok := assistantMsg["retrievalContext"].(map[string]any)
	s.True(ok, "retrievalContext should be an object")
	agentRunID, ok := retrievalCtx["agent_run_id"].(string)
	s.True(ok, "should have agent_run_id")
	s.NotEmpty(agentRunID)

	// 5.4 Verify agent run trace is persisted to DB (mock trace added in our handler)
	runResp := s.Client.GET("/api/projects/"+s.ProjectID+"/agent-runs/"+agentRunID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusOK, runResp.StatusCode)

	msgResp := s.Client.GET("/api/projects/"+s.ProjectID+"/agent-runs/"+agentRunID+"/messages",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusOK, msgResp.StatusCode)

	tcResp := s.Client.GET("/api/projects/"+s.ProjectID+"/agent-runs/"+agentRunID+"/tool-calls",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusOK, tcResp.StatusCode)

	// 5.6 Multi-turn conversation
	req2 := map[string]any{
		"message":        "Tell me more about it",
		"conversationId": convID,
	}

	resp2 := s.Client.POST("/api/chat/stream",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(req2),
	)

	s.Equal(http.StatusOK, resp2.StatusCode)
	events2 := parseSSEEvents(resp2.String())
	s.Greater(len(events2), 2)
	s.Equal("meta", events2[0]["type"])
	s.Equal(convID, events2[0]["conversationId"])

	// Allow some time for async DB writes to complete
	time.Sleep(100 * time.Millisecond)

	// Verify conversation now has 4 messages (2 user, 2 assistant)
	getConvResp2 := s.Client.GET("/api/chat/"+convID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusOK, getConvResp2.StatusCode)

	var conv2 map[string]any
	json.Unmarshal(getConvResp2.Body, &conv2)
	messages2 := conv2["messages"].([]any)
	s.Len(messages2, 4)
}

func (s *AgentChatTestSuite) TestStreamChat_InvalidAgentDefinition() {
	// 5.5 Send invalid agent definition
	req := map[string]any{
		"message":           "Hello",
		"agentDefinitionId": "00000000-0000-0000-0000-000000000000",
	}

	resp := s.Client.POST("/api/chat/stream",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(req),
	)

	// Should return 400 JSON error
	s.Equal(http.StatusBadRequest, resp.StatusCode)
	s.Contains(resp.Headers.Get("Content-Type"), "application/json")
}
