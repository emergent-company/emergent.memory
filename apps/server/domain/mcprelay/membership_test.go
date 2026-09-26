package mcprelay_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// MCPRelayMembershipSuite proves a caller's relay access is derived from real
// organization membership, not from the client-supplied X-Project-ID header /
// token binding (issue #913). The relay resolves the project via
// auth.GetProjectID, which the shared membership pair must gate.
type MCPRelayMembershipSuite struct {
	testutil.BaseSuite
}

func TestMCPRelayMembershipSuite(t *testing.T) {
	suite.Run(t, new(MCPRelayMembershipSuite))
}

func (s *MCPRelayMembershipSuite) SetupSuite() {
	s.SetDBSuffix("mcprelay_membership")
	s.BaseSuite.SetupSuite()
}

func (s *MCPRelayMembershipSuite) newForeignProject() string {
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

func (s *MCPRelayMembershipSuite) TestCrossProjectSessionsForbidden() {
	projectB := s.newForeignProject()

	resp := s.Client.GET("/api/mcp-relay/sessions",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(projectB))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project relay sessions must be forbidden, got %d: %s", resp.StatusCode, resp.String())
}

func (s *MCPRelayMembershipSuite) TestOwnProjectSessionsOK() {
	resp := s.Client.GET("/api/mcp-relay/sessions",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"own-project relay sessions must succeed, got %d: %s", resp.StatusCode, resp.String())
}
