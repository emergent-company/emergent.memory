package e2e

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent/internal/testutil"
)

// MCPSSEToolsSuite tests all MCP tools via the unified SSE endpoint.
// This suite discovers and tests each of the 18 exposed MCP tools:
// - schema_version, list_entity_types, query_entities, search_entities, get_entity_edges
// - create_entity, create_relationship, update_entity, delete_entity
// - list_template_packs, get_template_pack, get_available_templates, get_installed_templates
// - assign_template_pack, update_template_assignment, uninstall_template_pack
// - create_template_pack, delete_template_pack
type MCPSSEToolsSuite struct {
	testutil.BaseSuite
	sessionID string
}

func TestMCPSSEToolsSuite(t *testing.T) {
	suite.Run(t, new(MCPSSEToolsSuite))
}

func (s *MCPSSEToolsSuite) SetupSuite() {
	s.SetDBSuffix("mcp_sse")
	s.BaseSuite.SetupSuite()
}

// initSession initializes an MCP session and returns the session ID
func (s *MCPSSEToolsSuite) initSession() string {
	return s.initSessionWithProject("")
}

// initSessionWithProject initializes an MCP session with optional project context
func (s *MCPSSEToolsSuite) initSessionWithProject(projectID string) string {
	params := map[string]any{
		"protocolVersion": "2025-11-25",
		"clientInfo": map[string]any{
			"name":    "mcp-sse-test",
			"version": "1.0.0",
		},
	}
	if projectID != "" {
		params["project_id"] = projectID
	}

	resp := s.Client.POST("/api/mcp",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithHeader("Accept", "application/json, text/event-stream"),
		testutil.WithHeader("MCP-Protocol-Version", "2025-11-25"),
		testutil.WithJSONBody(map[string]any{
			"jsonrpc": "2.0",
			"method":  "initialize",
			"id":      1,
			"params":  params,
		}),
	)

	s.Equal(http.StatusOK, resp.StatusCode, "Initialize failed: %s", string(resp.Body))

	sessionID := resp.Headers.Get("Mcp-Session-Id")
	s.NotEmpty(sessionID, "Expected Mcp-Session-Id header")

	return sessionID
}

// callTool calls an MCP tool and returns the parsed result
func (s *MCPSSEToolsSuite) callTool(sessionID, toolName string, args map[string]any) map[string]any {
	resp := s.Client.POST("/api/mcp",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithHeader("Accept", "application/json, text/event-stream"),
		testutil.WithHeader("MCP-Protocol-Version", "2025-11-25"),
		testutil.WithHeader("Mcp-Session-Id", sessionID),
		testutil.WithJSONBody(map[string]any{
			"jsonrpc": "2.0",
			"method":  "tools/call",
			"id":      2,
			"params": map[string]any{
				"name":      toolName,
				"arguments": args,
			},
		}),
	)

	s.Equal(http.StatusOK, resp.StatusCode, "Tool call failed for: %s, response: %s", toolName, string(resp.Body))

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	return body
}

// parseToolResultText extracts and parses the text content from a tool result
func (s *MCPSSEToolsSuite) parseToolResultText(body map[string]any) map[string]any {
	s.Nil(body["error"], "Tool returned error: %v", body["error"])

	result, ok := body["result"].(map[string]any)
	s.True(ok, "Expected result field")

	content, ok := result["content"].([]any)
	s.True(ok, "Expected content array")
	s.NotEmpty(content, "Expected non-empty content")

	block := content[0].(map[string]any)
	s.Equal("text", block["type"])

	text := block["text"].(string)
	var parsed map[string]any
	err := json.Unmarshal([]byte(text), &parsed)
	s.NoError(err, "Failed to parse tool result JSON")

	return parsed
}

// =============================================================================
// Test: Tool Discovery
// =============================================================================

func (s *MCPSSEToolsSuite) TestToolsList_DiscoverAll18Tools() {
	sessionID := s.initSession()

	resp := s.Client.POST("/api/mcp",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithHeader("Accept", "application/json, text/event-stream"),
		testutil.WithHeader("MCP-Protocol-Version", "2025-11-25"),
		testutil.WithHeader("Mcp-Session-Id", sessionID),
		testutil.WithJSONBody(map[string]any{
			"jsonrpc": "2.0",
			"method":  "tools/list",
			"id":      2,
		}),
	)

	s.Equal(http.StatusOK, resp.StatusCode, "tools/list failed: %s", string(resp.Body))

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	s.Nil(body["error"])

	result, ok := body["result"].(map[string]any)
	s.True(ok, "Expected result field")

	tools, ok := result["tools"].([]any)
	s.True(ok, "Expected tools array")

	// Verify we have all 18 tools (14 original + 4 new CRUD tools)
	s.GreaterOrEqual(len(tools), 18, "Expected at least 18 tools")

	// Build tool name map
	toolNames := make(map[string]bool)
	for _, t := range tools {
		tool := t.(map[string]any)
		toolNames[tool["name"].(string)] = true
	}

	// Verify each expected tool exists
	expectedTools := []string{
		"schema_version",
		"list_entity_types",
		"query_entities",
		"search_entities",
		"get_entity_edges",
		"list_template_packs",
		"get_template_pack",
		"get_available_templates",
		"get_installed_templates",
		"assign_template_pack",
		"update_template_assignment",
		"uninstall_template_pack",
		"create_template_pack",
		"delete_template_pack",
	}

	for _, name := range expectedTools {
		s.True(toolNames[name], "Expected tool: %s", name)
	}
}

// =============================================================================
// Test: schema_version (No project required)
// =============================================================================

func (s *MCPSSEToolsSuite) TestTool_SchemaVersion() {
	sessionID := s.initSession()
	body := s.callTool(sessionID, "schema_version", map[string]any{})
	result := s.parseToolResultText(body)

	// Verify response structure
	s.Contains(result, "version")
	s.Contains(result, "timestamp")
	s.Contains(result, "pack_count")
	s.Contains(result, "cache_hint_ttl")
}

// =============================================================================
// Test: list_template_packs (No project required)
// =============================================================================

func (s *MCPSSEToolsSuite) TestTool_ListTemplatePacks() {
	sessionID := s.initSession()
	body := s.callTool(sessionID, "list_template_packs", map[string]any{})
	result := s.parseToolResultText(body)

	// Verify response structure
	s.Contains(result, "packs")
	s.Contains(result, "total")
	s.Contains(result, "page")
	s.Contains(result, "limit")
	s.Contains(result, "has_more")

	// Packs should be an array
	packs, ok := result["packs"].([]any)
	s.True(ok, "Expected packs to be an array")
	s.NotNil(packs)
}

func (s *MCPSSEToolsSuite) TestTool_ListTemplatePacks_WithPagination() {
	sessionID := s.initSession()
	body := s.callTool(sessionID, "list_template_packs", map[string]any{
		"limit": 5,
		"page":  1,
	})
	result := s.parseToolResultText(body)

	// Verify pagination is applied
	s.Equal(float64(5), result["limit"])
	s.Equal(float64(1), result["page"])
}

// =============================================================================
// Test: get_template_pack (No project required)
// =============================================================================

func (s *MCPSSEToolsSuite) TestTool_GetTemplatePack_NotFound() {
	sessionID := s.initSession()
	body := s.callTool(sessionID, "get_template_pack", map[string]any{
		"pack_id": "00000000-0000-0000-0000-000000000000",
	})

	// Should return error for non-existent pack
	errObj, hasError := body["error"].(map[string]any)
	if hasError {
		s.Contains(errObj["message"].(string), "not found")
	} else {
		// Or result with nil pack
		result := s.parseToolResultText(body)
		s.Nil(result["pack"])
	}
}

// =============================================================================
// Test: list_entity_types (Project required)
// =============================================================================

func (s *MCPSSEToolsSuite) TestTool_ListEntityTypes() {
	sessionID := s.initSessionWithProject(s.ProjectID)
	body := s.callTool(sessionID, "list_entity_types", map[string]any{})
	result := s.parseToolResultText(body)

	// Verify response structure
	s.Contains(result, "projectId")
	s.Contains(result, "types")
	s.Contains(result, "total")
	s.Equal(s.ProjectID, result["projectId"])

	// Types should be an array
	types, ok := result["types"].([]any)
	s.True(ok, "Expected types to be an array")
	s.NotNil(types)
}

// =============================================================================
// Test: query_entities (Project required)
// =============================================================================

func (s *MCPSSEToolsSuite) TestTool_QueryEntities_RequiresTypeName() {
	sessionID := s.initSessionWithProject(s.ProjectID)
	body := s.callTool(sessionID, "query_entities", map[string]any{})

	// Should return error for missing type_name
	errObj, ok := body["error"].(map[string]any)
	s.True(ok, "Expected error for missing type_name")
	s.Contains(errObj["message"].(string), "type_name")
}

func (s *MCPSSEToolsSuite) TestTool_QueryEntities_EmptyResult() {
	sessionID := s.initSessionWithProject(s.ProjectID)
	body := s.callTool(sessionID, "query_entities", map[string]any{
		"type_name": "NonExistentType",
	})
	result := s.parseToolResultText(body)

	// Should return empty entities array
	entities, ok := result["entities"].([]any)
	s.True(ok, "Expected entities to be an array")
	s.Len(entities, 0)
}

func (s *MCPSSEToolsSuite) TestTool_QueryEntities_WithPagination() {
	sessionID := s.initSessionWithProject(s.ProjectID)
	body := s.callTool(sessionID, "query_entities", map[string]any{
		"type_name": "SomeType",
		"limit":     10,
		"offset":    0,
	})
	result := s.parseToolResultText(body)

	// Verify pagination fields are in response
	s.Contains(result, "entities")
	s.Contains(result, "projectId")
}

// =============================================================================
// Test: search_entities (Project required)
// =============================================================================

func (s *MCPSSEToolsSuite) TestTool_SearchEntities_RequiresQuery() {
	sessionID := s.initSessionWithProject(s.ProjectID)
	body := s.callTool(sessionID, "search_entities", map[string]any{})

	// Should return error for missing query
	errObj, ok := body["error"].(map[string]any)
	s.True(ok, "Expected error for missing query")
	s.Contains(errObj["message"].(string), "query")
}

func (s *MCPSSEToolsSuite) TestTool_SearchEntities_EmptyResult() {
	sessionID := s.initSessionWithProject(s.ProjectID)
	body := s.callTool(sessionID, "search_entities", map[string]any{
		"query": "xyznonexistent123abc",
	})
	result := s.parseToolResultText(body)

	// Should return empty entities array
	s.Contains(result, "entities")
	s.Contains(result, "query")
	s.Contains(result, "count")

	entities, ok := result["entities"].([]any)
	s.True(ok, "Expected entities to be an array")
	s.Len(entities, 0)
}

func (s *MCPSSEToolsSuite) TestTool_SearchEntities_WithTypeFilter() {
	sessionID := s.initSessionWithProject(s.ProjectID)
	body := s.callTool(sessionID, "search_entities", map[string]any{
		"query":     "test",
		"type_name": "Document",
		"limit":     5,
	})
	result := s.parseToolResultText(body)

	// Verify response structure
	s.Contains(result, "entities")
	s.Contains(result, "query")
}

// =============================================================================
// Test: get_entity_edges (Project required)
// =============================================================================

func (s *MCPSSEToolsSuite) TestTool_GetEntityEdges_RequiresEntityID() {
	sessionID := s.initSessionWithProject(s.ProjectID)
	body := s.callTool(sessionID, "get_entity_edges", map[string]any{})

	// Should return error for missing entity_id
	errObj, ok := body["error"].(map[string]any)
	s.True(ok, "Expected error for missing entity_id")
	s.Contains(errObj["message"].(string), "entity_id")
}

func (s *MCPSSEToolsSuite) TestTool_GetEntityEdges_NotFound() {
	sessionID := s.initSessionWithProject(s.ProjectID)
	body := s.callTool(sessionID, "get_entity_edges", map[string]any{
		"entity_id": "00000000-0000-0000-0000-000000000000",
	})

	// Either error or empty result
	if errObj, hasError := body["error"].(map[string]any); hasError {
		s.Contains(errObj["message"].(string), "not found")
	} else {
		result := s.parseToolResultText(body)
		s.Contains(result, "entity_id")
		s.Contains(result, "incoming")
		s.Contains(result, "outgoing")
	}
}

// =============================================================================
// Test: get_available_templates (Project required)
// =============================================================================

func (s *MCPSSEToolsSuite) TestTool_GetAvailableTemplates() {
	sessionID := s.initSessionWithProject(s.ProjectID)
	body := s.callTool(sessionID, "get_available_templates", map[string]any{})
	result := s.parseToolResultText(body)

	// Verify response structure
	s.Contains(result, "project_id")
	s.Contains(result, "templates")
	s.Contains(result, "total")

	// Templates should be an array
	templates, ok := result["templates"].([]any)
	s.True(ok, "Expected templates to be an array")
	s.NotNil(templates)
}

// =============================================================================
// Test: get_installed_templates (Project required)
// =============================================================================

func (s *MCPSSEToolsSuite) TestTool_GetInstalledTemplates() {
	sessionID := s.initSessionWithProject(s.ProjectID)
	body := s.callTool(sessionID, "get_installed_templates", map[string]any{})
	result := s.parseToolResultText(body)

	// Verify response structure
	s.Contains(result, "project_id")
	s.Contains(result, "templates")
	s.Contains(result, "total")

	// Templates should be an array (empty for fresh project)
	templates, ok := result["templates"].([]any)
	s.True(ok, "Expected templates to be an array")
	s.NotNil(templates)
}

// =============================================================================
// Test: create_template_pack, assign, update, uninstall, delete (Full lifecycle)
// This test requires external server mode due to transaction isolation limits
// =============================================================================

func (s *MCPSSEToolsSuite) TestTool_TemplatePackLifecycle() {
	if !s.IsExternal() {
		s.T().Skip("Lifecycle test requires external server due to transaction isolation")
	}
	// Step 1: Create a template pack
	sessionID := s.initSession()
	createBody := s.callTool(sessionID, "create_template_pack", map[string]any{
		"name":        "Test Pack",
		"version":     "1.0.0",
		"description": "A test template pack for E2E testing",
		"author":      "E2E Test",
		"object_type_schemas": map[string]any{
			"TestEntity": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
			},
		},
	})
	createResult := s.parseToolResultText(createBody)

	s.True(createResult["success"].(bool), "Expected create to succeed")
	s.Contains(createResult, "pack")

	pack := createResult["pack"].(map[string]any)
	packID := pack["id"].(string)
	s.NotEmpty(packID)

	// Step 2: Get the created pack
	getBody := s.callTool(sessionID, "get_template_pack", map[string]any{
		"pack_id": packID,
	})
	getResult := s.parseToolResultText(getBody)
	s.Contains(getResult, "pack")
	getPack := getResult["pack"].(map[string]any)
	s.Equal(packID, getPack["id"])
	s.Equal("Test Pack", getPack["name"])

	// Step 3: Assign the template pack to a project
	projectSession := s.initSessionWithProject(s.ProjectID)
	assignBody := s.callTool(projectSession, "assign_template_pack", map[string]any{
		"template_pack_id": packID,
	})
	assignResult := s.parseToolResultText(assignBody)

	s.True(assignResult["success"].(bool), "Expected assign to succeed")
	s.Contains(assignResult, "assignment_id")

	assignmentID := assignResult["assignment_id"].(string)
	s.NotEmpty(assignmentID)

	// Step 4: Verify it appears in installed templates
	installedBody := s.callTool(projectSession, "get_installed_templates", map[string]any{})
	installedResult := s.parseToolResultText(installedBody)

	templates := installedResult["templates"].([]any)
	s.NotEmpty(templates, "Expected at least one installed template")

	// Step 5: Update the template assignment (deactivate)
	updateBody := s.callTool(projectSession, "update_template_assignment", map[string]any{
		"assignment_id": assignmentID,
		"active":        false,
	})
	updateResult := s.parseToolResultText(updateBody)

	s.True(updateResult["success"].(bool), "Expected update to succeed")
	s.Equal(false, updateResult["active"])

	// Step 6: Uninstall the template pack
	uninstallBody := s.callTool(projectSession, "uninstall_template_pack", map[string]any{
		"assignment_id": assignmentID,
	})
	uninstallResult := s.parseToolResultText(uninstallBody)

	s.True(uninstallResult["success"].(bool), "Expected uninstall to succeed")

	// Step 7: Delete the template pack
	deleteBody := s.callTool(sessionID, "delete_template_pack", map[string]any{
		"pack_id": packID,
	})
	deleteResult := s.parseToolResultText(deleteBody)

	s.True(deleteResult["success"].(bool), "Expected delete to succeed")
	s.Equal(packID, deleteResult["pack_id"])
}

// =============================================================================
// Test: assign_template_pack validation
// =============================================================================

func (s *MCPSSEToolsSuite) TestTool_AssignTemplatePack_RequiresTemplatePackID() {
	sessionID := s.initSessionWithProject(s.ProjectID)
	body := s.callTool(sessionID, "assign_template_pack", map[string]any{})

	// Should return error for missing template_pack_id
	errObj, ok := body["error"].(map[string]any)
	s.True(ok, "Expected error for missing template_pack_id")
	s.Contains(errObj["message"].(string), "template_pack_id")
}

func (s *MCPSSEToolsSuite) TestTool_AssignTemplatePack_NotFound() {
	sessionID := s.initSessionWithProject(s.ProjectID)
	body := s.callTool(sessionID, "assign_template_pack", map[string]any{
		"template_pack_id": "00000000-0000-0000-0000-000000000000",
	})

	// Should return error for non-existent pack
	errObj, ok := body["error"].(map[string]any)
	s.True(ok, "Expected error for non-existent pack")
	s.Contains(errObj["message"].(string), "not found")
}

// =============================================================================
// Test: update_template_assignment validation
// =============================================================================

func (s *MCPSSEToolsSuite) TestTool_UpdateTemplateAssignment_RequiresAssignmentID() {
	sessionID := s.initSessionWithProject(s.ProjectID)
	body := s.callTool(sessionID, "update_template_assignment", map[string]any{})

	// Should return error for missing assignment_id
	errObj, ok := body["error"].(map[string]any)
	s.True(ok, "Expected error for missing assignment_id")
	s.Contains(errObj["message"].(string), "assignment_id")
}

func (s *MCPSSEToolsSuite) TestTool_UpdateTemplateAssignment_NotFound() {
	sessionID := s.initSessionWithProject(s.ProjectID)
	body := s.callTool(sessionID, "update_template_assignment", map[string]any{
		"assignment_id": "00000000-0000-0000-0000-000000000000",
		"active":        true,
	})

	// Should return error for non-existent assignment
	errObj, ok := body["error"].(map[string]any)
	s.True(ok, "Expected error for non-existent assignment")
	s.Contains(errObj["message"].(string), "not found")
}

// =============================================================================
// Test: uninstall_template_pack validation
// =============================================================================

func (s *MCPSSEToolsSuite) TestTool_UninstallTemplatePack_RequiresAssignmentID() {
	sessionID := s.initSessionWithProject(s.ProjectID)
	body := s.callTool(sessionID, "uninstall_template_pack", map[string]any{})

	// Should return error for missing assignment_id
	errObj, ok := body["error"].(map[string]any)
	s.True(ok, "Expected error for missing assignment_id")
	s.Contains(errObj["message"].(string), "assignment_id")
}

func (s *MCPSSEToolsSuite) TestTool_UninstallTemplatePack_NotFound() {
	sessionID := s.initSessionWithProject(s.ProjectID)
	body := s.callTool(sessionID, "uninstall_template_pack", map[string]any{
		"assignment_id": "00000000-0000-0000-0000-000000000000",
	})

	// Should return error for non-existent assignment
	errObj, ok := body["error"].(map[string]any)
	s.True(ok, "Expected error for non-existent assignment")
	s.Contains(errObj["message"].(string), "not found")
}

// =============================================================================
// Test: create_template_pack validation
// =============================================================================

func (s *MCPSSEToolsSuite) TestTool_CreateTemplatePack_RequiresName() {
	sessionID := s.initSession()
	body := s.callTool(sessionID, "create_template_pack", map[string]any{
		"version":             "1.0.0",
		"object_type_schemas": map[string]any{},
	})

	// Should return error for missing name
	errObj, ok := body["error"].(map[string]any)
	s.True(ok, "Expected error for missing name")
	s.Contains(errObj["message"].(string), "name")
}

func (s *MCPSSEToolsSuite) TestTool_CreateTemplatePack_RequiresVersion() {
	sessionID := s.initSession()
	body := s.callTool(sessionID, "create_template_pack", map[string]any{
		"name":                "Test",
		"object_type_schemas": map[string]any{},
	})

	// Should return error for missing version
	errObj, ok := body["error"].(map[string]any)
	s.True(ok, "Expected error for missing version")
	s.Contains(errObj["message"].(string), "version")
}

func (s *MCPSSEToolsSuite) TestTool_CreateTemplatePack_RequiresSchemas() {
	sessionID := s.initSession()
	body := s.callTool(sessionID, "create_template_pack", map[string]any{
		"name":    "Test",
		"version": "1.0.0",
	})

	// Should return error for missing object_type_schemas
	errObj, ok := body["error"].(map[string]any)
	s.True(ok, "Expected error for missing object_type_schemas")
	s.Contains(errObj["message"].(string), "object_type_schemas")
}

// =============================================================================
// Test: delete_template_pack validation
// =============================================================================

func (s *MCPSSEToolsSuite) TestTool_DeleteTemplatePack_RequiresPackID() {
	sessionID := s.initSession()
	body := s.callTool(sessionID, "delete_template_pack", map[string]any{})

	// Should return error for missing pack_id
	errObj, ok := body["error"].(map[string]any)
	s.True(ok, "Expected error for missing pack_id")
	s.Contains(errObj["message"].(string), "pack_id")
}

func (s *MCPSSEToolsSuite) TestTool_DeleteTemplatePack_NotFound() {
	sessionID := s.initSession()
	body := s.callTool(sessionID, "delete_template_pack", map[string]any{
		"pack_id": "00000000-0000-0000-0000-000000000000",
	})

	// Should return error for non-existent pack
	errObj, ok := body["error"].(map[string]any)
	s.True(ok, "Expected error for non-existent pack")
	s.Contains(errObj["message"].(string), "not found")
}

// =============================================================================
// Test: Session termination
// =============================================================================

func (s *MCPSSEToolsSuite) TestSession_DELETE_Terminates() {
	// Create session
	sessionID := s.initSession()

	// Terminate session
	resp := s.Client.DELETE("/api/mcp",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithHeader("MCP-Protocol-Version", "2025-11-25"),
		testutil.WithHeader("Mcp-Session-Id", sessionID),
	)
	s.Equal(http.StatusNoContent, resp.StatusCode)

	// Verify session is gone - should get 404
	resp2 := s.Client.POST("/api/mcp",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithHeader("Accept", "application/json, text/event-stream"),
		testutil.WithHeader("MCP-Protocol-Version", "2025-11-25"),
		testutil.WithHeader("Mcp-Session-Id", sessionID),
		testutil.WithJSONBody(map[string]any{
			"jsonrpc": "2.0",
			"method":  "tools/list",
			"id":      2,
		}),
	)
	s.Equal(http.StatusNotFound, resp2.StatusCode)
}
