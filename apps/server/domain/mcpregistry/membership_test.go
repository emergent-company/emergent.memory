package mcpregistry_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// MCPRegistryMembershipSuite exercises the header-scoped /api/admin/mcp-servers
// group through the full in-process server. Handlers scope by user.ProjectID
// (the X-Project-ID header) with no membership check before the fix
// (issue #868, the #864 class), so an org-A member could list org B's MCP
// servers.
type MCPRegistryMembershipSuite struct {
	testutil.BaseSuite
}

func TestMCPRegistryMembershipSuite(t *testing.T) {
	suite.Run(t, new(MCPRegistryMembershipSuite))
}

func (s *MCPRegistryMembershipSuite) SetupSuite() {
	s.SetDBSuffix("mcpregistry_membership")
	s.BaseSuite.SetupSuite()
}

func (s *MCPRegistryMembershipSuite) newForeignProject() string {
	orgB := uuid.New().String()
	s.Require().NoError(testutil.CreateTestOrganization(s.Ctx, s.DB(), orgB, "Org B"))
	projectB := uuid.New().String()
	s.Require().NoError(testutil.CreateTestProject(s.Ctx, s.DB(), testutil.TestProject{
		ID:    projectB,
		OrgID: orgB,
		Name:  "Project B",
	}, testutil.AdminUser.ID))
	return projectB
}

func (s *MCPRegistryMembershipSuite) TestCrossProjectListForbidden() {
	projectB := s.newForeignProject()
	resp := s.Client.GET("/api/admin/mcp-servers",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(projectB))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project mcp-servers list must be forbidden, got %d: %s", resp.StatusCode, resp.String())
}

func (s *MCPRegistryMembershipSuite) TestOwnProjectListOK() {
	resp := s.Client.GET("/api/admin/mcp-servers",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"own-project mcp-servers list must succeed, got %d: %s", resp.StatusCode, resp.String())
}

func (s *MCPRegistryMembershipSuite) TestNoUserUnauthorized() {
	resp := s.Client.GET("/api/admin/mcp-servers", testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusUnauthorized, resp.StatusCode,
		"unauthenticated mcp-servers list must be 401, got %d: %s", resp.StatusCode, resp.String())
}

func (s *MCPRegistryMembershipSuite) TestTokenProjectBindingForbidden() {
	projectB := s.newForeignProject()
	token := "emt_868_mcpregistry_binding"
	s.Require().NoError(testutil.CreateTestAPIToken(s.Ctx, s.DB(),
		testutil.AdminUser.ID, token, []string{"admin"}, s.ProjectID))

	resp := s.Client.GET("/api/admin/mcp-servers",
		testutil.WithAuth(token), testutil.WithProjectID(projectB))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"project token addressing a different project must be 403, got %d: %s", resp.StatusCode, resp.String())
}
