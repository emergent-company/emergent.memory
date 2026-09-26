package branches_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// BranchesMembershipSuite proves a caller's branch access is derived from real
// organization membership, not from the client-supplied ?project_id query param
// or request-body project_id field (issue #913). The admin user is an org_admin
// member of s.OrgID (org A); a foreign org B is created with no membership, and
// the caller addresses org B's project via the actual source the handlers read.
type BranchesMembershipSuite struct {
	testutil.BaseSuite
}

func TestBranchesMembershipSuite(t *testing.T) {
	suite.Run(t, new(BranchesMembershipSuite))
}

func (s *BranchesMembershipSuite) SetupSuite() {
	s.SetDBSuffix("branches_membership")
	s.BaseSuite.SetupSuite()
}

func (s *BranchesMembershipSuite) newForeignProject() string {
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

// TestCrossProjectBranchReadForbidden is the read reproducer: a member of org A
// addressing org B's project via ?project_id must not list branches there.
func (s *BranchesMembershipSuite) TestCrossProjectBranchReadForbidden() {
	projectB := s.newForeignProject()

	resp := s.Client.GET("/api/graph/branches?project_id="+projectB,
		testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project branch list must be forbidden, got %d: %s", resp.StatusCode, resp.String())

	resp = s.Client.GET("/api/graph/branches/"+uuid.New().String()+"?project_id="+projectB,
		testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project branch get must be forbidden, got %d: %s", resp.StatusCode, resp.String())
}

// TestCrossProjectBranchWriteForbidden is the higher-severity reproducer: a
// member of org A must not create a branch in org B's project via the body
// project_id field.
func (s *BranchesMembershipSuite) TestCrossProjectBranchWriteForbidden() {
	projectB := s.newForeignProject()

	resp := s.Client.POST("/api/graph/branches",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{"project_id": projectB, "name": "stolen"}))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project branch create must be forbidden, got %d: %s", resp.StatusCode, resp.String())
}

// TestOwnProjectBranchOK proves the membership derivation still admits the
// caller's own project for both read and write.
func (s *BranchesMembershipSuite) TestOwnProjectBranchOK() {
	resp := s.Client.GET("/api/graph/branches?project_id="+s.ProjectID,
		testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"own-project branch list must succeed, got %d: %s", resp.StatusCode, resp.String())

	resp = s.Client.POST("/api/graph/branches",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{"project_id": s.ProjectID, "name": "feature-x"}))
	s.Require().Equal(http.StatusCreated, resp.StatusCode,
		"own-project branch create must succeed, got %d: %s", resp.StatusCode, resp.String())
}
