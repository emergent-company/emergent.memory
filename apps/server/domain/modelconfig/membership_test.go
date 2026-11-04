package modelconfig_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// ModelConfigMembershipSuite exercises the path-scoped
// /api/v1/projects/:projectId/model-config group through the full in-process
// server to prove that a caller's authorization is derived from the shared
// RequireProjectTokenScope → RequireProjectMember pair, not from the
// client-supplied :projectId path param (issue #926). The modelconfig routes
// register via module.go, which is why previous domain/*/routes.go sweeps never
// saw them.
type ModelConfigMembershipSuite struct {
	testutil.BaseSuite
}

func TestModelConfigMembershipSuite(t *testing.T) {
	suite.Run(t, new(ModelConfigMembershipSuite))
}

func (s *ModelConfigMembershipSuite) SetupSuite() {
	s.SetDBSuffix("modelconfig_membership")
	s.BaseSuite.SetupSuite()
}

// newForeignProject creates an organization the admin user is NOT a member of,
// plus a project owned by it, and returns the foreign project ID.
func (s *ModelConfigMembershipSuite) newForeignProject() string {
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

// TestCrossProjectReadForbidden is the fail-first reproducer: a member of org A
// must not read org B's model config via the :projectId path param.
func (s *ModelConfigMembershipSuite) TestCrossProjectReadForbidden() {
	projectB := s.newForeignProject()

	resp := s.Client.GET("/api/v1/projects/"+projectB+"/model-config",
		testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project model-config read must be 403, got %d: %s", resp.StatusCode, resp.String())
}

// TestCrossProjectWriteForbidden proves the write surface fails closed too.
func (s *ModelConfigMembershipSuite) TestCrossProjectWriteForbidden() {
	projectB := s.newForeignProject()

	resp := s.Client.PUT("/api/v1/projects/"+projectB+"/model-config",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"generativeModel": "deepseek/deepseek-v4-flash",
			"embeddingModel":  "google/gemini-embedding-001",
		}))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project model-config write must be 403, got %d: %s", resp.StatusCode, resp.String())
}

// TestOwnProjectReadOK proves a member reading their own project's config is
// still admitted.
func (s *ModelConfigMembershipSuite) TestOwnProjectReadOK() {
	resp := s.Client.GET("/api/v1/projects/"+s.ProjectID+"/model-config",
		testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"own-project model-config read must succeed, got %d: %s", resp.StatusCode, resp.String())
}

// TestOwnProjectWriteOK proves a member writing their own project's config is
// still admitted.
func (s *ModelConfigMembershipSuite) TestOwnProjectWriteOK() {
	resp := s.Client.PUT("/api/v1/projects/"+s.ProjectID+"/model-config",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"generativeModel": "deepseek/deepseek-v4-flash",
			"embeddingModel":  "google/gemini-embedding-001",
		}))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"own-project model-config write must succeed, got %d: %s", resp.StatusCode, resp.String())
}
