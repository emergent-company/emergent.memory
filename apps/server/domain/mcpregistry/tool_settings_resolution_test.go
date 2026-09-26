package mcpregistry_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/domain/mcpregistry"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// MCPRegistryToolSettingsResolutionSuite proves ResolveBuiltinToolSettings honours
// three-tier precedence (issue #988): explicit project override → org default →
// builtin default, and reports `source` truthfully so the UI can distinguish
// inherited from explicit settings.
type MCPRegistryToolSettingsResolutionSuite struct {
	testutil.BaseSuite
}

func TestMCPRegistryToolSettingsResolutionSuite(t *testing.T) {
	suite.Run(t, new(MCPRegistryToolSettingsResolutionSuite))
}

func (s *MCPRegistryToolSettingsResolutionSuite) SetupSuite() {
	s.SetDBSuffix("mcpregistry_tool_settings_resolution")
	s.BaseSuite.SetupSuite()
}

// seedBuiltinTool inserts a builtin server and one tool row for the project. A nil
// enabledOverride mirrors EnsureBuiltinServer's bulk upsert (no explicit project
// override); a non-nil value mirrors an explicit project toggle.
func (s *MCPRegistryToolSettingsResolutionSuite) seedBuiltinTool(projectID, toolName string, enabledOverride *bool) string {
	serverID := uuid.New().String()
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.mcp_servers (id, project_id, name, enabled, type, created_at, updated_at)
		VALUES (?, ?, ?, true, 'builtin', NOW(), NOW())
	`, serverID, projectID, "builtin").Exec(s.Ctx)
	s.Require().NoError(err)

	_, err = s.DB().NewRaw(`
		INSERT INTO kb.mcp_server_tools (id, server_id, tool_name, enabled, enabled_override, created_at)
		VALUES (?, ?, ?, true, ?, NOW())
	`, uuid.New().String(), serverID, toolName, enabledOverride).Exec(s.Ctx)
	s.Require().NoError(err)

	return serverID
}

// seedOrgToolSetting upserts an org-level tool setting.
func (s *MCPRegistryToolSettingsResolutionSuite) seedOrgToolSetting(orgID, toolName string, enabled bool) {
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.org_tool_settings (org_id, tool_name, enabled, created_at, updated_at)
		VALUES (?, ?, ?, NOW(), NOW())
		ON CONFLICT (org_id, tool_name) DO UPDATE SET enabled = EXCLUDED.enabled, updated_at = NOW()
	`, orgID, toolName, enabled).Exec(s.Ctx)
	s.Require().NoError(err)
}

func (s *MCPRegistryToolSettingsResolutionSuite) svc() *mcpregistry.Service {
	return mcpregistry.NewService(mcpregistry.NewRepository(s.DB()), nil, nil, nil, slog.Default())
}

func (s *MCPRegistryToolSettingsResolutionSuite) TestOrgDefaultUsedWhenNoProjectOverride() {
	const toolName = "web-search-brave"
	s.seedBuiltinTool(s.ProjectID, toolName, nil) // bulk-upserted row, no explicit override
	s.seedOrgToolSetting(s.OrgID, toolName, false)

	enabled, _, source, err := s.svc().ResolveBuiltinToolSettings(s.Ctx, s.ProjectID, toolName)
	s.Require().NoError(err)
	s.Require().False(enabled, "expected org default (enabled=false) to win when no project override")
	s.Require().Equal("org", source)
}

func (s *MCPRegistryToolSettingsResolutionSuite) TestProjectOverrideWins() {
	const toolName = "web-search-brave"
	disabled := false
	s.seedBuiltinTool(s.ProjectID, toolName, &disabled) // explicit project disable
	s.seedOrgToolSetting(s.OrgID, toolName, true)       // org default enables

	enabled, _, source, err := s.svc().ResolveBuiltinToolSettings(s.Ctx, s.ProjectID, toolName)
	s.Require().NoError(err)
	s.Require().False(enabled, "expected explicit project override to beat org default")
	s.Require().Equal("project", source)
}

func (s *MCPRegistryToolSettingsResolutionSuite) TestBuiltinDefaultWhenNeitherSet() {
	const toolName = "web-search-brave"
	s.seedBuiltinTool(s.ProjectID, toolName, nil) // no override, no org setting

	enabled, _, source, err := s.svc().ResolveBuiltinToolSettings(s.Ctx, s.ProjectID, toolName)
	s.Require().NoError(err)
	s.Require().True(enabled, "expected builtin default (enabled=true) when neither project nor org set")
	s.Require().Equal("global", source)
}

// listToolFromServer returns the tool map for toolName from the list endpoint,
// proving the full HTTP surfacing path (ListServerTools → ResolveBuiltinToolSettings
// → enabled + inheritedFrom).
func (s *MCPRegistryToolSettingsResolutionSuite) listToolFromServer(serverID, toolName string) map[string]any {
	resp := s.Client.GET("/api/admin/mcp-servers/"+serverID+"/tools",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, resp.StatusCode, resp.String())

	var body struct {
		Success bool             `json:"success"`
		Data    []map[string]any `json:"data"`
	}
	s.Require().NoError(json.Unmarshal(resp.Body, &body))
	s.Require().True(body.Success)

	for _, t := range body.Data {
		if t["toolName"] == toolName {
			return t
		}
	}
	s.Require().Fail("tool not found in list", "toolName=%s", toolName)
	return nil
}

func (s *MCPRegistryToolSettingsResolutionSuite) TestListServerTools_SurfacesOrgDefault() {
	const toolName = "web-search-brave"
	serverID := s.seedBuiltinTool(s.ProjectID, toolName, nil) // no explicit project override
	s.seedOrgToolSetting(s.OrgID, toolName, false)

	tool := s.listToolFromServer(serverID, toolName)
	s.Require().Equal(false, tool["enabled"], "expected org default (enabled=false) surfaced in list")
	s.Require().Equal("org", tool["inheritedFrom"], "expected inheritedFrom=org")
}

func (s *MCPRegistryToolSettingsResolutionSuite) TestListServerTools_SurfacesProjectOverride() {
	const toolName = "web-search-brave"
	disabled := false
	serverID := s.seedBuiltinTool(s.ProjectID, toolName, &disabled) // explicit project disable
	s.seedOrgToolSetting(s.OrgID, toolName, true)                   // org default enables

	tool := s.listToolFromServer(serverID, toolName)
	s.Require().Equal(false, tool["enabled"], "expected project override (enabled=false) surfaced in list")
	s.Require().Equal("project", tool["inheritedFrom"], "expected inheritedFrom=project")
}
