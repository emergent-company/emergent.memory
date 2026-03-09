package e2e

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// ToolSettingsTestSuite tests per-org and per-project built-in tool settings.
type ToolSettingsTestSuite struct {
	testutil.BaseSuite
}

func TestToolSettingsSuite(t *testing.T) {
	suite.Run(t, new(ToolSettingsTestSuite))
}

func (s *ToolSettingsTestSuite) SetupSuite() {
	s.SetDBSuffix("tool_settings")
	s.BaseSuite.SetupSuite()
}

// ─────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────

// getBuiltinServerID calls GET /api/admin/mcp-servers (which auto-creates the
// builtin server) and returns the ID of the builtin server for this project.
func (s *ToolSettingsTestSuite) getBuiltinServerID() string {
	resp := s.Client.GET("/api/admin/mcp-servers",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "list servers: %s", resp.String())

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.Require().NoError(err)

	data := result["data"].([]any)
	for _, item := range data {
		srv := item.(map[string]any)
		if srv["type"].(string) == "builtin" {
			return srv["id"].(string)
		}
	}
	s.Fail("builtin server not found")
	return ""
}

// listBuiltinTools returns the tools slice from the builtin server's tool list.
func (s *ToolSettingsTestSuite) listBuiltinTools(serverID string) []map[string]any {
	resp := s.Client.GET("/api/admin/mcp-servers/"+serverID+"/tools",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "list tools: %s", resp.String())

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.Require().NoError(err)

	raw := result["data"].([]any)
	tools := make([]map[string]any, 0, len(raw))
	for _, r := range raw {
		tools = append(tools, r.(map[string]any))
	}
	return tools
}

// findTool returns the first tool in the list with the given name.
func findTool(tools []map[string]any, name string) (map[string]any, bool) {
	for _, t := range tools {
		if t["toolName"].(string) == name {
			return t, true
		}
	}
	return nil, false
}

// ─────────────────────────────────────────────────────────────
// 10.2 TestProjectToolSettings_ToggleBuiltinTool
// ─────────────────────────────────────────────────────────────

func (s *ToolSettingsTestSuite) TestProjectToolSettings_ToggleBuiltinTool() {
	serverID := s.getBuiltinServerID()
	tools := s.listBuiltinTools(serverID)

	// Find brave_web_search tool (should exist as a builtin)
	tool, ok := findTool(tools, "brave_web_search")
	if !ok {
		s.T().Skip("brave_web_search not in builtin tools list — skipping")
	}

	toolID := tool["id"].(string)
	initialEnabled := tool["enabled"].(bool)

	// Toggle to the opposite
	resp := s.Client.PATCH("/api/admin/mcp-servers/"+serverID+"/tools/"+toolID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"enabled": !initialEnabled,
		}),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "toggle: %s", resp.String())

	// Re-fetch and verify
	tools = s.listBuiltinTools(serverID)
	tool, ok = findTool(tools, "brave_web_search")
	s.Require().True(ok)
	s.Equal(!initialEnabled, tool["enabled"].(bool))

	// Restore
	resp = s.Client.PATCH("/api/admin/mcp-servers/"+serverID+"/tools/"+toolID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"enabled": initialEnabled,
		}),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "restore: %s", resp.String())
}

// ─────────────────────────────────────────────────────────────
// 10.3 TestOrgToolSettings_CRUD
// ─────────────────────────────────────────────────────────────

func (s *ToolSettingsTestSuite) TestOrgToolSettings_CRUD() {
	toolName := "brave_web_search"

	// Create (upsert) an org tool setting
	resp := s.Client.PUT("/api/admin/orgs/"+s.OrgID+"/tool-settings/"+toolName,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"enabled": false,
			"config":  map[string]any{"api_key": "test-key-123"},
		}),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "upsert: %s", resp.String())

	var upsertResult map[string]any
	err := json.Unmarshal(resp.Body, &upsertResult)
	s.Require().NoError(err)
	s.True(upsertResult["success"].(bool))

	data := upsertResult["data"].(map[string]any)
	s.Equal(toolName, data["toolName"])
	s.Equal(false, data["enabled"])

	// Read: list org tool settings
	resp = s.Client.GET("/api/admin/orgs/"+s.OrgID+"/tool-settings",
		testutil.WithAuth("e2e-test-user"),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "list: %s", resp.String())

	var listResult map[string]any
	err = json.Unmarshal(resp.Body, &listResult)
	s.Require().NoError(err)
	s.True(listResult["success"].(bool))

	list := listResult["data"].([]any)
	s.Require().GreaterOrEqual(len(list), 1)

	var found map[string]any
	for _, item := range list {
		ts := item.(map[string]any)
		if ts["toolName"].(string) == toolName {
			found = ts
			break
		}
	}
	s.Require().NotNil(found, "org tool setting not in list")
	s.Equal(false, found["enabled"].(bool))

	// Update: re-upsert with enabled=true
	resp = s.Client.PUT("/api/admin/orgs/"+s.OrgID+"/tool-settings/"+toolName,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"enabled": true,
		}),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "update: %s", resp.String())

	var updateResult map[string]any
	err = json.Unmarshal(resp.Body, &updateResult)
	s.Require().NoError(err)
	s.Equal(true, updateResult["data"].(map[string]any)["enabled"].(bool))

	// Delete: remove org override
	resp = s.Client.DELETE("/api/admin/orgs/"+s.OrgID+"/tool-settings/"+toolName,
		testutil.WithAuth("e2e-test-user"),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "delete: %s", resp.String())

	// Verify gone
	resp = s.Client.GET("/api/admin/orgs/"+s.OrgID+"/tool-settings",
		testutil.WithAuth("e2e-test-user"),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode)
	err = json.Unmarshal(resp.Body, &listResult)
	s.Require().NoError(err)

	list = listResult["data"].([]any)
	for _, item := range list {
		ts := item.(map[string]any)
		s.NotEqual(toolName, ts["toolName"], "deleted tool setting should be gone")
	}
}

// ─────────────────────────────────────────────────────────────
// 10.4 TestToolInheritance_OrgDefaultUsedWhenNoProjectOverride
// ─────────────────────────────────────────────────────────────

func (s *ToolSettingsTestSuite) TestToolInheritance_OrgDefaultUsedWhenNoProjectOverride() {
	toolName := "brave_web_search"

	// Disable at org level
	resp := s.Client.PUT("/api/admin/orgs/"+s.OrgID+"/tool-settings/"+toolName,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"enabled": false,
		}),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "set org: %s", resp.String())

	// Get the builtin server (no project-level override set yet)
	serverID := s.getBuiltinServerID()
	tools := s.listBuiltinTools(serverID)

	tool, ok := findTool(tools, toolName)
	if !ok {
		s.T().Skip("brave_web_search not in builtin tools — skipping")
	}

	// Should reflect org-level disabled and inheritedFrom = "org"
	s.Equal(false, tool["enabled"].(bool),
		"tool should be disabled per org default")
	s.Equal("org", tool["inheritedFrom"],
		"inheritedFrom should be 'org' when no project override exists")

	// Cleanup: remove org setting
	s.Client.DELETE("/api/admin/orgs/"+s.OrgID+"/tool-settings/"+toolName,
		testutil.WithAuth("e2e-test-user"),
	)
}

// ─────────────────────────────────────────────────────────────
// 10.5 TestToolInheritance_ProjectOverridesOrg
// ─────────────────────────────────────────────────────────────

func (s *ToolSettingsTestSuite) TestToolInheritance_ProjectOverridesOrg() {
	toolName := "brave_web_search"

	// Enable at org level
	resp := s.Client.PUT("/api/admin/orgs/"+s.OrgID+"/tool-settings/"+toolName,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"enabled": true,
		}),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "org enable: %s", resp.String())

	// Get the builtin server to find the tool
	serverID := s.getBuiltinServerID()
	tools := s.listBuiltinTools(serverID)

	tool, ok := findTool(tools, toolName)
	if !ok {
		s.T().Skip("brave_web_search not in builtin tools — skipping")
	}
	toolID := tool["id"].(string)

	// Disable at project level (override)
	resp = s.Client.PATCH("/api/admin/mcp-servers/"+serverID+"/tools/"+toolID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"enabled": false,
		}),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "project disable: %s", resp.String())

	// Re-list tools
	tools = s.listBuiltinTools(serverID)
	tool, ok = findTool(tools, toolName)
	s.Require().True(ok)

	// Project override should win
	s.Equal(false, tool["enabled"].(bool),
		"project-level disabled should override org-level enabled")
	s.Equal("project", tool["inheritedFrom"],
		"inheritedFrom should be 'project' when project override is set")

	// Cleanup: remove org setting
	s.Client.DELETE("/api/admin/orgs/"+s.OrgID+"/tool-settings/"+toolName,
		testutil.WithAuth("e2e-test-user"),
	)
}

// ─────────────────────────────────────────────────────────────
// 10.6 TestBraveWebSearch_ProjectApiKey
// ─────────────────────────────────────────────────────────────

func (s *ToolSettingsTestSuite) TestBraveWebSearch_ProjectApiKey() {
	toolName := "brave_web_search"

	// Set a per-project API key via the MCP server tool config
	serverID := s.getBuiltinServerID()
	tools := s.listBuiltinTools(serverID)

	tool, ok := findTool(tools, toolName)
	if !ok {
		s.T().Skip("brave_web_search not in builtin tools — skipping")
	}
	toolID := tool["id"].(string)

	// Store a sentinel API key at project level
	resp := s.Client.PATCH("/api/admin/mcp-servers/"+serverID+"/tools/"+toolID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"enabled": true,
			"config":  map[string]any{"api_key": "PROJ-TEST-KEY"},
		}),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "set project api_key: %s", resp.String())

	// Verify config is persisted via the tool list
	tools = s.listBuiltinTools(serverID)
	tool, ok = findTool(tools, toolName)
	s.Require().True(ok)

	// Config should be present in response
	cfg, hasCfg := tool["config"]
	s.Require().True(hasCfg, "config field should be present in tool response")
	if cfgMap, ok := cfg.(map[string]any); ok {
		s.Equal("PROJ-TEST-KEY", cfgMap["api_key"],
			"api_key should be stored at project level")
	}

	// inheritedFrom should be "project" since we set a project-level config
	s.Equal("project", tool["inheritedFrom"],
		"inheritedFrom should be 'project' after setting project-level config")
}
