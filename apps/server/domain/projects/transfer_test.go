package projects_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// ProjectTransferSuite tests the POST /api/projects/{id}/transfer endpoint
// (reparenting a project to another organization).
type ProjectTransferSuite struct {
	testutil.BaseSuite
}

func TestProjectTransferSuite(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "Skipping database integration test in short mode")
	}
	suite.Run(t, new(ProjectTransferSuite))
}

func (s *ProjectTransferSuite) SetupSuite() {
	s.SetDBSuffix("project_transfer")
	s.BaseSuite.SetupSuite()
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (s *ProjectTransferSuite) transfer(projectID, destOrgID, token string) (int, string) {
	resp := s.Client.POST("/api/projects/"+projectID+"/transfer",
		testutil.WithAuth(token),
		testutil.WithJSONBody(map[string]any{"orgId": destOrgID}),
	)
	return resp.StatusCode, resp.String()
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func (s *ProjectTransferSuite) TestTransfer_Success() {
	destOrgID := uuid.New().String()
	s.Require().NoError(testutil.CreateTestOrganization(s.Ctx, s.DB(), destOrgID, "Dest Org"))
	s.Require().NoError(testutil.CreateTestOrgMembership(s.Ctx, s.DB(), destOrgID, testutil.AdminUser.ID, "org_admin"))

	resp := s.Client.POST("/api/projects/"+s.ProjectID+"/transfer",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{"orgId": destOrgID}),
	)
	s.Require().Equal(200, resp.StatusCode, "transfer failed: %s", resp.String())

	var project map[string]any
	s.Require().NoError(resp.JSON(&project))
	s.Equal(destOrgID, project["orgId"])
	// Identity is preserved — only the org association changes.
	s.Equal(s.ProjectID, project["id"])
	s.Equal("Test Project", project["name"])
}

func (s *ProjectTransferSuite) TestTransfer_DestEqualsSource() {
	status, body := s.transfer(s.ProjectID, s.OrgID, "e2e-test-user")
	s.Equal(400, status, body)
}

func (s *ProjectTransferSuite) TestTransfer_ProjectNotFound() {
	status, body := s.transfer(uuid.New().String(), s.OrgID, "e2e-test-user")
	s.Equal(404, status, body)
}

func (s *ProjectTransferSuite) TestTransfer_ForbiddenNotOrgAdminOfSource() {
	destOrgID := uuid.New().String()
	s.Require().NoError(testutil.CreateTestOrganization(s.Ctx, s.DB(), destOrgID, "Dest Org"))
	s.Require().NoError(testutil.CreateTestOrgMembership(s.Ctx, s.DB(), destOrgID, testutil.AllScopesUser.ID, "org_admin"))
	// AllScopesUser is only a plain "member" of the source org, not org_admin.
	s.Require().NoError(testutil.CreateTestOrgMembership(s.Ctx, s.DB(), s.OrgID, testutil.AllScopesUser.ID, "member"))

	status, body := s.transfer(s.ProjectID, destOrgID, "all-scopes")
	s.Equal(403, status, body)
}

func (s *ProjectTransferSuite) TestTransfer_ForbiddenNotMemberOfDestination() {
	destOrgID := uuid.New().String()
	s.Require().NoError(testutil.CreateTestOrganization(s.Ctx, s.DB(), destOrgID, "Dest Org"))
	// AdminUser (e2e-test-user) is org_admin of the source org but NOT a member
	// of the destination org.

	status, body := s.transfer(s.ProjectID, destOrgID, "e2e-test-user")
	s.Equal(403, status, body)
}
