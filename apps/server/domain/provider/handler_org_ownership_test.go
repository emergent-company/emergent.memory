package provider_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// OrgOwnershipSuite exercises the org-scoped provider routes through the full
// in-process server (auth middleware + route table + handler) to prove that a
// caller's authorization is derived from actual organization membership, not
// from the :orgId path parameter self-satisfying an empty context (issue #841).
//
// The admin user is set up by BaseSuite.SetupTest as an org_admin member of
// s.OrgID (org A). A second org B is created on demand with no membership for
// the admin user.
type OrgOwnershipSuite struct {
	testutil.BaseSuite
}

func TestOrgOwnershipSuite(t *testing.T) {
	suite.Run(t, new(OrgOwnershipSuite))
}

func (s *OrgOwnershipSuite) SetupSuite() {
	s.SetDBSuffix("org_ownership")
	s.BaseSuite.SetupSuite()
}

// newForeignOrg creates an organization the admin user is NOT a member of,
// plus a project owned by it, and returns the org ID.
func (s *OrgOwnershipSuite) newForeignOrg() string {
	orgB := uuid.New().String()
	s.Require().NoError(testutil.CreateTestOrganization(s.Ctx, s.DB(), orgB, "Org B"))
	s.Require().NoError(testutil.CreateTestProject(s.Ctx, s.DB(), testutil.TestProject{
		ID:    uuid.New().String(),
		OrgID: orgB,
		Name:  "Project B",
	}, testutil.AdminUser.ID))
	return orgB
}

// TestCrossOrgUsageForbidden is the primary reproducer: a caller who is a member
// of org A (and has no org context in the request) must NOT be able to read org
// B's usage. Before the fix this returned 200 because the handler injected the
// :orgId path parameter into the empty context and compared it against itself.
func (s *OrgOwnershipSuite) TestCrossOrgUsageForbidden() {
	orgB := s.newForeignOrg()

	resp := s.Client.GET(
		"/api/v1/organizations/"+orgB+"/usage",
		testutil.WithAuth("e2e-test-user"),
	)
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-org usage read must be forbidden, got %d: %s", resp.StatusCode, resp.String())
}

// TestOwnOrgUsageOK proves the membership derivation still admits the caller's
// own org: the admin user is a member of s.OrgID, so the same-shaped request
// against their own org must succeed.
func (s *OrgOwnershipSuite) TestOwnOrgUsageOK() {
	resp := s.Client.GET(
		"/api/v1/organizations/"+s.OrgID+"/usage",
		testutil.WithAuth("e2e-test-user"),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"own-org usage read must succeed, got %d: %s", resp.StatusCode, resp.String())
}

// TestCrossOrgRemainingOrgRoutesForbidden verifies the sibling org-scoped
// provider routes share the same guard and reject a cross-org caller.
func (s *OrgOwnershipSuite) TestCrossOrgRemainingOrgRoutesForbidden() {
	orgB := s.newForeignOrg()

	routes := []string{
		"/api/v1/organizations/" + orgB + "/project-providers",
		"/api/v1/organizations/" + orgB + "/usage/timeseries",
		"/api/v1/organizations/" + orgB + "/usage/by-project",
	}
	for _, path := range routes {
		resp := s.Client.GET(path, testutil.WithAuth("e2e-test-user"))
		s.Require().Equal(http.StatusForbidden, resp.StatusCode,
			"%s must be forbidden, got %d: %s", path, resp.StatusCode, resp.String())
	}
}
