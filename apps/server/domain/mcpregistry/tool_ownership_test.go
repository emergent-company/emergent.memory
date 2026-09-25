package mcpregistry_test

import (
	"errors"
	"log/slog"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/domain/mcpregistry"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// MCPRegistryToolOwnershipSuite proves the tool-mutation routes are project-scoped
// (issue #978): a project member can enable/disable their own project's tools, but
// a foreign toolId — even addressed through the member's own server path — is
// refused with 404 and leaves the victim's state untouched.
type MCPRegistryToolOwnershipSuite struct {
	testutil.BaseSuite

	// project B (foreign) fixtures
	projectBID string
	serverBID  string
	toolBID    string
}

func TestMCPRegistryToolOwnershipSuite(t *testing.T) {
	suite.Run(t, new(MCPRegistryToolOwnershipSuite))
}

func (s *MCPRegistryToolOwnershipSuite) SetupSuite() {
	s.SetDBSuffix("mcpregistry_tool_ownership")
	s.BaseSuite.SetupSuite()
}

func (s *MCPRegistryToolOwnershipSuite) SetupTest() {
	s.BaseSuite.SetupTest()

	// A foreign project (different org) with its own server + tool.
	orgB := uuid.New().String()
	s.Require().NoError(testutil.CreateTestOrganization(s.Ctx, s.DB(), orgB, "Org B"))
	s.projectBID = uuid.New().String()
	s.Require().NoError(testutil.CreateTestProject(s.Ctx, s.DB(), testutil.TestProject{
		ID:    s.projectBID,
		OrgID: orgB,
		Name:  "Project B",
	}, testutil.AdminUser.ID))

	s.serverBID, s.toolBID = s.seedServerAndTool(s.projectBID, "foreign-tool")
}

// seedServerAndTool inserts a builtin MCP server and a single enabled tool for a
// project and returns the (serverID, toolID) pair.
func (s *MCPRegistryToolOwnershipSuite) seedServerAndTool(projectID, toolName string) (string, string) {
	serverID := uuid.New().String()
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.mcp_servers (id, project_id, name, enabled, type, created_at, updated_at)
		VALUES (?, ?, ?, true, 'builtin', NOW(), NOW())
	`, serverID, projectID, "builtin").Exec(s.Ctx)
	s.Require().NoError(err)

	toolID := uuid.New().String()
	_, err = s.DB().NewRaw(`
		INSERT INTO kb.mcp_server_tools (id, server_id, tool_name, enabled, created_at)
		VALUES (?, ?, ?, true, NOW())
	`, toolID, serverID, toolName).Exec(s.Ctx)
	s.Require().NoError(err)

	return serverID, toolID
}

// readToolEnabled returns the current enabled state of a tool by bare ID.
func (s *MCPRegistryToolOwnershipSuite) readToolEnabled(toolID string) bool {
	var enabled bool
	err := s.DB().NewRaw(`SELECT enabled FROM kb.mcp_server_tools WHERE id = ?`, toolID).Scan(s.Ctx, &enabled)
	s.Require().NoError(err)
	return enabled
}

// seedToolOnServer inserts an enabled tool row for an existing server.
func (s *MCPRegistryToolOwnershipSuite) seedToolOnServer(serverID, toolName string) {
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.mcp_server_tools (id, server_id, tool_name, enabled, created_at)
		VALUES (?, ?, ?, true, NOW())
	`, uuid.New().String(), serverID, toolName).Exec(s.Ctx)
	s.Require().NoError(err)
}

// listToolNames returns the tool_name values for a server, sorted.
func (s *MCPRegistryToolOwnershipSuite) listToolNames(serverID string) []string {
	var names []string
	err := s.DB().NewRaw(`SELECT tool_name FROM kb.mcp_server_tools WHERE server_id = ? ORDER BY tool_name`, serverID).Scan(s.Ctx, &names)
	s.Require().NoError(err)
	return names
}

// --- ToggleTool (PATCH /api/admin/mcp-servers/:id/tools/:toolId) ---

func (s *MCPRegistryToolOwnershipSuite) TestToggleForeignToolRefused404() {
	// A member of project A addresses project B's tool through A's own server id.
	// The :id is authoritative and the tool is project-scoped: 404, victim unchanged.
	serverAID, _ := s.seedServerAndTool(s.ProjectID, "own-tool")

	resp := s.Client.PATCH("/api/admin/mcp-servers/"+serverAID+"/tools/"+s.toolBID,
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{"enabled": false}))
	victimEnabled := s.readToolEnabled(s.toolBID)

	s.Require().Equal(http.StatusNotFound, resp.StatusCode,
		"foreign tool PATCH must be 404, got %d (victim enabled=%v): %s", resp.StatusCode, victimEnabled, resp.String())
	s.Require().True(victimEnabled,
		"victim tool must be unchanged (enabled=true), got enabled=%v", victimEnabled)
}

func (s *MCPRegistryToolOwnershipSuite) TestToggleForeignServerRefused404() {
	// A member of project A addresses project B's server id directly: 404, victim unchanged.
	_, toolAID := s.seedServerAndTool(s.ProjectID, "own-tool")

	resp := s.Client.PATCH("/api/admin/mcp-servers/"+s.serverBID+"/tools/"+toolAID,
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{"enabled": false}))
	victimEnabled := s.readToolEnabled(s.toolBID)

	s.Require().Equal(http.StatusNotFound, resp.StatusCode,
		"foreign server :id PATCH must be 404, got %d (victim enabled=%v): %s", resp.StatusCode, victimEnabled, resp.String())
}

func (s *MCPRegistryToolOwnershipSuite) TestToggleOwnToolOK() {
	serverAID, toolAID := s.seedServerAndTool(s.ProjectID, "own-tool")

	resp := s.Client.PATCH("/api/admin/mcp-servers/"+serverAID+"/tools/"+toolAID,
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{"enabled": false}))

	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"own-project tool PATCH must be 200, got %d: %s", resp.StatusCode, resp.String())
	s.Require().False(s.readToolEnabled(toolAID),
		"own-project tool must be disabled after PATCH")
}

// --- UpdateBuiltinTool (PATCH /api/admin/builtin-tools/:toolId) ---

func (s *MCPRegistryToolOwnershipSuite) TestUpdateBuiltinForeignToolRefused404() {
	resp := s.Client.PATCH("/api/admin/builtin-tools/"+s.toolBID,
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{"enabled": false}))
	victimEnabled := s.readToolEnabled(s.toolBID)

	s.Require().Equal(http.StatusNotFound, resp.StatusCode,
		"foreign builtin-tool PATCH must be 404, got %d (victim enabled=%v): %s", resp.StatusCode, victimEnabled, resp.String())
	s.Require().True(victimEnabled,
		"victim builtin tool must be unchanged (enabled=true), got enabled=%v", victimEnabled)
}

func (s *MCPRegistryToolOwnershipSuite) TestUpdateBuiltinOwnToolOK() {
	_, toolAID := s.seedServerAndTool(s.ProjectID, "own-tool")

	resp := s.Client.PATCH("/api/admin/builtin-tools/"+toolAID,
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{"enabled": false}))

	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"own-project builtin-tool PATCH must be 200, got %d: %s", resp.StatusCode, resp.String())
	s.Require().False(s.readToolEnabled(toolAID),
		"own-project builtin tool must be disabled after PATCH")
}

// --- SyncServerTools (MCP manual-sync branch) ---

// TestSyncServerToolsForeignServerRefused proves the manual-sync branch of
// SyncServerTools is project-scoped (issue #978): a member of A targeting
// project B's server_id is refused and B's tool rows are untouched.
func (s *MCPRegistryToolOwnershipSuite) TestSyncServerToolsForeignServerRefused() {
	// Two more victim rows on project B's server (plus the existing "foreign-tool").
	s.seedToolOnServer(s.serverBID, "victim-1")
	s.seedToolOnServer(s.serverBID, "victim-2")

	svc := mcpregistry.NewService(mcpregistry.NewRepository(s.DB()), nil, nil, nil, slog.Default())

	err := svc.SyncServerTools(s.Ctx, s.ProjectID, s.serverBID, []mcpregistry.DiscoveredTool{{Name: "attacker-tool"}})

	s.Require().Error(err, "foreign server sync must be refused")
	s.Require().True(errors.Is(err, mcpregistry.ErrServerNotFound),
		"foreign server sync must return ErrServerNotFound, got: %v", err)

	names := s.listToolNames(s.serverBID)
	s.Require().ElementsMatch([]string{"foreign-tool", "victim-1", "victim-2"}, names,
		"victim tool rows must be unchanged, got: %v", names)
}

// TestSyncServerToolsOwnProjectOK proves the legitimate manual-sync path still
// works for the caller's own server.
func (s *MCPRegistryToolOwnershipSuite) TestSyncServerToolsOwnProjectOK() {
	serverAID, _ := s.seedServerAndTool(s.ProjectID, "own-tool")

	svc := mcpregistry.NewService(mcpregistry.NewRepository(s.DB()), nil, nil, nil, slog.Default())

	err := svc.SyncServerTools(s.Ctx, s.ProjectID, serverAID, []mcpregistry.DiscoveredTool{
		{Name: "own-tool"},
		{Name: "new-tool"},
	})
	s.Require().NoError(err, "own-project sync must succeed: %v", err)

	names := s.listToolNames(serverAID)
	s.Require().ElementsMatch([]string{"own-tool", "new-tool"}, names,
		"own-project sync must upsert the provided tools, got: %v", names)
}
