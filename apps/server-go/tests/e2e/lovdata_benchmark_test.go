package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/emergent-company/emergent/internal/testutil"
	"github.com/stretchr/testify/suite"
)

type LovdataBenchmarkSuite struct {
	suite.Suite
	Client     *testutil.HTTPClient
	Ctx        context.Context
	projectID  string
	agentDefID string
	apiKey     string
}

func TestLovdataBenchmarkSuite(t *testing.T) {
	suite.Run(t, new(LovdataBenchmarkSuite))
}

func (s *LovdataBenchmarkSuite) SetupSuite() {
	if os.Getenv("RUN_LOVDATA_BENCHMARK") != "true" {
		s.T().Skip("Skipping Lovdata benchmark test - set RUN_LOVDATA_BENCHMARK=true to run")
	}

	s.Ctx = context.Background()

	serverURL := os.Getenv("BENCHMARK_SERVER_URL")
	if serverURL == "" {
		serverURL = "http://mcj-emergent:3002"
	}
	s.Client = testutil.NewExternalHTTPClient(serverURL)

	s.projectID = os.Getenv("LOVDATA_PROJECT_ID")
	if s.projectID == "" {
		// Default to the Norwegian Law project we seeded
		s.projectID = "cfb7d045-a2ac-49b0-9ff4-48e545fec272"
	}

	s.apiKey = os.Getenv("LOVDATA_API_KEY")
	if s.apiKey == "" {
		s.apiKey = "emt_ec70233facfa29385abfef9bff015df72f08f7205be51f3034b42bf1484d0ec1"
	}

	s.agentDefID = os.Getenv("LOVDATA_AGENT_DEF_ID")
	if s.agentDefID == "" {
		s.agentDefID = "0938a58b-a673-440b-a490-cf692cda3c23"
	}
}

// runAgentQuery executes a natural language query against the graph agent and parses the result
func (s *LovdataBenchmarkSuite) runAgentQuery(query string) (string, []string) {
	req := map[string]any{
		"message":           query,
		"agentDefinitionId": s.agentDefID,
	}

	resp := s.Client.POST("/api/chat/stream",
		testutil.WithHeader("X-API-Key", s.apiKey),
		testutil.WithProjectID(s.projectID),
		testutil.WithJSONBody(req),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "Agent query failed: %s", resp.String())

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

	return fullResponse.String(), usedTools
}

func (s *LovdataBenchmarkSuite) TestBenchmark_Queries() {
	// 1. Verify project has laws loaded
	var count int
	countResp := s.Client.GET("/api/graph/objects/count?type=Law",
		testutil.WithHeader("X-API-Key", s.apiKey),
		testutil.WithProjectID(s.projectID),
	)
	if countResp.StatusCode == 200 {
		var res map[string]any
		json.Unmarshal(countResp.Body, &res)
		if countVal, ok := res["count"].(float64); ok {
			count = int(countVal)
		}
	}
	s.T().Logf("Law count in project: %d", count)
	if count < 10 {
		s.T().Skip("Project has fewer than 10 laws — run 'emergent db lovdata' first to seed data")
	}

	// QUERY 1: Direct Retrieval / 1 Hop
	s.T().Run("DirectRetrieval", func(t *testing.T) {
		response, tools := s.runAgentQuery("What ministry administers the Working Environment Act (Arbeidsmiljøloven)?")
		t.Logf("Tools used: %v", tools)
		t.Logf("Response:\n%s", response)

		s.Require().NotEmpty(tools, "Agent should use tools to answer")
		lowerResponse := strings.ToLower(response)
		// Usually Arbeids- og inkluderingsdepartementet
		s.Contains(lowerResponse, "departementet")
	})

	// QUERY 2: 2 Hop Traversal + Property Filter
	s.T().Run("PropertyFilterTraversal", func(t *testing.T) {
		response, tools := s.runAgentQuery("List 3 regulations that are administered by the Ministry of Finance (Finansdepartementet) and belong to the 'Tax' (Skatt) legal area.")
		t.Logf("Tools used: %v", tools)
		t.Logf("Response:\n%s", response)

		s.Require().NotEmpty(tools, "Agent should use tools to answer")
		// Response should mention some regulations and tax
		lowerResponse := strings.ToLower(response)
		s.True(strings.Contains(lowerResponse, "forskrift") || strings.Contains(lowerResponse, "skatt"))
	})

	// QUERY 3: Multi-hop + External Domain Intersection
	s.T().Run("MultiHopExternal", func(t *testing.T) {
		response, tools := s.runAgentQuery("Find a Norwegian law that implements an EU directive related to 'consumer protection'. Name the law and the EU directive.")
		t.Logf("Tools used: %v", tools)
		t.Logf("Response:\n%s", response)

		s.Require().NotEmpty(tools, "Agent should use tools to answer")
		lowerResponse := strings.ToLower(response)
		s.Contains(lowerResponse, "lov") // Must mention a "lov" (law)
	})

	// QUERY 4: Cross-Reference Self-Referential
	s.T().Run("CrossReferenceAmends", func(t *testing.T) {
		response, tools := s.runAgentQuery("Which Norwegian laws amend the 'Road Traffic Act' (Vegtrafikkloven)? List up to 5.")
		t.Logf("Tools used: %v", tools)
		t.Logf("Response:\n%s", response)

		s.Require().NotEmpty(tools, "Agent should use tools to answer")
		lowerResponse := strings.ToLower(response)
		s.Contains(lowerResponse, "lov")
	})

	// QUERY 5: Complex Aggregation/Semantic + Graph
	s.T().Run("ComplexAggregation", func(t *testing.T) {
		response, tools := s.runAgentQuery("Use a direct graph query to find which EU directive (by CELEX ID) is most frequently implemented by Norwegian regulations.")
		t.Logf("Tools used: %v", tools)
		t.Logf("Response:\n%s", response)

		s.Require().NotEmpty(tools, "Agent should use tools to answer")
		lowerResponse := strings.ToLower(response)
		s.Contains(lowerResponse, "3") // Most CELEX IDs start with '3'
	})
}
