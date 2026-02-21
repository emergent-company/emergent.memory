package e2e

import (
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent/internal/testutil"
)

// AgentGraphQueryLiveSuite tests natural language graph queries against live test data in mcj-emergent.
// It verifies that the agent can successfully use vector search (via hybrid_search/semantic_search)
// to answer questions about relationships.
//
// Usage:
//
//	TEST_SERVER_URL=https://api.mcj-emergent.com TEST_API_KEY=<key> go test -v -run TestAgentGraphQueryLiveSuite
type AgentGraphQueryLiveSuite struct {
	testutil.BaseSuite

	projectID  string
	agentDefID string
	apiKey     string
}

func TestAgentGraphQueryLiveSuite(t *testing.T) {
	suite.Run(t, new(AgentGraphQueryLiveSuite))
}

func (s *AgentGraphQueryLiveSuite) SetupSuite() {
	s.SetDBSuffix("agent_graph_live")
	s.BaseSuite.SetupSuite()

	if !s.Client.IsExternal() {
		s.T().Skip("Skipping live test - TEST_SERVER_URL not set")
	}

	s.apiKey = os.Getenv("TEST_API_KEY")
	if s.apiKey == "" {
		// Fallback to the standard mcj-emergent key if running against it
		s.apiKey = "4334d35abf06be28271d7c5cd5a1cf02a6e9b4c2753db816e48419330e023060"
	}

	// Use standard mcj-emergent project and default graph query agent
	s.projectID = "b04e0cd4-1800-4f42-a875-18172541d9fc"
	s.agentDefID = "70356e5f-2c97-4ce4-9754-ec14e15a2a13"
}

// TestVectorSearchOnRelationships verifies the agent can use vector search tools
// to find semantically matching relationships and reason about them.
func (s *AgentGraphQueryLiveSuite) TestVectorSearchOnRelationships() {
	s.T().Log("=== Testing Agent Vector Search on Relationships ===")

	req := map[string]any{
		"message":           "Find relationships related to pattern usage using semantic search, and tell me what types of relationships you found.",
		"agentDefinitionId": s.agentDefID,
	}

	s.T().Log("Sending query to agent: " + req["message"].(string))

	resp := s.Client.POST("/api/chat/stream",
		testutil.WithHeader("X-API-Key", s.apiKey),
		testutil.WithProjectID(s.projectID),
		testutil.WithJSONBody(req),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode)

	events := parseSSEEvents(resp.String())
	s.Require().Greater(len(events), 0, "Should receive SSE events from live server")

	// Parse out tool calls and text response
	var usedTools []string
	var fullResponse strings.Builder

	for _, evt := range events {
		if evt["type"] == "mcp_tool" {
			status, _ := evt["status"].(string)
			if status == "started" {
				toolName, _ := evt["tool"].(string)
				usedTools = append(usedTools, toolName)
				s.T().Logf("Agent invoked tool: %s", toolName)
			}
		} else if evt["type"] == "token" {
			token, _ := evt["token"].(string)
			fullResponse.WriteString(token)
		}
	}

	finalResponse := fullResponse.String()
	s.T().Logf("\nAgent Response:\n%s\n", finalResponse)

	// Verify the agent successfully used one of the search tools
	s.Require().NotEmpty(usedTools, "Agent should have used at least one search tool")

	// Check if hybrid_search, semantic_search, or list_relationships was used
	usedVectorSearch := false
	for _, t := range usedTools {
		if t == "hybrid_search" || t == "semantic_search" || t == "list_relationships" {
			usedVectorSearch = true
			break
		}
	}
	s.True(usedVectorSearch, "Agent must use a search tool to find relationships")

	// The query was specifically about 'pattern usage' which matches the 'uses_pattern' relationship
	// in the live DB. The agent should find it and mention it.
	lowerResponse := strings.ToLower(finalResponse)
	s.Contains(lowerResponse, "uses_pattern", "Agent response should mention the 'uses_pattern' relationship type")

	s.T().Log("✓ Agent successfully used vector search to discover relationships")
}

// TestMultiStepGraphTraversal tests the agent's ability to chain relationship searches
func (s *AgentGraphQueryLiveSuite) TestMultiStepGraphTraversal() {
	s.T().Log("=== Testing Agent Multi-Step Relationship Traversal ===")

	req := map[string]any{
		"message":           "What is the ChatPage entity, and what other entities are connected to it?",
		"agentDefinitionId": s.agentDefID,
	}

	resp := s.Client.POST("/api/chat/stream",
		testutil.WithHeader("X-API-Key", s.apiKey),
		testutil.WithProjectID(s.projectID),
		testutil.WithJSONBody(req),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode)

	events := parseSSEEvents(resp.String())

	var usedTools []string
	var fullResponse strings.Builder

	for _, evt := range events {
		if evt["type"] == "mcp_tool" {
			status, _ := evt["status"].(string)
			if status == "started" {
				toolName, _ := evt["tool"].(string)
				usedTools = append(usedTools, toolName)
			}
		} else if evt["type"] == "token" {
			token, _ := evt["token"].(string)
			fullResponse.WriteString(token)
		}
	}

	s.T().Logf("Tools used: %v", usedTools)
	s.T().Logf("Agent Response:\n%s\n", fullResponse.String())

	s.Require().GreaterOrEqual(len(usedTools), 1, "Agent should use tools to find the ChatPage")

	lowerResponse := strings.ToLower(fullResponse.String())
	s.True(strings.Contains(lowerResponse, "chat") || strings.Contains(lowerResponse, "page"),
		"Agent should describe the ChatPage")

	// Usually connected to error-boundary, layout, etc.
	s.T().Log("✓ Agent successfully traversed relationships")
}

// helper to parse SSE format since testutil doesn't export it
