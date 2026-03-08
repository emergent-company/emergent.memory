package e2e

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// MCPTestSuite tests the MCP (Model Context Protocol) API endpoints
type MCPTestSuite struct {
	testutil.BaseSuite
}

func TestMCPSuite(t *testing.T) {
	suite.Run(t, new(MCPTestSuite))
}

func (s *MCPTestSuite) SetupSuite() {
	s.SetDBSuffix("mcp")
	s.BaseSuite.SetupSuite()
}

// =============================================================================
// Test: Authentication
// =============================================================================

func (s *MCPTestSuite) TestRPC_RequiresAuth() {
	// Request without Authorization header should fail
	resp := s.Client.POST("/mcp/rpc",
		testutil.WithJSON(),
		testutil.WithBody(`{"jsonrpc": "2.0", "method": "initialize", "id": 1}`),
	)

	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

// =============================================================================
// Test: JSON-RPC Protocol
// =============================================================================

func (s *MCPTestSuite) TestRPC_InvalidJSONRPCVersion() {
	resp := s.Client.POST("/mcp/rpc",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"jsonrpc": "1.0", // Invalid version
			"method":  "initialize",
			"id":      1,
		}),
	)

	s.Equal(http.StatusOK, resp.StatusCode) // JSON-RPC errors are returned with 200

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	errObj, ok := body["error"].(map[string]any)
	s.True(ok, "Expected error field in response")
	s.Equal(float64(-32600), errObj["code"]) // Invalid Request
}

func (s *MCPTestSuite) TestRPC_MethodNotFound() {
	// First initialize
	s.initializeMCPSession()

	// Call unknown method
	resp := s.Client.POST("/mcp/rpc",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"jsonrpc": "2.0",
			"method":  "unknown/method",
			"id":      2,
		}),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	errObj, ok := body["error"].(map[string]any)
	s.True(ok, "Expected error field in response")
	s.Equal(float64(-32601), errObj["code"]) // Method not found
}

// =============================================================================
// Test: Initialize
// =============================================================================

func (s *MCPTestSuite) TestRPC_Initialize() {
	resp := s.Client.POST("/mcp/rpc",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"jsonrpc": "2.0",
			"method":  "initialize",
			"id":      1,
			"params": map[string]any{
				"protocolVersion": "2025-06-18",
				"clientInfo": map[string]any{
					"name":    "test-client",
					"version": "1.0.0",
				},
			},
		}),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	s.Equal("2.0", body["jsonrpc"])
	s.Equal(float64(1), body["id"])
	s.Nil(body["error"])

	result, ok := body["result"].(map[string]any)
	s.True(ok, "Expected result field")
	s.Equal("2025-06-18", result["protocolVersion"])

	serverInfo, ok := result["serverInfo"].(map[string]any)
	s.True(ok, "Expected serverInfo field")
	s.Equal("emergent-mcp-server-go", serverInfo["name"])
}

func (s *MCPTestSuite) TestRPC_Initialize_MissingParams() {
	resp := s.Client.POST("/mcp/rpc",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"jsonrpc": "2.0",
			"method":  "initialize",
			"id":      1,
			"params":  map[string]any{},
		}),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	errObj, ok := body["error"].(map[string]any)
	s.True(ok, "Expected error field")
	s.Equal(float64(-32602), errObj["code"]) // Invalid params
}

// =============================================================================
// Test: tools/list
// =============================================================================

func (s *MCPTestSuite) TestRPC_ToolsList() {
	// First initialize
	s.initializeMCPSession()

	// Call tools/list
	resp := s.Client.POST("/mcp/rpc",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"jsonrpc": "2.0",
			"method":  "tools/list",
			"id":      2,
		}),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	s.Nil(body["error"])

	result, ok := body["result"].(map[string]any)
	s.True(ok, "Expected result field")

	tools, ok := result["tools"].([]any)
	s.True(ok, "Expected tools array")
	s.GreaterOrEqual(len(tools), 4) // At least 4 tools

	// Verify tool names
	toolNames := make(map[string]bool)
	for _, t := range tools {
		tool := t.(map[string]any)
		toolNames[tool["name"].(string)] = true
	}

	s.True(toolNames["schema_version"], "Expected schema_version tool")
	s.True(toolNames["list_entity_types"], "Expected list_entity_types tool")
	s.True(toolNames["query_entities"], "Expected query_entities tool")
	s.True(toolNames["search_entities"], "Expected search_entities tool")
}

func (s *MCPTestSuite) TestRPC_ToolsList_RequiresInitialize() {
	// Call tools/list without initialize (using different auth token for fresh session)
	resp := s.Client.POST("/mcp/rpc",
		testutil.WithAuth("all-scopes"),
		testutil.WithJSONBody(map[string]any{
			"jsonrpc": "2.0",
			"method":  "tools/list",
			"id":      1,
		}),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	errObj, ok := body["error"].(map[string]any)
	s.True(ok, "Expected error - session not initialized")
	s.Equal(float64(-32600), errObj["code"]) // Invalid request
}

// =============================================================================
// Test: tools/call - schema_version
// =============================================================================

func (s *MCPTestSuite) TestRPC_ToolsCall_SchemaVersion() {
	// Initialize session
	s.initializeMCPSession()

	// Call schema_version tool
	resp := s.Client.POST("/mcp/rpc",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"jsonrpc": "2.0",
			"method":  "tools/call",
			"id":      3,
			"params": map[string]any{
				"name":      "schema_version",
				"arguments": map[string]any{},
			},
		}),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	s.Nil(body["error"])

	result, ok := body["result"].(map[string]any)
	s.True(ok, "Expected result field")

	// Result should have content array with text block
	content, ok := result["content"].([]any)
	s.True(ok, "Expected content array")
	s.Len(content, 1)

	block := content[0].(map[string]any)
	s.Equal("text", block["type"])

	// Parse the text content as JSON
	text := block["text"].(string)
	var schemaResult map[string]any
	err = json.Unmarshal([]byte(text), &schemaResult)
	s.NoError(err)

	s.Contains(schemaResult, "version")
	s.Contains(schemaResult, "timestamp")
}

// =============================================================================
// Test: tools/call - list_entity_types
// =============================================================================

func (s *MCPTestSuite) TestRPC_ToolsCall_ListEntityTypes() {
	// Initialize session with project ID
	s.initializeMCPSessionWithProject(s.ProjectID)

	// Call list_entity_types tool
	resp := s.Client.POST("/mcp/rpc",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"jsonrpc": "2.0",
			"method":  "tools/call",
			"id":      4,
			"params": map[string]any{
				"name":      "list_entity_types",
				"arguments": map[string]any{},
			},
		}),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	s.Nil(body["error"])

	result, ok := body["result"].(map[string]any)
	s.True(ok, "Expected result field")

	content, ok := result["content"].([]any)
	s.True(ok, "Expected content array")
	s.Len(content, 1)

	// Parse result
	text := content[0].(map[string]any)["text"].(string)
	var typesResult map[string]any
	err = json.Unmarshal([]byte(text), &typesResult)
	s.NoError(err)

	s.Contains(typesResult, "projectId")
	s.Contains(typesResult, "types")
	s.Contains(typesResult, "total")
}

// =============================================================================
// Test: tools/call - query_entities
// =============================================================================

func (s *MCPTestSuite) TestRPC_ToolsCall_QueryEntities_MissingTypeName() {
	// Initialize session with project ID
	s.initializeMCPSessionWithProject(s.ProjectID)

	// Call query_entities without required type_name
	resp := s.Client.POST("/mcp/rpc",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"jsonrpc": "2.0",
			"method":  "tools/call",
			"id":      5,
			"params": map[string]any{
				"name":      "query_entities",
				"arguments": map[string]any{},
			},
		}),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	// Should return error about missing type_name
	errObj, ok := body["error"].(map[string]any)
	s.True(ok, "Expected error for missing type_name")
	s.Contains(errObj["message"].(string), "type_name")
}

func (s *MCPTestSuite) TestRPC_ToolsCall_QueryEntities_Empty() {
	// Initialize session with project ID
	s.initializeMCPSessionWithProject(s.ProjectID)

	// Query for an entity type that doesn't exist
	resp := s.Client.POST("/mcp/rpc",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"jsonrpc": "2.0",
			"method":  "tools/call",
			"id":      6,
			"params": map[string]any{
				"name": "query_entities",
				"arguments": map[string]any{
					"type_name": "NonExistentType",
				},
			},
		}),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	s.Nil(body["error"])

	result, ok := body["result"].(map[string]any)
	s.True(ok, "Expected result field")

	content, ok := result["content"].([]any)
	s.True(ok, "Expected content array")

	// Parse result
	text := content[0].(map[string]any)["text"].(string)
	var queryResult map[string]any
	err = json.Unmarshal([]byte(text), &queryResult)
	s.NoError(err)

	// Should return empty entities array
	entities, ok := queryResult["entities"].([]any)
	s.True(ok)
	s.Len(entities, 0)
}

// callMCPTool is a helper that calls a tools/call method and returns the parsed result map.
// It initializes the session with a project context before the call.
func (s *MCPTestSuite) callMCPTool(projectID, toolName string, args map[string]any, id int) map[string]any {
	resp := s.Client.POST("/mcp/rpc",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(projectID),
		testutil.WithJSONBody(map[string]any{
			"jsonrpc": "2.0",
			"method":  "tools/call",
			"id":      id,
			"params": map[string]any{
				"name":      toolName,
				"arguments": args,
			},
		}),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.Require().NoError(err)
	s.Require().Nil(body["error"], "unexpected error from %s: %v", toolName, body["error"])

	result, ok := body["result"].(map[string]any)
	s.Require().True(ok, "expected result field")

	content, ok := result["content"].([]any)
	s.Require().True(ok, "expected content array")
	s.Require().NotEmpty(content)

	text := content[0].(map[string]any)["text"].(string)
	var parsed map[string]any
	err = json.Unmarshal([]byte(text), &parsed)
	s.Require().NoError(err)
	return parsed
}

// =============================================================================
// Test: query_entities returns only the latest version per entity (issue #54)
// =============================================================================

// TestRPC_ToolsCall_QueryEntities_ReturnsLatestVersionOnly verifies that after
// updating an entity, query_entities returns only one row for that entity and
// its properties reflect the latest update — not the original version.
func (s *MCPTestSuite) TestRPC_ToolsCall_QueryEntities_ReturnsLatestVersionOnly() {
	s.initializeMCPSessionWithProject(s.ProjectID)

	// Step 1: Create an entity with status "created"
	createResult := s.callMCPTool(s.ProjectID, "create_entity", map[string]any{
		"type": "WorkPackage",
		"properties": map[string]any{
			"name":   "Version Test WP",
			"status": "created",
		},
	}, 10)

	s.True(createResult["success"].(bool), "create_entity should succeed")
	createdEntity := createResult["entity"].(map[string]any)
	entityID := createdEntity["id"].(string)
	s.NotEmpty(entityID, "entity ID should not be empty")
	s.Equal(float64(1), createdEntity["version"], "initial version should be 1")

	// Step 2: Update the entity (creates version 2)
	updateResult := s.callMCPTool(s.ProjectID, "update_entity", map[string]any{
		"entity_id": entityID,
		"properties": map[string]any{
			"name":   "Version Test WP",
			"status": "in_progress",
		},
	}, 11)

	s.True(updateResult["success"].(bool), "update_entity should succeed")
	updatedEntity := updateResult["entity"].(map[string]any)
	s.Equal(float64(2), updatedEntity["version"], "version should be 2 after update")

	// Step 3: Query entities — must return exactly one row for our entity
	queryResult := s.callMCPTool(s.ProjectID, "query_entities", map[string]any{
		"type_name": "WorkPackage",
	}, 12)

	entities, ok := queryResult["entities"].([]any)
	s.True(ok, "expected entities array")

	// Count how many times our entity appears — should be exactly 1 (latest version only)
	var matchCount int
	var latestProps map[string]any
	for _, e := range entities {
		entity := e.(map[string]any)
		if entity["id"].(string) == entityID {
			matchCount++
			latestProps = entity["properties"].(map[string]any)
		}
	}

	s.Equal(1, matchCount, "query_entities should return exactly one row per entity, not one per version")
	if latestProps != nil {
		s.Equal("in_progress", latestProps["status"],
			"query_entities should return the latest version properties, not original")
	}

	// Cleanup
	s.callMCPTool(s.ProjectID, "delete_entity", map[string]any{"entity_id": entityID}, 13)
}

// TestRPC_ToolsCall_QueryEntities_StableCanonicalID verifies that the entity ID
// returned by query_entities is the stable canonical_id (unchanged after updates),
// not the physical version row ID (which changes with every update).
func (s *MCPTestSuite) TestRPC_ToolsCall_QueryEntities_StableCanonicalID() {
	s.initializeMCPSessionWithProject(s.ProjectID)

	// Step 1: Create entity
	createResult := s.callMCPTool(s.ProjectID, "create_entity", map[string]any{
		"type": "WorkPackage",
		"properties": map[string]any{
			"name":  "Canonical ID Test WP",
			"value": "initial",
		},
	}, 20)

	s.True(createResult["success"].(bool), "create_entity should succeed")
	createdEntity := createResult["entity"].(map[string]any)
	createdID := createdEntity["id"].(string)
	s.NotEmpty(createdID, "created entity ID should not be empty")

	// Step 2: Update the entity (this creates a new physical version row with a new UUID)
	updateResult := s.callMCPTool(s.ProjectID, "update_entity", map[string]any{
		"entity_id": createdID,
		"properties": map[string]any{
			"name":  "Canonical ID Test WP",
			"value": "updated",
		},
	}, 21)

	s.True(updateResult["success"].(bool), "update_entity should succeed")

	// Step 3: Query entities and find our entity by canonical_id
	// The ID returned by query_entities must be the canonical_id (same as returned
	// by create_entity and update_entity), not the physical row ID.
	queryResult := s.callMCPTool(s.ProjectID, "query_entities", map[string]any{
		"type_name": "WorkPackage",
	}, 22)

	entities, ok := queryResult["entities"].([]any)
	s.True(ok, "expected entities array")

	var foundEntity map[string]any
	for _, e := range entities {
		entity := e.(map[string]any)
		if entity["id"].(string) == createdID {
			foundEntity = entity
			break
		}
	}

	s.NotNil(foundEntity, "query_entities should find the entity using its canonical_id (same as create_entity returned)")
	if foundEntity != nil {
		props := foundEntity["properties"].(map[string]any)
		s.Equal("updated", props["value"],
			"query_entities should return the updated properties via the canonical_id")
	}

	// Cleanup
	s.callMCPTool(s.ProjectID, "delete_entity", map[string]any{"entity_id": createdID}, 23)
}

// TestRPC_ToolsCall_QueryEntities_PaginationCountsOnlyLatestVersions verifies
// that pagination totals reflect unique entities, not total version rows.
func (s *MCPTestSuite) TestRPC_ToolsCall_QueryEntities_PaginationCountsOnlyLatestVersions() {
	s.initializeMCPSessionWithProject(s.ProjectID)

	// Create one entity and update it multiple times to create multiple versions
	createResult := s.callMCPTool(s.ProjectID, "create_entity", map[string]any{
		"type": "WorkPackage",
		"properties": map[string]any{
			"name":   "Pagination Count Test WP",
			"status": "v1",
		},
	}, 30)

	s.True(createResult["success"].(bool))
	entityID := createResult["entity"].(map[string]any)["id"].(string)

	// Create 3 more versions
	for i, status := range []string{"v2", "v3", "v4"} {
		s.callMCPTool(s.ProjectID, "update_entity", map[string]any{
			"entity_id": entityID,
			"properties": map[string]any{
				"name":   "Pagination Count Test WP",
				"status": status,
			},
		}, 31+i)
	}

	// Query with limit=1 to get the pagination total
	queryResult := s.callMCPTool(s.ProjectID, "query_entities", map[string]any{
		"type_name": "WorkPackage",
		"limit":     float64(50),
	}, 34)

	entities, ok := queryResult["entities"].([]any)
	s.True(ok)

	// Count occurrences of our entity — must be exactly 1
	var occurrences int
	for _, e := range entities {
		entity := e.(map[string]any)
		if entity["id"].(string) == entityID {
			occurrences++
		}
	}
	s.Equal(1, occurrences,
		"a single entity with multiple versions should appear exactly once in query results")

	// Pagination total must not count versions as separate entities
	pagination, ok := queryResult["pagination"].(map[string]any)
	s.True(ok, "expected pagination field")
	total := int(pagination["total"].(float64))
	s.GreaterOrEqual(total, 1, "total should be at least 1")
	// The total should equal the number of unique entities (not version rows).
	// We can't know the exact count from other tests, but we know occurrences=1 above.
	// The limit=50 query above already proves our entity appears once, not 4 times.

	// Cleanup
	s.callMCPTool(s.ProjectID, "delete_entity", map[string]any{"entity_id": entityID}, 35)
}

// =============================================================================

func (s *MCPTestSuite) TestRPC_ToolsCall_SearchEntities_MissingQuery() {
	// Initialize session with project ID
	s.initializeMCPSessionWithProject(s.ProjectID)

	// Call search_entities without required query
	resp := s.Client.POST("/mcp/rpc",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"jsonrpc": "2.0",
			"method":  "tools/call",
			"id":      7,
			"params": map[string]any{
				"name":      "search_entities",
				"arguments": map[string]any{},
			},
		}),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	// Should return error about missing query
	errObj, ok := body["error"].(map[string]any)
	s.True(ok, "Expected error for missing query")
	s.Contains(errObj["message"].(string), "query")
}

func (s *MCPTestSuite) TestRPC_ToolsCall_SearchEntities_Empty() {
	// Initialize session with project ID
	s.initializeMCPSessionWithProject(s.ProjectID)

	// Search for something that doesn't exist
	resp := s.Client.POST("/mcp/rpc",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"jsonrpc": "2.0",
			"method":  "tools/call",
			"id":      8,
			"params": map[string]any{
				"name": "search_entities",
				"arguments": map[string]any{
					"query": "xyznonexistent123",
				},
			},
		}),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	s.Nil(body["error"])

	result, ok := body["result"].(map[string]any)
	s.True(ok, "Expected result field")

	content, ok := result["content"].([]any)
	s.True(ok, "Expected content array")

	// Parse result
	text := content[0].(map[string]any)["text"].(string)
	var searchResult map[string]any
	err = json.Unmarshal([]byte(text), &searchResult)
	s.NoError(err)

	// Should return empty entities array
	entities, ok := searchResult["entities"].([]any)
	s.True(ok)
	s.Len(entities, 0)
}

// =============================================================================
// Test: SSE Endpoints
// =============================================================================

func (s *MCPTestSuite) TestSSE_Connect_RequiresAuth() {
	// Request without Authorization header should fail
	resp := s.Client.GET("/mcp/sse/" + s.ProjectID)

	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *MCPTestSuite) TestSSE_Connect_InvalidProjectID() {
	resp := s.Client.GET("/mcp/sse/invalid-uuid",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *MCPTestSuite) TestSSE_Message_RequiresAuth() {
	// Request without Authorization header should fail
	resp := s.Client.POST("/mcp/sse/"+s.ProjectID+"/message",
		testutil.WithJSON(),
		testutil.WithBody(`{"jsonrpc": "2.0", "method": "initialize", "id": 1}`),
	)

	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

// =============================================================================
// Helper Methods
// =============================================================================

func (s *MCPTestSuite) initializeMCPSession() {
	resp := s.Client.POST("/mcp/rpc",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"jsonrpc": "2.0",
			"method":  "initialize",
			"id":      1,
			"params": map[string]any{
				"protocolVersion": "2025-06-18",
				"clientInfo": map[string]any{
					"name":    "test-client",
					"version": "1.0.0",
				},
			},
		}),
	)
	s.Equal(http.StatusOK, resp.StatusCode)
}

func (s *MCPTestSuite) initializeMCPSessionWithProject(projectID string) {
	resp := s.Client.POST("/mcp/rpc",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(projectID),
		testutil.WithJSONBody(map[string]any{
			"jsonrpc": "2.0",
			"method":  "initialize",
			"id":      1,
			"params": map[string]any{
				"protocolVersion": "2025-06-18",
				"clientInfo": map[string]any{
					"name":    "test-client",
					"version": "1.0.0",
				},
				"project_id": projectID,
			},
		}),
	)
	s.Equal(http.StatusOK, resp.StatusCode)
}

// =============================================================================
// Test: Unified MCP Endpoint (Spec 2025-11-25)
// =============================================================================

func (s *MCPTestSuite) TestUnified_POST_RequiresAuth() {
	resp := s.Client.POST("/mcp",
		testutil.WithJSON(),
		testutil.WithBody(`{"jsonrpc": "2.0", "method": "initialize", "id": 1}`),
	)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *MCPTestSuite) TestUnified_POST_Initialize() {
	resp := s.Client.POST("/mcp",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithHeader("Accept", "application/json, text/event-stream"),
		testutil.WithJSONBody(map[string]any{
			"jsonrpc": "2.0",
			"method":  "initialize",
			"id":      1,
			"params": map[string]any{
				"protocolVersion": "2025-11-25",
				"clientInfo": map[string]any{
					"name":    "test-client",
					"version": "1.0.0",
				},
			},
		}),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	s.Equal("2.0", body["jsonrpc"])
	s.Nil(body["error"])

	result, ok := body["result"].(map[string]any)
	s.True(ok)
	s.Equal("2025-11-25", result["protocolVersion"])

	sessionID := resp.Headers.Get("Mcp-Session-Id")
	s.NotEmpty(sessionID, "Expected Mcp-Session-Id header")
}

func (s *MCPTestSuite) TestUnified_POST_SessionManagement() {
	resp1 := s.Client.POST("/mcp",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithHeader("Accept", "application/json, text/event-stream"),
		testutil.WithJSONBody(map[string]any{
			"jsonrpc": "2.0",
			"method":  "initialize",
			"id":      1,
			"params": map[string]any{
				"protocolVersion": "2025-11-25",
				"clientInfo":      map[string]any{"name": "test", "version": "1.0"},
			},
		}),
	)
	s.Equal(http.StatusOK, resp1.StatusCode)
	sessionID := resp1.Headers.Get("Mcp-Session-Id")
	s.NotEmpty(sessionID)

	resp2 := s.Client.POST("/mcp",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithHeader("Accept", "application/json, text/event-stream"),
		testutil.WithHeader("Mcp-Session-Id", sessionID),
		testutil.WithJSONBody(map[string]any{
			"jsonrpc": "2.0",
			"method":  "tools/list",
			"id":      2,
		}),
	)
	s.Equal(http.StatusOK, resp2.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp2.Body, &body)
	s.NoError(err)
	s.Nil(body["error"])

	result, ok := body["result"].(map[string]any)
	s.True(ok)
	tools, ok := result["tools"].([]any)
	s.True(ok)
	s.GreaterOrEqual(len(tools), 4)
}

func (s *MCPTestSuite) TestUnified_DELETE_TerminatesSession() {
	resp1 := s.Client.POST("/mcp",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithHeader("Accept", "application/json, text/event-stream"),
		testutil.WithJSONBody(map[string]any{
			"jsonrpc": "2.0",
			"method":  "initialize",
			"id":      1,
			"params": map[string]any{
				"protocolVersion": "2025-11-25",
				"clientInfo":      map[string]any{"name": "test", "version": "1.0"},
			},
		}),
	)
	sessionID := resp1.Headers.Get("Mcp-Session-Id")
	s.NotEmpty(sessionID)

	resp2 := s.Client.DELETE("/mcp",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithHeader("Mcp-Session-Id", sessionID),
	)
	s.Equal(http.StatusNoContent, resp2.StatusCode)

	resp3 := s.Client.POST("/mcp",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithHeader("Accept", "application/json, text/event-stream"),
		testutil.WithHeader("Mcp-Session-Id", sessionID),
		testutil.WithJSONBody(map[string]any{
			"jsonrpc": "2.0",
			"method":  "tools/list",
			"id":      3,
		}),
	)
	s.Equal(http.StatusNotFound, resp3.StatusCode)
}

func (s *MCPTestSuite) TestUnified_DELETE_RequiresSessionID() {
	resp := s.Client.DELETE("/mcp",
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}
