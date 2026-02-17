package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent/internal/testutil"
)

type MCPSchemaLifecycleSuite struct {
	testutil.BaseSuite
}

func TestMCPSchemaLifecycleSuite(t *testing.T) {
	suite.Run(t, new(MCPSchemaLifecycleSuite))
}

func (s *MCPSchemaLifecycleSuite) SetupSuite() {
	s.SetDBSuffix("mcp_lifecycle")
	s.BaseSuite.SetupSuite()

	if !s.IsExternal() {
		s.T().Skip("Schema lifecycle tests require external server mode (set TEST_SERVER_URL)")
	}
}

func (s *MCPSchemaLifecycleSuite) initSessionWithProject(projectID string) string {
	params := map[string]any{
		"protocolVersion": "2025-11-25",
		"clientInfo": map[string]any{
			"name":    "mcp-lifecycle-test",
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

	s.Require().Equal(http.StatusOK, resp.StatusCode, "Initialize failed: %s", string(resp.Body))

	sessionID := resp.Headers.Get("Mcp-Session-Id")
	s.Require().NotEmpty(sessionID, "Expected Mcp-Session-Id header")

	return sessionID
}

func (s *MCPSchemaLifecycleSuite) callTool(sessionID, toolName string, args map[string]any) map[string]any {
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

	s.Require().Equal(http.StatusOK, resp.StatusCode, "Tool %s failed: %s", toolName, string(resp.Body))

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.Require().NoError(err)

	return body
}

func (s *MCPSchemaLifecycleSuite) callToolExpectError(sessionID, toolName string, args map[string]any) map[string]any {
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

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.Require().NoError(err)

	return body
}

func (s *MCPSchemaLifecycleSuite) parseToolResult(body map[string]any) map[string]any {
	if errField := body["error"]; errField != nil {
		s.T().Fatalf("Tool returned error: %v", errField)
	}

	result, ok := body["result"].(map[string]any)
	s.Require().True(ok, "Expected result field")

	content, ok := result["content"].([]any)
	s.Require().True(ok, "Expected content array")
	s.Require().NotEmpty(content, "Expected non-empty content")

	block := content[0].(map[string]any)
	s.Require().Equal("text", block["type"])

	text := block["text"].(string)
	var parsed map[string]any
	err := json.Unmarshal([]byte(text), &parsed)
	s.Require().NoError(err, "Failed to parse tool result JSON: %s", text)

	return parsed
}

func (s *MCPSchemaLifecycleSuite) TestFullSchemaLifecycle() {
	sessionID := s.initSessionWithProject(s.ProjectID)
	packName := fmt.Sprintf("CRM-Test-%d", time.Now().UnixNano())

	// Step 1: Create template pack with Person and Company schemas
	s.T().Log("Step 1: Creating template pack with Person and Company schemas")
	createPackResult := s.parseToolResult(s.callTool(sessionID, "create_template_pack", map[string]any{
		"name":        packName,
		"version":     "1.0.0",
		"description": "CRM schema for testing",
		"author":      "E2E Test",
		"object_type_schemas": map[string]any{
			"Person": map[string]any{
				"description": "A person entity",
				"properties": map[string]any{
					"name":  map[string]any{"type": "string"},
					"email": map[string]any{"type": "string"},
					"age":   map[string]any{"type": "integer"},
				},
			},
			"Company": map[string]any{
				"description": "A company entity",
				"properties": map[string]any{
					"name":     map[string]any{"type": "string"},
					"industry": map[string]any{"type": "string"},
				},
			},
		},
		"relationship_type_schemas": map[string]any{
			"WORKS_AT": map[string]any{
				"description": "Person works at Company",
				"source_type": "Person",
				"target_type": "Company",
			},
		},
	}))

	s.True(createPackResult["success"].(bool), "create_template_pack should succeed")
	pack := createPackResult["pack"].(map[string]any)
	packID := pack["id"].(string)
	s.NotEmpty(packID)
	s.T().Logf("Created template pack: %s (ID: %s)", packName, packID)

	// Step 2: Assign template pack to project
	s.T().Log("Step 2: Assigning template pack to project")
	assignResult := s.parseToolResult(s.callTool(sessionID, "assign_template_pack", map[string]any{
		"template_pack_id": packID,
	}))

	s.True(assignResult["success"].(bool), "assign_template_pack should succeed")
	assignmentID := assignResult["assignment_id"].(string)
	s.NotEmpty(assignmentID)
	s.T().Logf("Assigned template pack, assignment ID: %s", assignmentID)

	// Step 3: Verify installation via get_installed_templates
	s.T().Log("Step 3: Verifying installation")
	installedResult := s.parseToolResult(s.callTool(sessionID, "get_installed_templates", map[string]any{}))

	templates := installedResult["templates"].([]any)
	s.GreaterOrEqual(len(templates), 1, "Should have at least 1 installed pack")

	var foundPack bool
	for _, inst := range templates {
		instMap := inst.(map[string]any)
		if instMap["template_pack_id"] == packID {
			foundPack = true
			s.True(instMap["active"].(bool), "Pack should be active")
			break
		}
	}
	s.True(foundPack, "Should find our installed pack")

	// Step 4: Create Person entity
	s.T().Log("Step 4: Creating Person entity")
	personResult := s.parseToolResult(s.callTool(sessionID, "create_entity", map[string]any{
		"type": "Person",
		"properties": map[string]any{
			"name":  "John Doe",
			"email": "john@example.com",
			"age":   30,
		},
		"key":    "john-doe",
		"status": "active",
		"labels": []string{"employee", "engineering"},
	}))

	s.True(personResult["success"].(bool), "create_entity should succeed")
	personEntity := personResult["entity"].(map[string]any)
	personID := personEntity["id"].(string)
	s.NotEmpty(personID)
	s.Equal("Person", personEntity["type"])
	s.Equal("john-doe", personEntity["key"])
	s.T().Logf("Created Person entity: %s", personID)

	// Step 5: Create Company entity
	s.T().Log("Step 5: Creating Company entity")
	companyResult := s.parseToolResult(s.callTool(sessionID, "create_entity", map[string]any{
		"type": "Company",
		"properties": map[string]any{
			"name":     "Acme Inc",
			"industry": "Technology",
		},
		"key": "acme-inc",
	}))

	s.True(companyResult["success"].(bool), "create_entity should succeed")
	companyEntity := companyResult["entity"].(map[string]any)
	companyID := companyEntity["id"].(string)
	s.NotEmpty(companyID)
	s.Equal("Company", companyEntity["type"])
	s.T().Logf("Created Company entity: %s", companyID)

	// Step 6: Create WORKS_AT relationship
	s.T().Log("Step 6: Creating WORKS_AT relationship")
	relResult := s.parseToolResult(s.callTool(sessionID, "create_relationship", map[string]any{
		"type":      "WORKS_AT",
		"source_id": personID,
		"target_id": companyID,
		"properties": map[string]any{
			"role":       "Software Engineer",
			"start_date": "2024-01-15",
		},
	}))

	s.True(relResult["success"].(bool), "create_relationship should succeed")
	relationship := relResult["relationship"].(map[string]any)
	relID := relationship["id"].(string)
	s.NotEmpty(relID)
	s.Equal("WORKS_AT", relationship["type"])
	s.Equal(personID, relationship["source_id"])
	s.Equal(companyID, relationship["target_id"])
	s.T().Logf("Created WORKS_AT relationship: %s", relID)

	// Step 7: Query entities by type
	s.T().Log("Step 7: Querying Person entities")
	queryResult := s.parseToolResult(s.callTool(sessionID, "query_entities", map[string]any{
		"type_name": "Person",
		"limit":     10,
	}))

	entities := queryResult["entities"].([]any)
	s.GreaterOrEqual(len(entities), 1, "Should find at least 1 Person")

	var foundPerson bool
	for _, e := range entities {
		entity := e.(map[string]any)
		if entity["id"] == personID {
			foundPerson = true
			break
		}
	}
	s.True(foundPerson, "Should find our created Person")

	// Step 8: Search entities by text
	s.T().Log("Step 8: Searching for 'John'")
	searchResult := s.parseToolResult(s.callTool(sessionID, "search_entities", map[string]any{
		"query": "John",
		"limit": 10,
	}))

	searchEntities := searchResult["entities"].([]any)
	s.T().Logf("Search found %d entities", len(searchEntities))

	// Step 9: Get entity edges
	s.T().Log("Step 9: Getting entity edges for Person")
	edgesResult := s.parseToolResult(s.callTool(sessionID, "get_entity_edges", map[string]any{
		"entity_id": personID,
	}))

	outgoing := edgesResult["outgoing"].([]any)
	s.GreaterOrEqual(len(outgoing), 1, "Person should have at least 1 outgoing edge")

	var foundWorksAt bool
	for _, e := range outgoing {
		edge := e.(map[string]any)
		if edge["relationship_type"] == "WORKS_AT" {
			foundWorksAt = true
			connectedEntity := edge["connected_entity"].(map[string]any)
			s.Equal(companyID, connectedEntity["id"])
			break
		}
	}
	s.True(foundWorksAt, "Should find WORKS_AT relationship in edges")

	// Step 10: Update entity
	s.T().Log("Step 10: Updating Person entity")
	updateResult := s.parseToolResult(s.callTool(sessionID, "update_entity", map[string]any{
		"entity_id": personID,
		"properties": map[string]any{
			"age":   31,
			"title": "Senior Engineer",
		},
		"status": "promoted",
	}))

	s.True(updateResult["success"].(bool), "update_entity should succeed")
	updatedEntity := updateResult["entity"].(map[string]any)
	s.Equal(2, int(updatedEntity["version"].(float64)), "Version should be 2 after update")
	s.Equal("promoted", updatedEntity["status"])

	// Step 11: Verify list_entity_types shows our types
	s.T().Log("Step 11: Verifying list_entity_types")
	typesResult := s.parseToolResult(s.callTool(sessionID, "list_entity_types", map[string]any{}))

	types := typesResult["types"].([]any)
	s.T().Logf("Found %d entity types", len(types))

	var hasPersonType, hasCompanyType bool
	for _, t := range types {
		typeInfo := t.(map[string]any)
		typeName := typeInfo["name"].(string)
		if typeName == "Person" {
			hasPersonType = true
		}
		if typeName == "Company" {
			hasCompanyType = true
		}
	}
	s.True(hasPersonType, "Should have Person type")
	s.True(hasCompanyType, "Should have Company type")

	// Step 12: Delete Person entity
	s.T().Log("Step 12: Deleting Person entity")
	deleteResult := s.parseToolResult(s.callTool(sessionID, "delete_entity", map[string]any{
		"entity_id": personID,
	}))

	s.True(deleteResult["success"].(bool), "delete_entity should succeed")
	s.Equal(personID, deleteResult["entity_id"])

	// Step 12b: Delete Company entity (needed to uninstall template pack)
	s.T().Log("Step 12b: Deleting Company entity")
	deleteCompanyResult := s.parseToolResult(s.callTool(sessionID, "delete_entity", map[string]any{
		"entity_id": companyID,
	}))

	s.True(deleteCompanyResult["success"].(bool), "delete_entity for Company should succeed")
	s.Equal(companyID, deleteCompanyResult["entity_id"])

	// Step 13: Uninstall template pack
	s.T().Log("Step 13: Uninstalling template pack")
	uninstallResult := s.parseToolResult(s.callTool(sessionID, "uninstall_template_pack", map[string]any{
		"assignment_id": assignmentID,
	}))

	s.True(uninstallResult["success"].(bool), "uninstall_template_pack should succeed")

	// Step 14: Delete template pack
	s.T().Log("Step 14: Deleting template pack")
	deletePackResult := s.parseToolResult(s.callTool(sessionID, "delete_template_pack", map[string]any{
		"pack_id": packID,
	}))

	s.True(deletePackResult["success"].(bool), "delete_template_pack should succeed")

	s.T().Log("Full schema lifecycle test completed successfully!")
}

func (s *MCPSchemaLifecycleSuite) TestToolsListShowsAllTools() {
	sessionID := s.initSessionWithProject(s.ProjectID)

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

	s.Require().Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.Require().NoError(err)

	result := body["result"].(map[string]any)
	tools := result["tools"].([]any)

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
		"create_entity",
		"create_relationship",
		"update_entity",
		"delete_entity",
	}

	foundTools := make(map[string]bool)
	for _, t := range tools {
		tool := t.(map[string]any)
		foundTools[tool["name"].(string)] = true
	}

	for _, expected := range expectedTools {
		s.True(foundTools[expected], "Should have tool: %s", expected)
	}

	s.T().Logf("Found all %d expected tools", len(expectedTools))
}

func (s *MCPSchemaLifecycleSuite) TestCreateEntityRequiresType() {
	sessionID := s.initSessionWithProject(s.ProjectID)

	result := s.callToolExpectError(sessionID, "create_entity", map[string]any{
		"properties": map[string]any{"name": "Test"},
	})

	s.NotNil(result["error"], "Should return error for missing type")
}

func (s *MCPSchemaLifecycleSuite) TestCreateRelationshipRequiresAllParams() {
	sessionID := s.initSessionWithProject(s.ProjectID)

	result := s.callToolExpectError(sessionID, "create_relationship", map[string]any{
		"type":      "KNOWS",
		"source_id": "00000000-0000-0000-0000-000000000001",
	})

	s.NotNil(result["error"], "Should return error for missing target_id")
}

func (s *MCPSchemaLifecycleSuite) TestUpdateEntityRequiresEntityID() {
	sessionID := s.initSessionWithProject(s.ProjectID)

	result := s.callToolExpectError(sessionID, "update_entity", map[string]any{
		"properties": map[string]any{"name": "Updated"},
	})

	s.NotNil(result["error"], "Should return error for missing entity_id")
}

func (s *MCPSchemaLifecycleSuite) TestDeleteEntityRequiresEntityID() {
	sessionID := s.initSessionWithProject(s.ProjectID)

	result := s.callToolExpectError(sessionID, "delete_entity", map[string]any{})

	s.NotNil(result["error"], "Should return error for missing entity_id")
}
