package agents_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// LegacyAgentDefinitionsMembershipSuite exercises the header-scoped
// /api/agent-definitions (legacy flat alias) group through the full in-process
// server. The admin user is an org_admin member of s.OrgID (org A); a foreign
// org B is created on demand with no membership for the admin user. Before the
// fix (issue #868, the #864 class) this group scoped by the client-supplied
// X-Project-ID header with no membership check, so an org-A member could list
// org B's agent definitions.
type LegacyAgentDefinitionsMembershipSuite struct {
	testutil.BaseSuite
}

func TestLegacyAgentDefinitionsMembershipSuite(t *testing.T) {
	suite.Run(t, new(LegacyAgentDefinitionsMembershipSuite))
}

func (s *LegacyAgentDefinitionsMembershipSuite) SetupSuite() {
	s.SetDBSuffix("legacy_agent_defs_membership")
	s.BaseSuite.SetupSuite()
}

func (s *LegacyAgentDefinitionsMembershipSuite) newForeignProject() string {
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

func (s *LegacyAgentDefinitionsMembershipSuite) TestCrossProjectListForbidden() {
	projectB := s.newForeignProject()
	resp := s.Client.GET("/api/agent-definitions",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(projectB))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project agent-definitions list must be forbidden, got %d: %s", resp.StatusCode, resp.String())
}

func (s *LegacyAgentDefinitionsMembershipSuite) TestOwnProjectListOK() {
	resp := s.Client.GET("/api/agent-definitions",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"own-project agent-definitions list must succeed, got %d: %s", resp.StatusCode, resp.String())
}

func (s *LegacyAgentDefinitionsMembershipSuite) TestNoUserUnauthorized() {
	resp := s.Client.GET("/api/agent-definitions", testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusUnauthorized, resp.StatusCode,
		"unauthenticated agent-definitions list must be 401, got %d: %s", resp.StatusCode, resp.String())
}

func (s *LegacyAgentDefinitionsMembershipSuite) TestTokenProjectBindingForbidden() {
	projectB := s.newForeignProject()
	token := "emt_868_legacy_defs_binding"
	s.Require().NoError(testutil.CreateTestAPIToken(s.Ctx, s.DB(),
		testutil.AdminUser.ID, token, []string{"agents:read"}, s.ProjectID))

	resp := s.Client.GET("/api/agent-definitions",
		testutil.WithAuth(token), testutil.WithProjectID(projectB))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"project token addressing a different project must be 403, got %d: %s", resp.StatusCode, resp.String())
}
