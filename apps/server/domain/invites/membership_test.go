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
// authenticated context, not from a client-supplied project/org id (issues
// #926 and #960). The invites routes register via module.go, which is why
// previous domain/*/routes.go sweeps never saw them.
//
//   - ListByProject reads the project from the :projectId path param, so the
//     shared RequireProjectTokenScope → RequireProjectMember pair applies.
//   - Create reads the project and org from the request body, which the pair
//     cannot inspect, so the handler authorizes both: the project via
//     auth.AuthorizeProject, the org via server-side binding/membership.
//   - Delete (revoke) resolves the invite server-side and requires the caller
//     to be a member of the invite's organization.
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

// inviteCountForOrg returns the number of kb.invites rows for an organization.
func (s *InvitesMembershipSuite) inviteCountForOrg(orgID string) int {
	var count int
	err := s.DB().NewRaw(`SELECT COUNT(*) FROM kb.invites WHERE organization_id = ?`, orgID).Scan(s.Ctx, &count)
	s.Require().NoError(err)
	return count
}

// createInviteID creates an org-level invite in the admin's own org and returns
// its ID.
func (s *InvitesMembershipSuite) createInviteID(orgID string) string {
	resp := s.Client.POST("/api/invites",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"orgId": orgID,
			"email": "invitee@example.com",
			"role":  "project_user",
		}))
	s.Require().Equal(http.StatusCreated, resp.StatusCode,
		"invite create must succeed, got %d: %s", resp.StatusCode, resp.String())
	var inv struct {
		ID string `json:"id"`
	}
	s.Require().NoError(resp.JSON(&inv))
	s.Require().NotEmpty(inv.ID)
	return inv.ID
}

// ─── ListByProject (path-scoped pair) ────────────────────────────────────────

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

// ─── Create (body-sourced project/org) ───────────────────────────────────────

// TestCreateCrossProjectForbidden is the fail-first reproducer for the
// body-sourced project: a member of org A must not create an invitation for
// org B's project by supplying its projectId in the request body.
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

// TestCreateOrgBindingMismatchRejected is the fail-first reproducer for the
// cross-tenant escalation (issue #960): a member supplies their own project but
// a foreign orgId. The handler binds the body orgId to the project's
// server-resolved owning org and rejects the contradiction with 400, so no
// invite row is ever written to the foreign org.
func (s *InvitesMembershipSuite) TestCreateOrgBindingMismatchRejected() {
	orgB := uuid.New().String()
	s.Require().NoError(testutil.CreateTestOrganization(s.Ctx, s.DB(), orgB, "Org B"))

	resp := s.Client.POST("/api/invites",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"orgId":     orgB,        // foreign org
			"projectId": s.ProjectID, // own project
			"email":     "invitee@example.com",
			"role":      "org_admin",
		}))
	s.Require().Equal(http.StatusBadRequest, resp.StatusCode,
		"contradictory orgId/projectId must be 400, got %d: %s", resp.StatusCode, resp.String())
	s.Require().Equal(0, s.inviteCountForOrg(orgB),
		"foreign org must have zero invite rows after a rejected create")
}

// TestCreateOrgOnlyNonMemberForbidden proves the org-only path is closed: a
// caller who is not a member of the target org cannot create an org-level
// invite (issue #960).
func (s *InvitesMembershipSuite) TestCreateOrgOnlyNonMemberForbidden() {
	resp := s.Client.POST("/api/invites",
		testutil.WithAuth("no-scope"),
		testutil.WithJSONBody(map[string]any{
			"orgId": s.OrgID,
			"email": "invitee@example.com",
			"role":  "org_admin",
		}))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"org-only invite by a non-member must be 403, got %d: %s", resp.StatusCode, resp.String())
	s.Require().Equal(0, s.inviteCountForOrg(s.OrgID),
		"no invite row may be created for a non-member org-only create")
}

// TestCreateOrgOnlyMemberOK proves a member creating an org-level invite is
// still admitted.
func (s *InvitesMembershipSuite) TestCreateOrgOnlyMemberOK() {
	resp := s.Client.POST("/api/invites",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"orgId": s.OrgID,
			"email": "invitee@example.com",
			"role":  "org_admin",
		}))
	s.Require().Equal(http.StatusCreated, resp.StatusCode,
		"org-only invite by a member must succeed, got %d: %s", resp.StatusCode, resp.String())
}

// ─── Create role-grant authorization (issue #967) ────────────────────────────

// seedMemberInOrg adds a plain member (not org_admin) to the suite org so a test
// can authenticate as that member via their token ("read-only" → ReadOnlyUser).
func (s *InvitesMembershipSuite) seedMemberInOrg() {
	s.Require().NoError(testutil.CreateTestOrgMembership(
		s.Ctx, s.DB(), s.OrgID, testutil.ReadOnlyUser.ID, "member"))
}

// TestCreateOrgAdminByMemberForbidden is the fail-first reproducer for the
// self-escalation (issue #967): a plain member creating an org_admin invitation
// for their own org must be refused with 403 and must write no invite row.
func (s *InvitesMembershipSuite) TestCreateOrgAdminByMemberForbidden() {
	s.seedMemberInOrg()

	resp := s.Client.POST("/api/invites",
		testutil.WithAuth("read-only"),
		testutil.WithJSONBody(map[string]any{
			"orgId": s.OrgID,
			"email": "invitee@example.com",
			"role":  "org_admin",
		}))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"member creating an org_admin invite must be 403, got %d: %s", resp.StatusCode, resp.String())
	s.Require().Equal(0, s.inviteCountForOrg(s.OrgID),
		"no invite row may be created when a member attempts an org_admin invite")
}

// TestCreateOrgAdminByOrgAdminOK proves an org_admin can still create an
// org_admin invitation (the gate must not over-restrict legitimate admins).
func (s *InvitesMembershipSuite) TestCreateOrgAdminByOrgAdminOK() {
	resp := s.Client.POST("/api/invites",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"orgId": s.OrgID,
			"email": "invitee@example.com",
			"role":  "org_admin",
		}))
	s.Require().Equal(http.StatusCreated, resp.StatusCode,
		"org_admin creating an org_admin invite must succeed, got %d: %s", resp.StatusCode, resp.String())
}

// TestCreateMemberRoleByMemberOK proves a plain member can still create an
// ordinary member-granting (project_*) invitation — the gate must not
// over-restrict non-org_admin roles.
func (s *InvitesMembershipSuite) TestCreateMemberRoleByMemberOK() {
	s.seedMemberInOrg()

	resp := s.Client.POST("/api/invites",
		testutil.WithAuth("read-only"),
		testutil.WithJSONBody(map[string]any{
			"orgId": s.OrgID,
			"email": "invitee@example.com",
			"role":  "project_user",
		}))
	s.Require().Equal(http.StatusCreated, resp.StatusCode,
		"member creating a project_user invite must succeed, got %d: %s", resp.StatusCode, resp.String())
}

// ─── Create project-scoped role validation (issue #979) ──────────────────────

// TestCreateProjectScopedOrgAdminRejected proves a project-scoped invitation
// with role org_admin is refused with 400 and writes no invite row. org_admin
// is an organization-level grant with no meaning in project scope, and
// accepting such an invite would write an out-of-vocabulary "org_admin" value
// into kb.project_memberships.role (issue #979). The admin user is an org_admin
// of the suite org, so this exercises the project-scope rejection specifically,
// not the role-grant authority gate.
func (s *InvitesMembershipSuite) TestCreateProjectScopedOrgAdminRejected() {
	resp := s.Client.POST("/api/invites",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"orgId":     s.OrgID,
			"projectId": s.ProjectID,
			"email":     "invitee@example.com",
			"role":      "org_admin",
		}))
	s.Require().Equal(http.StatusBadRequest, resp.StatusCode,
		"project-scoped org_admin invite must be 400, got %d: %s", resp.StatusCode, resp.String())
	s.Require().Equal(0, s.inviteCountForOrg(s.OrgID),
		"no invite row may be created for a project-scoped org_admin invite")
}

// ─── Revoke (DELETE /api/invites/:id) ────────────────────────────────────────

// TestRevokeNonMemberForbidden proves a non-member (bare user) cannot revoke an
// invite and receives 403 — revoke is an org_admin action, not a membership one.
func (s *InvitesMembershipSuite) TestRevokeNonMemberForbidden() {
	inviteID := s.createInviteID(s.OrgID)

	resp := s.Client.DELETE("/api/invites/"+inviteID, testutil.WithAuth("no-scope"))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"non-member revoke must be 403, got %d: %s", resp.StatusCode, resp.String())
}

// TestRevokePlainMemberForbidden is the fail-first reproducer for the tier gap:
// a plain member (role "member") of the invite's org must NOT revoke an invite
// — the tier-correct bar is org_admin, not mere membership.
func (s *InvitesMembershipSuite) TestRevokePlainMemberForbidden() {
	s.seedMemberInOrg()
	inviteID := s.createInviteID(s.OrgID)

	resp := s.Client.DELETE("/api/invites/"+inviteID, testutil.WithAuth("read-only"))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"plain member revoke must be 403, got %d: %s", resp.StatusCode, resp.String())
}

// TestRevokeOrgAdminOK proves an org_admin revoking their own org's invite succeeds.
func (s *InvitesMembershipSuite) TestRevokeOrgAdminOK() {
	inviteID := s.createInviteID(s.OrgID)

	resp := s.Client.DELETE("/api/invites/"+inviteID, testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusNoContent, resp.StatusCode,
		"org_admin revoke must be 204, got %d: %s", resp.StatusCode, resp.String())
}

// TestRevokeUnknownInviteNotFound proves a non-existent id returns 404.
func (s *InvitesMembershipSuite) TestRevokeUnknownInviteNotFound() {
	resp := s.Client.DELETE("/api/invites/"+uuid.New().String(), testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusNotFound, resp.StatusCode,
		"revoke of a non-existent invite must be 404, got %d: %s", resp.StatusCode, resp.String())
}
