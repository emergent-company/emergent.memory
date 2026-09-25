package search_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// SearchMembershipSuite exercises the header-scoped /api/search group through
// the full in-process server to prove that a caller's authorization is derived
// from real organization membership, not from the client-supplied X-Project-ID
// header (issue #868).
type SearchMembershipSuite struct {
	testutil.BaseSuite
}

func TestSearchMembershipSuite(t *testing.T) {
	suite.Run(t, new(SearchMembershipSuite))
}

func (s *SearchMembershipSuite) SetupSuite() {
	s.SetDBSuffix("search_membership")
	s.BaseSuite.SetupSuite()
}

// newForeignProject creates an organization the admin user is NOT a member of,
// plus a project owned by it, and returns the foreign project ID.
func (s *SearchMembershipSuite) newForeignProject() string {
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

// TestCrossProjectSearchForbidden is the read reproducer: a member of org A who
// sends org B's project id via X-Project-ID must not search org B's data.
// Before the fix the search ran scoped by the header-derived project ID without
// any membership check.
func (s *SearchMembershipSuite) TestCrossProjectSearchForbidden() {
	projectB := s.newForeignProject()

	resp := s.Client.POST("/api/search/unified",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(projectB),
		testutil.WithJSONBody(map[string]any{"query": "foreign secret"}))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project search must be forbidden, got %d: %s", resp.StatusCode, resp.String())
}

// TestOwnProjectSearchAccessOK proves the membership derivation still admits the
// caller's own project: the admin user is a member of s.OrgID, so a search
// against their own project must pass the auth guard (any non-401/403 status).
func (s *SearchMembershipSuite) TestOwnProjectSearchAccessOK() {
	resp := s.Client.POST("/api/search/unified",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{"query": "nonexistent query term"}))
	s.Require().NotEqual(http.StatusUnauthorized, resp.StatusCode,
		"own-project search must not be 401, got %d: %s", resp.StatusCode, resp.String())
	s.Require().NotEqual(http.StatusForbidden, resp.StatusCode,
		"own-project search must not be 403, got %d: %s", resp.StatusCode, resp.String())
}

// TestSearchNoUserUnauthorized proves an unauthenticated request is rejected
// with 401 before any membership resolution.
func (s *SearchMembershipSuite) TestSearchNoUserUnauthorized() {
	resp := s.Client.POST("/api/search/unified",
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{"query": "test"}))
	s.Require().Equal(http.StatusUnauthorized, resp.StatusCode,
		"unauthenticated search must be 401, got %d: %s", resp.StatusCode, resp.String())
}

// TestSearchUnknownProjectNotFound proves a session caller addressing a
// non-existent project receives 404 (no existence oracle).
func (s *SearchMembershipSuite) TestSearchUnknownProjectNotFound() {
	resp := s.Client.POST("/api/search/unified",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(uuid.New().String()),
		testutil.WithJSONBody(map[string]any{"query": "test"}))
	s.Require().Equal(http.StatusNotFound, resp.StatusCode,
		"unknown-project search must be 404, got %d: %s", resp.StatusCode, resp.String())
}

// TestSearchTokenProjectBindingForbidden proves a project-bound emt_* token
// that presents a different project's id via X-Project-ID is rejected 403.
func (s *SearchMembershipSuite) TestSearchTokenProjectBindingForbidden() {
	projectB := s.newForeignProject()

	token := "emt_test_868_search_binding"
	s.Require().NoError(testutil.CreateTestAPIToken(s.Ctx, s.DB(),
		testutil.AdminUser.ID, token, []string{"search:read"}, s.ProjectID))

	resp := s.Client.POST("/api/search/unified",
		testutil.WithAuth(token), testutil.WithProjectID(projectB),
		testutil.WithJSONBody(map[string]any{"query": "test"}))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"project token addressing a different project via X-Project-ID must be 403, got %d: %s",
		resp.StatusCode, resp.String())
}

// TestSearchTokenProjectBindingOK proves a project-bound emt_* token presenting
// its own project via X-Project-ID is still admitted.
func (s *SearchMembershipSuite) TestSearchTokenProjectBindingOK() {
	token := "emt_test_868_search_binding_ok"
	s.Require().NoError(testutil.CreateTestAPIToken(s.Ctx, s.DB(),
		testutil.AdminUser.ID, token, []string{"search:read"}, s.ProjectID))

	resp := s.Client.POST("/api/search/unified",
		testutil.WithAuth(token), testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{"query": "nonexistent query term"}))
	s.Require().NotEqual(http.StatusForbidden, resp.StatusCode,
		"project token addressing its own project must not be 403, got %d: %s",
		resp.StatusCode, resp.String())
}
