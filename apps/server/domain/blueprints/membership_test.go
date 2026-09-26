package blueprints_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// BlueprintsMembershipSuite exercises the header-scoped /api/blueprints group
// through the full in-process server. Every blueprint handler scopes by
// user.ProjectID (read scope = global + own private). Before the fix
// (issue #868, the #864 class) this was the client-supplied X-Project-ID header
// with no membership check, so an org-A member could read org B's private
// blueprints or apply a blueprint to org B's project.
type BlueprintsMembershipSuite struct {
	testutil.BaseSuite
}

func TestBlueprintsMembershipSuite(t *testing.T) {
	suite.Run(t, new(BlueprintsMembershipSuite))
}

func (s *BlueprintsMembershipSuite) SetupSuite() {
	s.SetDBSuffix("blueprints_membership")
	s.BaseSuite.SetupSuite()
}

func (s *BlueprintsMembershipSuite) newForeignProject() string {
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

func (s *BlueprintsMembershipSuite) TestCrossProjectListForbidden() {
	projectB := s.newForeignProject()
	resp := s.Client.GET("/api/blueprints",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(projectB))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project blueprints list must be forbidden, got %d: %s", resp.StatusCode, resp.String())
}

func (s *BlueprintsMembershipSuite) TestOwnProjectListOK() {
	resp := s.Client.GET("/api/blueprints",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"own-project blueprints list must succeed, got %d: %s", resp.StatusCode, resp.String())
}

func (s *BlueprintsMembershipSuite) TestNoUserUnauthorized() {
	resp := s.Client.GET("/api/blueprints", testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusUnauthorized, resp.StatusCode,
		"unauthenticated blueprints list must be 401, got %d: %s", resp.StatusCode, resp.String())
}

func (s *BlueprintsMembershipSuite) TestTokenProjectBindingForbidden() {
	projectB := s.newForeignProject()
	token := "emt_868_blueprints_binding"
	s.Require().NoError(testutil.CreateTestAPIToken(s.Ctx, s.DB(),
		testutil.AdminUser.ID, token, []string{"data:read"}, s.ProjectID))

	resp := s.Client.GET("/api/blueprints",
		testutil.WithAuth(token), testutil.WithProjectID(projectB))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"project token addressing a different project must be 403, got %d: %s", resp.StatusCode, resp.String())
}
