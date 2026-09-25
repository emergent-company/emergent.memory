package invites_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// InvitesMembershipSuite exercises the invites routes through the full
// in-process server to prove that a caller's authorization is derived from the
// authenticated context, not from a client-supplied project id (issue #926).
// The invites routes register via module.go, which is why previous
// domain/*/routes.go sweeps never saw them.
//
//   - ListByProject reads the project from the :projectId path param, so the
//     shared RequireProjectTokenScope → RequireProjectMember pair applies.
//   - Create reads the project from the request body, which the pair cannot
//     inspect, so the handler authorizes it via auth.AuthorizeProject.
type InvitesMembershipSuite struct {
	testutil.BaseSuite
}

func TestInvitesMembershipSuite(t *testing.T) {
	suite.Run(t, new(InvitesMembershipSuite))
}

func (s *InvitesMembershipSuite) SetupSuite() {
	s.SetDBSuffix("invites_membership")
	s.BaseSuite.SetupSuite()
}

// newForeignProject creates an organization the admin user is NOT a member of,
// plus a project owned by it, and returns the foreign project ID.
func (s *InvitesMembershipSuite) newForeignProject() string {
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

// TestListByProjectCrossProjectForbidden proves the path-scoped pair denies a
// non-member listing another org's project invitations.
func (s *InvitesMembershipSuite) TestListByProjectCrossProjectForbidden() {
	projectB := s.newForeignProject()

	resp := s.Client.GET("/api/projects/"+projectB+"/invites",
		testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project invite list must be 403, got %d: %s", resp.StatusCode, resp.String())
}

// TestListByProjectOwnProjectOK proves a member listing their own project's
// invitations is still admitted.
func (s *InvitesMembershipSuite) TestListByProjectOwnProjectOK() {
	resp := s.Client.GET("/api/projects/"+s.ProjectID+"/invites",
		testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"own-project invite list must succeed, got %d: %s", resp.StatusCode, resp.String())
}

// TestCreateCrossProjectForbidden is the fail-first reproducer for the
// body-sourced project: a member of org A must not create an invitation for
// org B's project by supplying its projectId in the request body. The shared
// middleware pair cannot see the body, so this is enforced in the handler.
func (s *InvitesMembershipSuite) TestCreateCrossProjectForbidden() {
	projectB := s.newForeignProject()

	resp := s.Client.POST("/api/invites",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"orgId":     s.OrgID,
			"projectId": projectB,
			"email":     "invitee@example.com",
			"role":      "project_user",
		}))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project invite create must be 403, got %d: %s", resp.StatusCode, resp.String())
}

// TestCreateOwnProjectOK proves a member creating an invitation for their own
// project is still admitted.
func (s *InvitesMembershipSuite) TestCreateOwnProjectOK() {
	resp := s.Client.POST("/api/invites",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"orgId":     s.OrgID,
			"projectId": s.ProjectID,
			"email":     "invitee@example.com",
			"role":      "project_user",
		}))
	s.Require().Equal(http.StatusCreated, resp.StatusCode,
		"own-project invite create must succeed, got %d: %s", resp.StatusCode, resp.String())
}
