package e2e

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/emergent-company/emergent/internal/testutil"
	"github.com/stretchr/testify/suite"
)

// MCPRegistryTestSuite tests the MCP registry REST API endpoints.
type MCPRegistryTestSuite struct {
	testutil.BaseSuite
}

func TestMCPRegistrySuite(t *testing.T) {
	suite.Run(t, new(MCPRegistryTestSuite))
}

func (s *MCPRegistryTestSuite) SetupSuite() {
	s.SetDBSuffix("mcpregistry")
	s.BaseSuite.SetupSuite()
}

// --- Auth tests ---

func (s *MCPRegistryTestSuite) TestListServers_RequiresAuth() {
	resp := s.Client.GET("/api/admin/mcp-servers")
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *MCPRegistryTestSuite) TestCreateServer_RequiresAuth() {
	resp := s.Client.POST("/api/admin/mcp-servers",
		testutil.WithJSONBody(map[string]any{
			"name": "test-server",
			"type": "http",
			"url":  "https://example.com/mcp",
		}),
	)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

// --- CRUD tests ---

func (s *MCPRegistryTestSuite) TestCreateServer_Success() {
	resp := s.Client.POST("/api/admin/mcp-servers",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"name": "test-http-server",
			"type": "http",
			"url":  "https://example.com/mcp",
		}),
	)
	s.Require().Equal(http.StatusCreated, resp.StatusCode, "body: %s", resp.String())

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)
	s.True(result["success"].(bool))

	data := result["data"].(map[string]any)
	s.NotEmpty(data["id"])
	s.Equal("test-http-server", data["name"])
	s.Equal("http", data["type"])
	s.Equal(true, data["enabled"])
}

func (s *MCPRegistryTestSuite) TestCreateServer_StdioType() {
	cmd := "npx"
	resp := s.Client.POST("/api/admin/mcp-servers",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"name":    "test-stdio-server",
			"type":    "stdio",
			"command": cmd,
			"args":    []string{"-y", "@modelcontextprotocol/server-filesystem"},
		}),
	)
	s.Require().Equal(http.StatusCreated, resp.StatusCode, "body: %s", resp.String())

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)
	s.True(result["success"].(bool))

	data := result["data"].(map[string]any)
	s.Equal("stdio", data["type"])
}

func (s *MCPRegistryTestSuite) TestCreateServer_DuplicateName() {
	// Create first server
	resp := s.Client.POST("/api/admin/mcp-servers",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"name": "duplicate-name",
			"type": "http",
			"url":  "https://example.com/mcp",
		}),
	)
	s.Require().Equal(http.StatusCreated, resp.StatusCode, "body: %s", resp.String())

	// Try to create second with same name
	resp = s.Client.POST("/api/admin/mcp-servers",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"name": "duplicate-name",
			"type": "http",
			"url":  "https://other.com/mcp",
		}),
	)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *MCPRegistryTestSuite) TestCreateServer_BuiltinTypeRejected() {
	resp := s.Client.POST("/api/admin/mcp-servers",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"name": "sneaky-builtin",
			"type": "builtin",
		}),
	)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *MCPRegistryTestSuite) TestCreateServer_HttpMissingURL() {
	resp := s.Client.POST("/api/admin/mcp-servers",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"name": "missing-url",
			"type": "http",
		}),
	)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *MCPRegistryTestSuite) TestCreateServer_StdioMissingCommand() {
	resp := s.Client.POST("/api/admin/mcp-servers",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"name": "missing-cmd",
			"type": "stdio",
		}),
	)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *MCPRegistryTestSuite) TestListServers_Empty() {
	resp := s.Client.GET("/api/admin/mcp-servers",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "body: %s", resp.String())

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)
	s.True(result["success"].(bool))

	// Empty list may serialize as null or [] depending on handler
	data := result["data"]
	if data == nil {
		// null data means empty list
		return
	}
	s.Equal(0, len(data.([]any)))
}

func (s *MCPRegistryTestSuite) TestListServers_WithServers() {
	// Create two servers
	s.createTestServer("server-a", "http", "https://a.example.com/mcp")
	s.createTestServer("server-b", "http", "https://b.example.com/mcp")

	resp := s.Client.GET("/api/admin/mcp-servers",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "body: %s", resp.String())

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	data := result["data"].([]any)
	s.Equal(2, len(data))

	// Verify alphabetical ordering
	first := data[0].(map[string]any)
	second := data[1].(map[string]any)
	s.Equal("server-a", first["name"])
	s.Equal("server-b", second["name"])
}

func (s *MCPRegistryTestSuite) TestGetServer_Success() {
	serverID := s.createTestServer("get-me", "http", "https://example.com/mcp")

	resp := s.Client.GET("/api/admin/mcp-servers/"+serverID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "body: %s", resp.String())

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	data := result["data"].(map[string]any)
	s.Equal(serverID, data["id"])
	s.Equal("get-me", data["name"])
	// Detail DTO includes tools array
	tools := data["tools"].([]any)
	s.Equal(0, len(tools))
}

func (s *MCPRegistryTestSuite) TestGetServer_NotFound() {
	resp := s.Client.GET("/api/admin/mcp-servers/00000000-0000-0000-0000-000000000000",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusNotFound, resp.StatusCode)
}

func (s *MCPRegistryTestSuite) TestUpdateServer_Success() {
	serverID := s.createTestServer("update-me", "http", "https://example.com/mcp")

	resp := s.Client.PATCH("/api/admin/mcp-servers/"+serverID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"name":    "updated-name",
			"enabled": false,
		}),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "body: %s", resp.String())

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	data := result["data"].(map[string]any)
	s.Equal("updated-name", data["name"])
	s.Equal(false, data["enabled"])
}

func (s *MCPRegistryTestSuite) TestDeleteServer_Success() {
	serverID := s.createTestServer("delete-me", "http", "https://example.com/mcp")

	resp := s.Client.DELETE("/api/admin/mcp-servers/"+serverID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "body: %s", resp.String())

	// Verify it's gone
	resp = s.Client.GET("/api/admin/mcp-servers/"+serverID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusNotFound, resp.StatusCode)
}

// --- Tool sync and toggle tests ---

func (s *MCPRegistryTestSuite) TestSyncTools_Success() {
	serverID := s.createTestServer("tool-server", "http", "https://example.com/mcp")

	// Sync some tools
	tools := []map[string]any{
		{
			"name":        "read_file",
			"description": "Read a file from disk",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "File path to read",
					},
				},
				"required": []string{"path"},
			},
		},
		{
			"name":        "write_file",
			"description": "Write content to a file",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "File path to write",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "Content to write",
					},
				},
				"required": []string{"path", "content"},
			},
		},
	}

	resp := s.Client.POST("/api/admin/mcp-servers/"+serverID+"/sync",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(tools),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "body: %s", resp.String())

	// Verify tools are listed
	resp = s.Client.GET("/api/admin/mcp-servers/"+serverID+"/tools",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "body: %s", resp.String())

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	data := result["data"].([]any)
	s.Equal(2, len(data))
}

func (s *MCPRegistryTestSuite) TestSyncTools_RemovesStale() {
	serverID := s.createTestServer("stale-server", "http", "https://example.com/mcp")

	// First sync: 3 tools
	tools := []map[string]any{
		{"name": "tool_a", "description": "Tool A"},
		{"name": "tool_b", "description": "Tool B"},
		{"name": "tool_c", "description": "Tool C"},
	}
	resp := s.Client.POST("/api/admin/mcp-servers/"+serverID+"/sync",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(tools),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "body: %s", resp.String())

	// Second sync: only 2 tools (tool_c removed)
	tools = []map[string]any{
		{"name": "tool_a", "description": "Tool A updated"},
		{"name": "tool_b", "description": "Tool B"},
	}
	resp = s.Client.POST("/api/admin/mcp-servers/"+serverID+"/sync",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(tools),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "body: %s", resp.String())

	// Verify only 2 tools remain
	resp = s.Client.GET("/api/admin/mcp-servers/"+serverID+"/tools",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "body: %s", resp.String())

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	data := result["data"].([]any)
	s.Equal(2, len(data))
}

func (s *MCPRegistryTestSuite) TestToggleTool_Success() {
	serverID := s.createTestServer("toggle-server", "http", "https://example.com/mcp")

	// Sync a tool
	tools := []map[string]any{
		{"name": "my_tool", "description": "A test tool"},
	}
	resp := s.Client.POST("/api/admin/mcp-servers/"+serverID+"/sync",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(tools),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "body: %s", resp.String())

	// Get the tool ID
	resp = s.Client.GET("/api/admin/mcp-servers/"+serverID+"/tools",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "body: %s", resp.String())

	var listResult map[string]any
	err := json.Unmarshal(resp.Body, &listResult)
	s.NoError(err)

	data := listResult["data"].([]any)
	s.Require().Equal(1, len(data))
	toolData := data[0].(map[string]any)
	toolID := toolData["id"].(string)
	s.Equal(true, toolData["enabled"])

	// Disable the tool
	resp = s.Client.PATCH("/api/admin/mcp-servers/"+serverID+"/tools/"+toolID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"enabled": false,
		}),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "body: %s", resp.String())

	// Verify the tool is disabled
	resp = s.Client.GET("/api/admin/mcp-servers/"+serverID+"/tools",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "body: %s", resp.String())

	err = json.Unmarshal(resp.Body, &listResult)
	s.NoError(err)

	data = listResult["data"].([]any)
	toolData = data[0].(map[string]any)
	s.Equal(false, toolData["enabled"])
}

func (s *MCPRegistryTestSuite) TestDeleteServer_CascadesTools() {
	serverID := s.createTestServer("cascade-server", "http", "https://example.com/mcp")

	// Sync tools
	tools := []map[string]any{
		{"name": "tool_x", "description": "Tool X"},
	}
	resp := s.Client.POST("/api/admin/mcp-servers/"+serverID+"/sync",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(tools),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "body: %s", resp.String())

	// Delete the server
	resp = s.Client.DELETE("/api/admin/mcp-servers/"+serverID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "body: %s", resp.String())

	// Tools endpoint should 404 since server is gone
	resp = s.Client.GET("/api/admin/mcp-servers/"+serverID+"/tools",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusNotFound, resp.StatusCode)
}

// --- Helper ---

// createTestServer creates an HTTP-type MCP server and returns its ID.
func (s *MCPRegistryTestSuite) createTestServer(name, serverType, url string) string {
	body := map[string]any{
		"name": name,
		"type": serverType,
	}
	if url != "" {
		body["url"] = url
	}

	resp := s.Client.POST("/api/admin/mcp-servers",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(body),
	)
	s.Require().Equal(http.StatusCreated, resp.StatusCode, "failed to create test server %s: %s", name, resp.String())

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.Require().NoError(err)

	data := result["data"].(map[string]any)
	return data["id"].(string)
}

// ============================================================================
// Official MCP Registry Browse/Install Tests
// ============================================================================

func (s *MCPRegistryTestSuite) TestSearchRegistry_RequiresAuth() {
	resp := s.Client.GET("/api/admin/mcp-registry/search?q=github")
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *MCPRegistryTestSuite) TestSearchRegistry_Success() {
	resp := s.Client.GET("/api/admin/mcp-registry/search?q=github&limit=5",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "body: %s", resp.String())

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)
	s.True(result["success"].(bool))

	data := result["data"].(map[string]any)
	servers := data["servers"].([]any)
	s.Greater(len(servers), 0, "expected at least one server matching 'github'")

	// Verify server structure
	first := servers[0].(map[string]any)
	s.NotEmpty(first["name"])
	s.NotEmpty(first["description"])
}

func (s *MCPRegistryTestSuite) TestSearchRegistry_EmptyQuery() {
	resp := s.Client.GET("/api/admin/mcp-registry/search?limit=3",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "body: %s", resp.String())

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)
	s.True(result["success"].(bool))

	data := result["data"].(map[string]any)
	servers := data["servers"].([]any)
	s.Greater(len(servers), 0, "expected at least one server in the registry")
}

func (s *MCPRegistryTestSuite) TestGetRegistryServer_Success() {
	// Use a well-known server name from the official registry
	resp := s.Client.GET("/api/admin/mcp-registry/servers/io.github.github%2Fgithub-mcp-server",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "body: %s", resp.String())

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)
	s.True(result["success"].(bool))

	data := result["data"].(map[string]any)
	s.Contains(data["name"], "github")
	s.NotEmpty(data["description"])
	s.NotEmpty(data["version"])
}

func (s *MCPRegistryTestSuite) TestGetRegistryServer_NotFound() {
	resp := s.Client.GET("/api/admin/mcp-registry/servers/io.nonexistent.server%2Fdoes-not-exist",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	// Should return a bad request (since we pass the error through)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *MCPRegistryTestSuite) TestInstallFromRegistry_RequiresAuth() {
	resp := s.Client.POST("/api/admin/mcp-registry/install",
		testutil.WithJSONBody(map[string]any{
			"registryName": "io.github.github/github-mcp-server",
		}),
	)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *MCPRegistryTestSuite) TestInstallFromRegistry_Success() {
	resp := s.Client.POST("/api/admin/mcp-registry/install",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"registryName": "io.github.github/github-mcp-server",
		}),
	)
	s.Require().Equal(http.StatusCreated, resp.StatusCode, "body: %s", resp.String())

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)
	s.True(result["success"].(bool))

	data := result["data"].(map[string]any)

	// Install response is wrapped in InstallResultDTO
	server := data["server"].(map[string]any)
	s.NotEmpty(server["id"])
	s.Equal("github-mcp-server", server["name"])

	// GitHub MCP server has a streamable-http remote → installed as http
	s.Equal("http", server["type"].(string))

	// Should have requiredEnvVars array (may be empty or populated depending on registry data)
	envVars := data["requiredEnvVars"].([]any)
	s.NotNil(envVars)
}

func (s *MCPRegistryTestSuite) TestInstallFromRegistry_CustomName() {
	resp := s.Client.POST("/api/admin/mcp-registry/install",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"registryName": "io.github.github/github-mcp-server",
			"name":         "my-github",
		}),
	)
	s.Require().Equal(http.StatusCreated, resp.StatusCode, "body: %s", resp.String())

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	data := result["data"].(map[string]any)
	server := data["server"].(map[string]any)
	s.Equal("my-github", server["name"])
}

func (s *MCPRegistryTestSuite) TestInstallFromRegistry_MissingRegistryName() {
	resp := s.Client.POST("/api/admin/mcp-registry/install",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{}),
	)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *MCPRegistryTestSuite) TestInstallFromRegistry_DuplicateName() {
	// First install
	resp := s.Client.POST("/api/admin/mcp-registry/install",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"registryName": "io.github.github/github-mcp-server",
			"name":         "dup-install",
		}),
	)
	s.Require().Equal(http.StatusCreated, resp.StatusCode, "body: %s", resp.String())

	// Second install with same name should fail
	resp = s.Client.POST("/api/admin/mcp-registry/install",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"registryName": "io.github.github/github-mcp-server",
			"name":         "dup-install",
		}),
	)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *MCPRegistryTestSuite) TestInstallFromRegistry_StdioOnlyBlocked() {
	// Context7 official only has npm packages (stdio) — should be blocked
	resp := s.Client.POST("/api/admin/mcp-registry/install",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"registryName": "io.github.upstash/context7",
		}),
	)
	s.Equal(http.StatusBadRequest, resp.StatusCode)

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	// Error message should mention stdio not being supported
	msg := result["error"].(map[string]any)["message"].(string)
	s.Contains(msg, "stdio")
}

// ============================================================================
// Inspect/Test-Connection Tests
// ============================================================================

func (s *MCPRegistryTestSuite) TestInspectServer_RequiresAuth() {
	resp := s.Client.POST("/api/admin/mcp-servers/00000000-0000-0000-0000-000000000000/inspect")
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *MCPRegistryTestSuite) TestInspectServer_NotFound() {
	resp := s.Client.POST("/api/admin/mcp-servers/00000000-0000-0000-0000-000000000000/inspect",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusNotFound, resp.StatusCode)
}

func (s *MCPRegistryTestSuite) TestInspectServer_BuiltinServer() {
	// Ensure the builtin server exists by listing servers (triggers auto-create)
	resp := s.Client.GET("/api/admin/mcp-servers",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "body: %s", resp.String())

	// Create a builtin server manually — we need its ID.
	// The list endpoint ensures the builtin server exists, so find it.
	var listResult map[string]any
	err := json.Unmarshal(resp.Body, &listResult)
	s.Require().NoError(err)

	// Find the builtin server from the list (or it may be the only one if tests are isolated)
	data := listResult["data"]
	var builtinID string
	if data != nil {
		servers := data.([]any)
		for _, srv := range servers {
			srvMap := srv.(map[string]any)
			if srvMap["type"].(string) == "builtin" {
				builtinID = srvMap["id"].(string)
				break
			}
		}
	}

	// If no builtin server found in list (e.g., list didn't auto-create), skip
	if builtinID == "" {
		s.T().Skip("no builtin server found — skipping inspect builtin test")
	}

	// Inspect the builtin server
	resp = s.Client.POST("/api/admin/mcp-servers/"+builtinID+"/inspect",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "body: %s", resp.String())

	var result map[string]any
	err = json.Unmarshal(resp.Body, &result)
	s.NoError(err)
	s.True(result["success"].(bool))

	inspect := result["data"].(map[string]any)
	s.Equal("ok", inspect["status"])
	s.Equal("builtin", inspect["serverType"])
	s.NotEmpty(inspect["serverId"])
	s.NotEmpty(inspect["serverName"])

	// Builtin server returns synthetic server info
	serverInfo := inspect["serverInfo"].(map[string]any)
	s.Equal("emergent-builtin", serverInfo["name"])
	s.Equal("1.0.0", serverInfo["version"])

	// Capabilities should show tools=true
	capabilities := inspect["capabilities"].(map[string]any)
	s.Equal(true, capabilities["tools"])

	// Empty arrays (not null) for tools/prompts/resources
	s.NotNil(inspect["tools"])
	s.NotNil(inspect["prompts"])
	s.NotNil(inspect["resources"])
	s.NotNil(inspect["resourceTemplates"])
}

func (s *MCPRegistryTestSuite) TestInspectServer_UnreachableHTTP() {
	// Create a server pointing to an unreachable URL
	serverID := s.createTestServer("unreachable-inspect", "http", "http://192.0.2.1:9999/mcp")

	resp := s.Client.POST("/api/admin/mcp-servers/"+serverID+"/inspect",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "body: %s", resp.String())

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)
	s.True(result["success"].(bool))

	inspect := result["data"].(map[string]any)
	s.Equal("error", inspect["status"])
	s.NotNil(inspect["error"], "expected error message for unreachable server")
	s.NotEmpty(inspect["error"].(string))
	s.Equal("http", inspect["serverType"])
}
