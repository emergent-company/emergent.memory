package orgs_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// OrgMembershipSuite exercises the org-scoped routes of domain/orgs through the
// full in-process server (auth middleware + route table + handler) to prove a
// caller's authorization is derived from actual organization membership, not the
// :id path parameter self-satisfying an empty check (issue #851).
//
// The admin user is set up by BaseSuite.SetupTest as an org_admin member of
// s.OrgID (org A). A second org B is created on demand with no membership for
// the admin user.
type OrgMembershipSuite struct {
	testutil.BaseSuite
}

func TestOrgMembershipSuite(t *testing.T) {
	suite.Run(t, new(OrgMembershipSuite))
}

func (s *OrgMembershipSuite) SetupSuite() {
	s.SetDBSuffix("org_membership")
	s.BaseSuite.SetupSuite()
}

// newForeignOrg creates an organization the admin user is NOT a member of.
func (s *OrgMembershipSuite) newForeignOrg() string {
	orgB := uuid.New().String()
	s.Require().NoError(testutil.CreateTestOrganization(s.Ctx, s.DB(), orgB, "Org B"))
	return orgB
}

// newOwnedOrg creates an organization the admin user IS a member of (org_admin),
// so the "member allowed" cases have a disposable target that can be mutated or
// deleted without disturbing the shared s.OrgID fixture.
func (s *OrgMembershipSuite) newOwnedOrg() string {
	orgID := uuid.New().String()
	s.Require().NoError(testutil.CreateTestOrganization(s.Ctx, s.DB(), orgID, "Owned Org"))
	s.Require().NoError(testutil.CreateTestOrgMembership(s.Ctx, s.DB(), orgID, testutil.AdminUser.ID, "org_admin"))
	return orgID
}

// TestCrossOrgRoutesForbidden is the primary reproducer: a caller who is a member
// of org A must NOT be able to read or mutate org B through the org-scoped
// routes. Before the fix every route below returned 200/201 because the handlers
// trusted the :id path parameter with no membership assertion.
func (s *OrgMembershipSuite) TestCrossOrgRoutesForbidden() {
	orgB := s.newForeignOrg()

	checks := []struct {
		method string
		path   string
		opts   []testutil.RequestOption
	}{
		{method: http.MethodGet, path: "/api/orgs/" + orgB},
		{method: http.MethodGet, path: "/api/orgs/" + orgB + "/members"},
		{method: http.MethodPatch, path: "/api/orgs/" + orgB, opts: []testutil.RequestOption{testutil.WithJSONBody(map[string]any{"name": "stolen"})}},
		{method: http.MethodDelete, path: "/api/orgs/" + orgB},
		// Tool-settings routes are org-scoped too and must stay closed.
		{method: http.MethodGet, path: "/api/admin/orgs/" + orgB + "/tool-settings"},
		{method: http.MethodPut, path: "/api/admin/orgs/" + orgB + "/tool-settings/web", opts: []testutil.RequestOption{testutil.WithJSONBody(map[string]any{"enabled": true})}},
		{method: http.MethodDelete, path: "/api/admin/orgs/" + orgB + "/tool-settings/web"},
	}

	for _, tc := range checks {
		resp := s.Client.Request(tc.method, tc.path, append(tc.opts, testutil.WithAuth("e2e-test-user"))...)
		s.Require().Equal(http.StatusForbidden, resp.StatusCode,
			"%s %s must be forbidden, got %d: %s", tc.method, tc.path, resp.StatusCode, resp.String())
	}
}

// TestOwnOrgRoutesAllowed proves the membership derivation still admits the
// caller's own org: the admin user is a member of s.OrgID, so the same-shaped
// requests against their own org must succeed.
func (s *OrgMembershipSuite) TestOwnOrgRoutesAllowed() {
	resp := s.Client.GET("/api/orgs/"+s.OrgID, testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"own-org get must succeed, got %d: %s", resp.StatusCode, resp.String())

	resp = s.Client.GET("/api/orgs/"+s.OrgID+"/members", testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"own-org members must succeed, got %d: %s", resp.StatusCode, resp.String())

	resp = s.Client.PATCH("/api/orgs/"+s.OrgID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{"name": "Renamed Org"}),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"own-org update must succeed, got %d: %s", resp.StatusCode, resp.String())

	// Disposable owned org so the delete case has a target that can be removed.
	owned := s.newOwnedOrg()
	resp = s.Client.DELETE("/api/orgs/"+owned, testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"own-org delete must succeed, got %d: %s", resp.StatusCode, resp.String())
}

// TestNoUserUnauthorized proves the routes fail closed with 401 when no
// authenticated user is present.
func (s *OrgMembershipSuite) TestNoUserUnauthorized() {
	orgB := s.newForeignOrg()

	checks := []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/orgs/" + orgB},
		{method: http.MethodGet, path: "/api/orgs/" + orgB + "/members"},
		{method: http.MethodPatch, path: "/api/orgs/" + orgB},
		{method: http.MethodDelete, path: "/api/orgs/" + orgB},
	}

	for _, tc := range checks {
		resp := s.Client.Request(tc.method, tc.path)
		s.Require().Equal(http.StatusUnauthorized, resp.StatusCode,
			"%s %s without auth must be 401, got %d: %s", tc.method, tc.path, resp.StatusCode, resp.String())
	}
}
