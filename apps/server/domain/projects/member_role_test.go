package projects_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// MemberRoleSuite covers the PATCH /api/projects/:id/members/:userId role-change
// endpoint across the caller-authority matrix and the role-validation guards
// (invalid role, unknown member, last-admin protection).
type MemberRoleSuite struct {
	testutil.BaseSuite
}

func TestMemberRoleSuite(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "Skipping database integration test in short mode")
	}
	suite.Run(t, new(MemberRoleSuite))
}

func (s *MemberRoleSuite) SetupSuite() {
	s.SetDBSuffix("project_member_role")
	s.BaseSuite.SetupSuite()
}

// grant adds org and/or project memberships for a user to the default
// org/project (per-test, rolled back with the test transaction).
func (s *MemberRoleSuite) grant(userID, orgRole, projectRole string) {
	if orgRole != "" {
		s.Require().NoError(testutil.CreateTestOrgMembership(s.Ctx, s.DB(), s.OrgID, userID, orgRole))
	}
	if projectRole != "" {
		s.Require().NoError(testutil.CreateTestProjectMembership(s.Ctx, s.DB(), s.ProjectID, userID, projectRole))
	}
}

func (s *MemberRoleSuite) roleBody(role string) testutil.RequestOption {
	return testutil.WithJSONBody(map[string]any{"role": role})
}

func (s *MemberRoleSuite) memberPath(userID string) string {
	return "/api/projects/" + s.ProjectID + "/members/" + userID
}

// TestUpdateMemberRole_Authority exercises the authority matrix: a project_admin
// or owning-org org_admin may change a role; project_user/project_viewer/foreign
// callers are refused.
func (s *MemberRoleSuite) TestUpdateMemberRole_Authority() {
	// Victim: a project_user membership to change.
	s.grant(testutil.AllScopesUser.ID, "", "project_user")
	victimPath := s.memberPath(testutil.AllScopesUser.ID)

	s.Run("unauthenticated", func() {
		resp := s.Client.PATCH(victimPath, s.roleBody("project_viewer"))
		s.Equal(401, resp.StatusCode, resp.String())
	})
	s.Run("foreign", func() {
		resp := s.Client.PATCH(victimPath, testutil.WithAuth(noScope), s.roleBody("project_viewer"))
		s.Equal(404, resp.StatusCode, resp.String())
	})
	s.Run("project user forbidden", func() {
		s.grant(testutil.WithScopeUser.ID, "member", "project_user")
		resp := s.Client.PATCH(victimPath, testutil.WithAuth(withScope), s.roleBody("project_viewer"))
		s.Equal(403, resp.StatusCode, resp.String())
	})
	s.Run("project viewer forbidden", func() {
		s.grant(testutil.WithScopeUser.ID, "member", "project_viewer")
		resp := s.Client.PATCH(victimPath, testutil.WithAuth(withScope), s.roleBody("project_viewer"))
		s.Equal(403, resp.StatusCode, resp.String())
	})
	s.Run("project admin", func() {
		// AdminUser is project_admin of the default project.
		resp := s.Client.PATCH(victimPath, testutil.WithAuth(adminToken), s.roleBody("project_viewer"))
		s.Equal(200, resp.StatusCode, resp.String())
	})
	s.Run("org admin allowed", func() {
		// An owning-org org_admin WITHOUT a project membership may still change roles.
		s.grant(testutil.AllScopesUser.ID, "org_admin", "")
		s.grant(testutil.WithScopeUser.ID, "", "project_user")
		resp := s.Client.PATCH(s.memberPath(testutil.WithScopeUser.ID), testutil.WithAuth(allScopes), s.roleBody("project_viewer"))
		s.Equal(200, resp.StatusCode, resp.String())
	})
}

// TestUpdateMemberRole_LastAdmin protects the final project_admin from demotion.
func (s *MemberRoleSuite) TestUpdateMemberRole_LastAdmin() {
	// AdminUser is the only project_admin; demoting them must be refused.
	resp := s.Client.PATCH(s.memberPath(testutil.AdminUser.ID), testutil.WithAuth(adminToken), s.roleBody("project_user"))
	s.Equal(403, resp.StatusCode, resp.String())
}

// TestUpdateMemberRole_DemotionRevokesButPromotionAllowed verifies a non-last
// admin can be demoted once a second admin exists (downgrade path), and that
// demotion is distinct from the last-admin refusal.
func (s *MemberRoleSuite) TestUpdateMemberRole_DemoteNonLastAdmin() {
	// Add a second admin; AdminUser remains admin, so demoting the second one is safe.
	s.grant(testutil.AllScopesUser.ID, "", "project_admin")

	resp := s.Client.PATCH(s.memberPath(testutil.AllScopesUser.ID), testutil.WithAuth(adminToken), s.roleBody("project_user"))
	s.Equal(200, resp.StatusCode, resp.String())
}

// TestUpdateMemberRole_InvalidRole rejects unknown role values.
func (s *MemberRoleSuite) TestUpdateMemberRole_InvalidRole() {
	s.grant(testutil.AllScopesUser.ID, "", "project_user")

	resp := s.Client.PATCH(s.memberPath(testutil.AllScopesUser.ID), testutil.WithAuth(adminToken), s.roleBody("super_admin"))
	s.Equal(400, resp.StatusCode, resp.String())
}

// TestUpdateMemberRole_UnknownMember rejects a membership that does not exist.
func (s *MemberRoleSuite) TestUpdateMemberRole_UnknownMember() {
	resp := s.Client.PATCH(s.memberPath(uuid.New().String()), testutil.WithAuth(adminToken), s.roleBody("project_user"))
	s.Equal(404, resp.StatusCode, resp.String())
}
