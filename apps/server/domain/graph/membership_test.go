package graph_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// GraphMembershipSuite exercises the header-scoped /api/graph group through the
// full in-process server to prove that a caller's authorization is derived from
// real organization membership, not from the client-supplied X-Project-ID
// header (issue #909).
type GraphMembershipSuite struct {
	testutil.BaseSuite
}

func TestGraphMembershipSuite(t *testing.T) {
	suite.Run(t, new(GraphMembershipSuite))
}

func (s *GraphMembershipSuite) SetupSuite() {
	s.SetDBSuffix("graph_membership")
	s.BaseSuite.SetupSuite()
}

// newForeignProject creates an organization the admin user is NOT a member of,
// plus a project owned by it, and returns the foreign project ID.
func (s *GraphMembershipSuite) newForeignProject() string {
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

// crossProjectDenied is the shared fail-first assertion: an authenticated member
// of org A addressing org B's project via X-Project-ID must be denied.
func (s *GraphMembershipSuite) crossProjectDenied(method, path string, opts ...testutil.RequestOption) {
	projectB := s.newForeignProject()
	opts = append([]testutil.RequestOption{
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(projectB),
	}, opts...)

	var resp *testutil.HTTPResponse
	switch method {
	case http.MethodGet:
		resp = s.Client.GET(path, opts...)
	case http.MethodPost:
		resp = s.Client.POST(path, opts...)
	}
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"%s %s cross-project must be 403, got %d: %s", method, path, resp.StatusCode, resp.String())
}

// ownProjectOK asserts a member of the owning project is still admitted (no
// 401/403) — membership derivation must admit the caller's own project.
func (s *GraphMembershipSuite) ownProjectOK(method, path string, opts ...testutil.RequestOption) {
	opts = append([]testutil.RequestOption{
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID),
	}, opts...)

	var resp *testutil.HTTPResponse
	switch method {
	case http.MethodGet:
		resp = s.Client.GET(path, opts...)
	case http.MethodPost:
		resp = s.Client.POST(path, opts...)
	}
	s.Require().NotEqual(http.StatusUnauthorized, resp.StatusCode,
		"%s %s own-project must not be 401, got %d: %s", method, path, resp.StatusCode, resp.String())
	s.Require().NotEqual(http.StatusForbidden, resp.StatusCode,
		"%s %s own-project must not be 403, got %d: %s", method, path, resp.StatusCode, resp.String())
}

func (s *GraphMembershipSuite) TestCrossProjectObjectsSearchForbidden() {
	s.crossProjectDenied(http.MethodGet, "/api/graph/objects/search?limit=25")
}

func (s *GraphMembershipSuite) TestCrossProjectObjectsCountForbidden() {
	s.crossProjectDenied(http.MethodGet, "/api/graph/objects/count")
}

func (s *GraphMembershipSuite) TestCrossProjectObjectsFTSForbidden() {
	s.crossProjectDenied(http.MethodGet, "/api/graph/objects/fts?q=test")
}

func (s *GraphMembershipSuite) TestCrossProjectHybridSearchForbidden() {
	s.crossProjectDenied(http.MethodPost, "/api/graph/search",
		testutil.WithJSONBody(map[string]any{"query": "foreign secret"}))
}

func (s *GraphMembershipSuite) TestOwnProjectObjectsSearchOK() {
	s.ownProjectOK(http.MethodGet, "/api/graph/objects/search?limit=25")
}

func (s *GraphMembershipSuite) TestOwnProjectObjectsCountOK() {
	s.ownProjectOK(http.MethodGet, "/api/graph/objects/count")
}

func (s *GraphMembershipSuite) TestOwnProjectObjectsFTSOK() {
	s.ownProjectOK(http.MethodGet, "/api/graph/objects/fts?q=nonexistent")
}

func (s *GraphMembershipSuite) TestOwnProjectHybridSearchOK() {
	s.ownProjectOK(http.MethodPost, "/api/graph/search",
		testutil.WithJSONBody(map[string]any{"query": "nonexistent query term"}))
}

func (s *GraphMembershipSuite) TestObjectsSearchNoUserUnauthorized() {
	resp := s.Client.GET("/api/graph/objects/search", testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusUnauthorized, resp.StatusCode,
		"unauthenticated search must be 401, got %d: %s", resp.StatusCode, resp.String())
}

func (s *GraphMembershipSuite) TestObjectsSearchUnknownProjectNotFound() {
	resp := s.Client.GET("/api/graph/objects/search",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(uuid.New().String()))
	s.Require().Equal(http.StatusNotFound, resp.StatusCode,
		"unknown-project search must be 404, got %d: %s", resp.StatusCode, resp.String())
}

// TestTokenProjectBindingForbidden proves a project-bound emt_* token that
// presents a different project's id via X-Project-ID is rejected 403
// (RequireProjectTokenScope), before any membership resolution.
func (s *GraphMembershipSuite) TestTokenProjectBindingForbidden() {
	projectB := s.newForeignProject()

	token := "emt_test_909_graph_binding"
	s.Require().NoError(testutil.CreateTestAPIToken(s.Ctx, s.DB(),
		testutil.AdminUser.ID, token, []string{"graph:read"}, s.ProjectID))

	resp := s.Client.GET("/api/graph/objects/search",
		testutil.WithAuth(token), testutil.WithProjectID(projectB))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"project token addressing a different project via X-Project-ID must be 403, got %d: %s",
		resp.StatusCode, resp.String())
}

// TestTokenProjectBindingOK proves a project-bound emt_* token presenting its
// own project via X-Project-ID is still admitted.
func (s *GraphMembershipSuite) TestTokenProjectBindingOK() {
	token := "emt_test_909_graph_binding_ok"
	s.Require().NoError(testutil.CreateTestAPIToken(s.Ctx, s.DB(),
		testutil.AdminUser.ID, token, []string{"graph:read"}, s.ProjectID))

	resp := s.Client.GET("/api/graph/objects/search",
		testutil.WithAuth(token), testutil.WithProjectID(s.ProjectID))
	s.Require().NotEqual(http.StatusForbidden, resp.StatusCode,
		"project token addressing its own project must not be 403, got %d: %s",
		resp.StatusCode, resp.String())
}
