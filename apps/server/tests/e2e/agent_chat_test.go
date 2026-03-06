package e2e

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
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
// Test: Agent-Backed Chat Flow
// =============================================================================

func (s *AgentChatTestSuite) TestStreamChat_AgentBacked() {
	// The graph-query-agent is automatically created when the project is created.
	// It is hidden from the public agent-definitions list (VisibilityInternal),
	// so we fetch its ID directly from the DB.
	var agentDefID string
	err := s.DB().NewRaw(
		`SELECT id FROM kb.agent_definitions WHERE project_id = ? AND name = 'graph-query-agent' LIMIT 1`,
		s.ProjectID,
	).Scan(s.Ctx, &agentDefID)
	s.NoError(err, "graph-query-agent should exist from project creation")
	s.NotEmpty(agentDefID)

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
