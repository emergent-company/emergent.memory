package e2e

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// GraphQueryEndpointTestSuite tests the POST /api/projects/:projectId/query endpoint.
type GraphQueryEndpointTestSuite struct {
	testutil.BaseSuite
}

func TestGraphQueryEndpointSuite(t *testing.T) {
	suite.Run(t, new(GraphQueryEndpointTestSuite))
}

func (s *GraphQueryEndpointTestSuite) SetupSuite() {
	s.SetDBSuffix("graph_query_endpoint")
	s.BaseSuite.SetupSuite()
}

// TestQueryEndpoint_ReturnsSSEStream verifies that the query endpoint returns a valid
// SSE stream with at least a "done" event. The graph-query-agent is automatically
// created when the project is created.
func (s *GraphQueryEndpointTestSuite) TestQueryEndpoint_ReturnsSSEStream() {
	resp := s.Client.POST("/api/projects/"+s.ProjectID+"/query",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"message": "what is in this project?",
		}),
	)

	s.Equal(http.StatusOK, resp.StatusCode, "expected 200 OK from query endpoint: %s", resp.String())

	// Parse SSE events — must have at least a "done" event.
	events := parseSSEEvents(resp.String())
	s.Greater(len(events), 0, "expected at least one SSE event")

	var hasDone bool
	for _, ev := range events {
		if t, ok := ev["type"].(string); ok && t == "done" {
			hasDone = true
			break
		}
	}
	s.True(hasDone, "expected a 'done' SSE event; got: %s", resp.String())
}

// TestQueryEndpoint_RequiresMessage verifies that an empty message is rejected.
func (s *GraphQueryEndpointTestSuite) TestQueryEndpoint_RequiresMessage() {
	resp := s.Client.POST("/api/projects/"+s.ProjectID+"/query",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"message": "",
		}),
	)

	s.Equal(http.StatusBadRequest, resp.StatusCode, "expected 400 for empty message")
}

// TestQueryEndpoint_GraphQueryAgentHiddenFromList verifies that the graph-query-agent
// (created automatically when the project is created) does NOT appear in the public
// agent definitions list.
func (s *GraphQueryEndpointTestSuite) TestQueryEndpoint_GraphQueryAgentHiddenFromList() {
	// Call the query endpoint to exercise the path (agent already exists from project creation).
	queryResp := s.Client.POST("/api/projects/"+s.ProjectID+"/query",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"message": "ping",
		}),
	)
	s.Equal(http.StatusOK, queryResp.StatusCode)

	// Allow the async DB writes from QueryStream to complete before the next request.
	time.Sleep(100 * time.Millisecond)

	// Now list agent definitions — graph-query-agent must not appear.
	listResp := s.Client.GET("/api/projects/"+s.ProjectID+"/agent-definitions",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusOK, listResp.StatusCode, "expected 200 from agent-definitions list: %s", listResp.String())

	// Response shape: {"success":true,"data":[...]} where data may be null or [].
	var result struct {
		Success bool                     `json:"success"`
		Data    []map[string]interface{} `json:"data"`
	}
	err := json.Unmarshal(listResp.Body, &result)
	s.NoError(err)
	s.True(result.Success)

	// graph-query-agent must not appear in the list.
	for _, def := range result.Data {
		name, _ := def["name"].(string)
		s.False(
			strings.EqualFold(name, "graph-query-agent"),
			"graph-query-agent should be hidden from the public list, but found: %v", def,
		)
	}
}

// TestQueryEndpoint_IsIdempotent verifies that calling the query endpoint multiple times
// doesn't create duplicate graph-query-agents (the agent is created once at project
// creation and EnsureGraphQueryAgent is a no-op thereafter).
func (s *GraphQueryEndpointTestSuite) TestQueryEndpoint_IsIdempotent() {
	for i := 0; i < 2; i++ {
		resp := s.Client.POST("/api/projects/"+s.ProjectID+"/query",
			testutil.WithAuth("e2e-test-user"),
			testutil.WithProjectID(s.ProjectID),
			testutil.WithJSONBody(map[string]any{
				"message": "idempotency check",
			}),
		)
		s.Equal(http.StatusOK, resp.StatusCode, "call %d failed: %s", i+1, resp.String())
		// Allow async DB writes from QueryStream to drain before the next iteration.
		time.Sleep(100 * time.Millisecond)
	}

	// Use the DB directly to count graph-query-agent definitions for this project.
	// EnsureGraphQueryAgent is idempotent — there should be exactly 1.
	var count int
	err := s.DB().NewRaw(
		`SELECT count(*) FROM kb.agent_definitions WHERE project_id = ? AND name = 'graph-query-agent'`,
		s.ProjectID,
	).Scan(s.Ctx, &count)
	s.NoError(err)
	s.Equal(1, count, "expected exactly 1 graph-query-agent, found %d", count)
}
