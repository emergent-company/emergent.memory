package provider_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// ProjectOwnershipSuite exercises the project-scoped provider routes through the
// full in-process server (auth middleware + route table + handler) to prove that
// a caller's authorization is derived from real organization membership, not from
// the :projectId path parameter self-satisfying the ownership check (issue #850).
//
// The admin user is set up by BaseSuite.SetupTest as an org_admin member of
// s.OrgID (org A). A second org B is created on demand with no membership for
// the admin user.
type ProjectOwnershipSuite struct {
	testutil.BaseSuite
}

func TestProjectOwnershipSuite(t *testing.T) {
	suite.Run(t, new(ProjectOwnershipSuite))
}

func (s *ProjectOwnershipSuite) SetupSuite() {
	s.SetDBSuffix("project_ownership")
	s.BaseSuite.SetupSuite()
}

// newForeignProject creates an organization the admin user is NOT a member of,
// plus a project owned by it, and returns the foreign project ID.
func (s *ProjectOwnershipSuite) newForeignProject() string {
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

// TestCrossProjectProviderReadForbidden is the primary read reproducer: a caller
// who is a member of org A (and has no org context in the request) must NOT be
// able to read org B's provider config. Before the fix this returned 200 because
// the handler injected the :projectId path parameter's org into the empty context
// and compared it against itself.
func (s *ProjectOwnershipSuite) TestCrossProjectProviderReadForbidden() {
	projectB := s.newForeignProject()

	for _, path := range []string{
		"/api/v1/projects/" + projectB + "/providers",
		"/api/v1/projects/" + projectB + "/providers/deepseek",
		"/api/v1/projects/" + projectB + "/usage",
		"/api/v1/projects/" + projectB + "/usage/timeseries",
		"/api/v1/projects/" + projectB + "/pricing-overrides",
	} {
		resp := s.Client.GET(path, testutil.WithAuth("e2e-test-user"))
		s.Require().Equal(http.StatusForbidden, resp.StatusCode,
			"%s must be forbidden, got %d: %s", path, resp.StatusCode, resp.String())
	}
}

// TestCrossProjectProviderWriteDeleteForbidden is the higher-severity reproducer:
// write and delete routes against a foreign project must be rejected with 403,
// not mutate org B's configuration.
func (s *ProjectOwnershipSuite) TestCrossProjectProviderWriteDeleteForbidden() {
	projectB := s.newForeignProject()

	s.Require().Equal(http.StatusForbidden,
		s.Client.PUT("/api/v1/projects/"+projectB+"/providers/deepseek",
			testutil.WithAuth("e2e-test-user"),
			testutil.WithJSONBody(map[string]any{"apiKey": "sk-attacker"})).StatusCode,
		"cross-project provider PUT must be forbidden")

	s.Require().Equal(http.StatusForbidden,
		s.Client.DELETE("/api/v1/projects/"+projectB+"/providers/deepseek",
			testutil.WithAuth("e2e-test-user")).StatusCode,
		"cross-project provider DELETE must be forbidden")

	s.Require().Equal(http.StatusForbidden,
		s.Client.PUT("/api/v1/projects/"+projectB+"/pricing-overrides",
			testutil.WithAuth("e2e-test-user"),
			testutil.WithJSONBody(map[string]any{
				"provider": "deepseek", "model": "deepseek-v4-pro", "outputPrice": 1.0,
			})).StatusCode,
		"cross-project pricing-overrides PUT must be forbidden")

	s.Require().Equal(http.StatusForbidden,
		s.Client.DELETE("/api/v1/projects/"+projectB+"/pricing-overrides/deepseek/deepseek-v4-pro",
			testutil.WithAuth("e2e-test-user")).StatusCode,
		"cross-project pricing-overrides DELETE must be forbidden")

	s.Require().Equal(http.StatusForbidden,
		s.Client.POST("/api/v1/projects/"+projectB+"/providers/deepseek/test",
			testutil.WithAuth("e2e-test-user")).StatusCode,
		"cross-project provider test must be forbidden")
}

// TestOwnProjectProviderAccessOK proves the membership derivation still admits the
// caller's own project: the admin user is a member of s.OrgID, so read and delete
// against their own project must succeed.
func (s *ProjectOwnershipSuite) TestOwnProjectProviderAccessOK() {
	resp := s.Client.GET("/api/v1/projects/"+s.ProjectID+"/providers",
		testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"own-project provider list must succeed, got %d: %s", resp.StatusCode, resp.String())

	resp = s.Client.GET("/api/v1/projects/"+s.ProjectID+"/usage",
		testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"own-project usage must succeed, got %d: %s", resp.StatusCode, resp.String())

	resp = s.Client.DELETE("/api/v1/projects/"+s.ProjectID+"/providers/deepseek",
		testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"own-project provider delete must succeed, got %d: %s", resp.StatusCode, resp.String())
}
