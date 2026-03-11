package e2e

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// MCPNewToolsTestSuite tests the newly-added MCP tools:
// documents, skills, embedding status, ADK sessions, traces, and query_knowledge.
type MCPNewToolsTestSuite struct {
	testutil.BaseSuite
}

func TestMCPNewToolsSuite(t *testing.T) {
	suite.Run(t, new(MCPNewToolsTestSuite))
}

func (s *MCPNewToolsTestSuite) SetupSuite() {
	s.SetDBSuffix("mcp_new_tools")
	s.BaseSuite.SetupSuite()
}

// callTool is a helper that calls a tools/call RPC and returns the parsed JSON
// from the first content text block. It fails the test on HTTP or JSON errors.
func (s *MCPNewToolsTestSuite) callTool(toolName string, args map[string]any) map[string]any {
	resp := s.Client.POST("/api/mcp/rpc",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"jsonrpc": "2.0",
			"method":  "tools/call",
			"id":      1,
			"params": map[string]any{
				"name":      toolName,
				"arguments": args,
			},
		}),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "unexpected HTTP status for %s", toolName)

	var body map[string]any
	s.Require().NoError(json.Unmarshal(resp.Body, &body))
	s.Require().Nil(body["error"], "unexpected RPC error from %s: %v", toolName, body["error"])

	result := body["result"].(map[string]any)
	content := result["content"].([]any)
	s.Require().NotEmpty(content, "empty content from %s", toolName)

	text := content[0].(map[string]any)["text"].(string)
	var parsed map[string]any
	s.Require().NoError(json.Unmarshal([]byte(text), &parsed), "non-JSON text from %s: %s", toolName, text)
	return parsed
}

// callToolArray is like callTool but expects the JSON text to be an array.
func (s *MCPNewToolsTestSuite) callToolArray(toolName string, args map[string]any) []any {
	resp := s.Client.POST("/api/mcp/rpc",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"jsonrpc": "2.0",
			"method":  "tools/call",
			"id":      1,
			"params": map[string]any{
				"name":      toolName,
				"arguments": args,
			},
		}),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "unexpected HTTP status for %s", toolName)

	var body map[string]any
	s.Require().NoError(json.Unmarshal(resp.Body, &body))
	s.Require().Nil(body["error"], "unexpected RPC error from %s: %v", toolName, body["error"])

	result := body["result"].(map[string]any)
	content := result["content"].([]any)
	s.Require().NotEmpty(content, "empty content from %s", toolName)

	text := content[0].(map[string]any)["text"].(string)
	var parsed []any
	s.Require().NoError(json.Unmarshal([]byte(text), &parsed), "expected JSON array from %s: %s", toolName, text)
	return parsed
}

// initSession sends an MCP initialize request with the test project context.
func (s *MCPNewToolsTestSuite) initSession() {
	resp := s.Client.POST("/api/mcp/rpc",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"jsonrpc": "2.0",
			"method":  "initialize",
			"id":      1,
			"params": map[string]any{
				"protocolVersion": "2025-06-18",
				"clientInfo":      map[string]any{"name": "test-client", "version": "1.0.0"},
			},
		}),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode)
}

// =============================================================================
// Test: 8.1 — list_documents
// =============================================================================

func (s *MCPNewToolsTestSuite) TestMCP_ListDocuments() {
	s.initSession()

	result := s.callTool("list_documents", map[string]any{})

	// Response must contain a "documents" key with an array value.
	docs, ok := result["documents"]
	s.True(ok, "expected 'documents' key in list_documents response")

	_, ok = docs.([]any)
	s.True(ok, "expected 'documents' to be an array")

	// "total" key should be present.
	s.Contains(result, "total")
}

// =============================================================================
// Test: 8.2 — list_skills
// =============================================================================

func (s *MCPNewToolsTestSuite) TestMCP_ListSkills() {
	s.initSession()

	// list_skills returns a JSON array directly.
	items := s.callToolArray("list_skills", map[string]any{})

	// An empty array is valid — we just need a proper array response.
	s.NotNil(items, "expected non-nil array from list_skills")
}

// TestMCP_CreateAndGetSkill creates a skill via MCP and retrieves it.
func (s *MCPNewToolsTestSuite) TestMCP_CreateAndGetSkill() {
	s.initSession()

	created := s.callTool("create_skill", map[string]any{
		"name":        "mcp-test-skill",
		"description": "A skill created by MCP e2e test",
		"content":     "## Test Skill\nThis skill does nothing.",
	})

	s.Contains(created, "id", "create_skill response must contain 'id'")
	s.Equal("mcp-test-skill", created["name"])

	skillID := created["id"].(string)
	s.NotEmpty(skillID)

	// Retrieve the skill by ID.
	got := s.callTool("get_skill", map[string]any{"skill_id": skillID})
	s.Equal(skillID, got["id"])
	s.Equal("mcp-test-skill", got["name"])
}

// =============================================================================
// Test: 8.3 — get_embedding_status (embedding control not wired in test env)
// =============================================================================

func (s *MCPNewToolsTestSuite) TestMCP_GetEmbeddingStatus_NotConfigured() {
	s.initSession()

	// In the test environment, embedding control is not wired up.
	// The tool should return a JSON-RPC-level error (not an HTTP error).
	resp := s.Client.POST("/api/mcp/rpc",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"jsonrpc": "2.0",
			"method":  "tools/call",
			"id":      1,
			"params": map[string]any{
				"name":      "get_embedding_status",
				"arguments": map[string]any{},
			},
		}),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	s.Require().NoError(json.Unmarshal(resp.Body, &body))

	// Either an RPC-level error OR a tool content error is acceptable.
	// The tool returns fmt.Errorf which translates to an RPC error.
	rpcErr, hasRPCErr := body["error"]
	if hasRPCErr {
		s.NotNil(rpcErr, "expected non-nil error")
	} else {
		// Tool returned content with an error message
		result := body["result"].(map[string]any)
		s.Contains(result, "content")
	}
}

// =============================================================================
// Test: 8.4 — list_adk_sessions
// =============================================================================

func (s *MCPNewToolsTestSuite) TestMCP_ListADKSessions() {
	s.initSession()

	// In the test environment the agent tool handler may not be configured.
	// Accept either a proper array result or an "agent tools not available" error.
	resp := s.Client.POST("/api/mcp/rpc",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"jsonrpc": "2.0",
			"method":  "tools/call",
			"id":      1,
			"params": map[string]any{
				"name":      "list_adk_sessions",
				"arguments": map[string]any{},
			},
		}),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	s.Require().NoError(json.Unmarshal(resp.Body, &body))

	if body["error"] != nil {
		// "agent tools not available" is acceptable in the test environment.
		s.T().Logf("list_adk_sessions returned error (agent handler not wired in test env): %v", body["error"])
		return
	}

	result := body["result"].(map[string]any)
	content := result["content"].([]any)
	s.Require().NotEmpty(content, "empty content from list_adk_sessions")

	text := content[0].(map[string]any)["text"].(string)
	var parsed map[string]any
	s.Require().NoError(json.Unmarshal([]byte(text), &parsed), "non-JSON from list_adk_sessions: %s", text)

	// Response should contain "items" array and pagination fields.
	s.Contains(parsed, "items", "expected 'items' key in list_adk_sessions response")
	s.Contains(parsed, "total_count")
}

// =============================================================================
// Test: 8.5 — list_traces (skip when Tempo not configured)
// =============================================================================

func (s *MCPNewToolsTestSuite) TestMCP_ListTraces_SkipWhenTempoNotConfigured() {
	s.initSession()

	// In the test environment, Tempo is not configured.
	// The tool should return either an error or an empty list gracefully.
	resp := s.Client.POST("/api/mcp/rpc",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"jsonrpc": "2.0",
			"method":  "tools/call",
			"id":      1,
			"params": map[string]any{
				"name": "list_traces",
				"arguments": map[string]any{
					"since": "30m",
				},
			},
		}),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	s.Require().NoError(json.Unmarshal(resp.Body, &body))

	// Either an RPC error (Tempo not configured) or a valid result is acceptable.
	if body["error"] != nil {
		s.T().Logf("list_traces returned error (Tempo not configured): %v", body["error"])
		return
	}

	result, ok := body["result"].(map[string]any)
	s.True(ok, "expected result field")
	s.Contains(result, "content")
}

// =============================================================================
// Test: 8.6 — query_knowledge
// =============================================================================

func (s *MCPNewToolsTestSuite) TestMCP_QueryKnowledge() {
	s.initSession()

	// query_knowledge makes an HTTP call to the local server's /api/projects/:id/query
	// endpoint. In the test environment there is no LLM configured so it may return
	// an error or an empty answer — either is acceptable, but the call must not panic
	// and must return a valid HTTP 200 JSON-RPC response.
	resp := s.Client.POST("/api/mcp/rpc",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"jsonrpc": "2.0",
			"method":  "tools/call",
			"id":      1,
			"params": map[string]any{
				"name": "query_knowledge",
				"arguments": map[string]any{
					"question": "What is this project about?",
				},
			},
		}),
	)
	// Must be HTTP 200 (JSON-RPC errors are returned as 200).
	s.Require().Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	s.Require().NoError(json.Unmarshal(resp.Body, &body))

	// Either an RPC error or a result with content is acceptable.
	if body["error"] != nil {
		s.T().Logf("query_knowledge returned error (no LLM in test env): %v", body["error"])
		return
	}

	result, ok := body["result"].(map[string]any)
	s.True(ok, "expected result field")
	s.Contains(result, "content")
}

// =============================================================================
// Test: upload_document via MCP
// =============================================================================

func (s *MCPNewToolsTestSuite) TestMCP_UploadDocument() {
	s.initSession()

	content := "# Test Document\nThis document was uploaded via MCP tool."
	contentB64 := base64.StdEncoding.EncodeToString([]byte(content))

	created := s.callTool("upload_document", map[string]any{
		"filename":       "mcp-test.md",
		"content_base64": contentB64,
		"mime_type":      "text/markdown",
	})

	// Response shape: {"document": {"id": ..., ...}, "isDuplicate": false}
	docObj, ok := created["document"].(map[string]any)
	s.True(ok, "upload_document response must contain 'document' object")

	docID, ok := docObj["id"].(string)
	s.True(ok, "document object must contain string 'id'")
	s.NotEmpty(docID)

	// Retrieve it.
	got := s.callTool("get_document", map[string]any{"document_id": docID})
	s.Equal(docID, got["id"])
}
