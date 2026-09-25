package documents_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// DocumentsMembershipSuite exercises the header-scoped /api/documents and
// /api/document-parsing-jobs groups through the full in-process server to prove
// that a caller's authorization is derived from real organization membership,
// not from the client-supplied X-Project-ID header (issue #868).
//
// The admin user (e2e-test-user token) is set up by BaseSuite.SetupTest as an
// org_admin member of s.OrgID (org A). A second org B is created on demand with
// no membership for the admin user, and the caller sends org B's project id via
// X-Project-ID to reach B's documents.
type DocumentsMembershipSuite struct {
	testutil.BaseSuite
}

func TestDocumentsMembershipSuite(t *testing.T) {
	suite.Run(t, new(DocumentsMembershipSuite))
}

func (s *DocumentsMembershipSuite) SetupSuite() {
	s.SetDBSuffix("documents_membership")
	s.BaseSuite.SetupSuite()
}

// newForeignProject creates an organization the admin user is NOT a member of,
// plus a project owned by it, and returns the foreign project ID.
func (s *DocumentsMembershipSuite) newForeignProject() string {
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

// seedDocument inserts a document owned by projectID directly in the DB (the
// admin user cannot create one via the API once the membership guard lands).
func (s *DocumentsMembershipSuite) seedDocument(projectID string) string {
	docID := uuid.New()
	s.Require().NoError(testutil.CreateTestDocument(s.Ctx, s.DB(), testutil.TestDocument{
		ID:        docID.String(),
		ProjectID: projectID,
		Filename:  testutil.StringPtr("foreign-secret.txt"),
		Content:   testutil.StringPtr("foreign secret content"),
	}))
	return docID.String()
}

// TestCrossProjectDocumentReadForbidden is the read reproducer: a member of
// org A who sends org B's project id via X-Project-ID must not list or read
// org B's documents. Before the fix these returned 200 because the handlers
// scoped by the header-derived project ID without any membership check.
func (s *DocumentsMembershipSuite) TestCrossProjectDocumentReadForbidden() {
	projectB := s.newForeignProject()
	docB := s.seedDocument(projectB)

	resp := s.Client.GET("/api/documents/"+docB,
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(projectB))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project document read must be forbidden, got %d: %s", resp.StatusCode, resp.String())

	resp = s.Client.GET("/api/documents",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(projectB))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project document list must be forbidden, got %d: %s", resp.StatusCode, resp.String())
}

// TestCrossProjectDocumentWriteForbidden is the higher-severity reproducer: a
// member of org A must not create (or delete) documents in org B's project via
// X-Project-ID.
func (s *DocumentsMembershipSuite) TestCrossProjectDocumentWriteForbidden() {
	projectB := s.newForeignProject()
	docB := s.seedDocument(projectB)

	resp := s.Client.POST("/api/documents",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(projectB),
		testutil.WithJSONBody(map[string]any{"filename": "stolen.txt", "content": "hi"}))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project document create must be forbidden, got %d: %s", resp.StatusCode, resp.String())

	resp = s.Client.DELETE("/api/documents/"+docB,
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(projectB))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project document delete must be forbidden, got %d: %s", resp.StatusCode, resp.String())
}

// TestCrossProjectParsingJobForbidden proves the legacy upload surface
// (/api/document-parsing-jobs) is protected by the same membership guard.
func (s *DocumentsMembershipSuite) TestCrossProjectParsingJobForbidden() {
	projectB := s.newForeignProject()

	resp := s.Client.POST("/api/document-parsing-jobs/upload",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(projectB))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project parsing-job upload must be forbidden, got %d: %s", resp.StatusCode, resp.String())
}

// TestOwnProjectDocumentAccessOK proves the membership derivation still admits
// the caller's own project: the admin user is a member of s.OrgID, so document
// read and write against their own project must succeed.
func (s *DocumentsMembershipSuite) TestOwnProjectDocumentAccessOK() {
	resp := s.Client.GET("/api/documents",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"own-project document list must succeed, got %d: %s", resp.StatusCode, resp.String())

	resp = s.Client.POST("/api/documents",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{"filename": "own.txt", "content": "own content"}))
	s.Require().True(resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated,
		"own-project document create must succeed, got %d: %s", resp.StatusCode, resp.String())
}

// TestDocumentsNoUserUnauthorized proves an unauthenticated request is rejected
// with 401 before any membership resolution.
func (s *DocumentsMembershipSuite) TestDocumentsNoUserUnauthorized() {
	resp := s.Client.GET("/api/documents", testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusUnauthorized, resp.StatusCode,
		"unauthenticated document list must be 401, got %d: %s", resp.StatusCode, resp.String())
}

// TestDocumentsUnknownProjectNotFound proves a session caller addressing a
// non-existent project receives 404 (no existence oracle) rather than an empty
// result set.
func (s *DocumentsMembershipSuite) TestDocumentsUnknownProjectNotFound() {
	resp := s.Client.GET("/api/documents",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(uuid.New().String()))
	s.Require().Equal(http.StatusNotFound, resp.StatusCode,
		"unknown-project document list must be 404, got %d: %s", resp.StatusCode, resp.String())
}

// TestDocumentsTokenProjectBindingForbidden proves a project-bound emt_* token
// that presents a different project's id via X-Project-ID is rejected 403 by the
// shared RequireProjectTokenScope middleware before any membership resolution.
func (s *DocumentsMembershipSuite) TestDocumentsTokenProjectBindingForbidden() {
	projectB := s.newForeignProject()

	token := "emt_test_868_docs_binding"
	s.Require().NoError(testutil.CreateTestAPIToken(s.Ctx, s.DB(),
		testutil.AdminUser.ID, token, []string{"documents:read"}, s.ProjectID))

	resp := s.Client.GET("/api/documents",
		testutil.WithAuth(token), testutil.WithProjectID(projectB))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"project token addressing a different project via X-Project-ID must be 403, got %d: %s",
		resp.StatusCode, resp.String())
}

// TestDocumentsTokenProjectBindingOK proves a project-bound emt_* token
// presenting its own project via X-Project-ID is still admitted.
func (s *DocumentsMembershipSuite) TestDocumentsTokenProjectBindingOK() {
	token := "emt_test_868_docs_binding_ok"
	s.Require().NoError(testutil.CreateTestAPIToken(s.Ctx, s.DB(),
		testutil.AdminUser.ID, token, []string{"documents:read"}, s.ProjectID))

	resp := s.Client.GET("/api/documents",
		testutil.WithAuth(token), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"project token addressing its own project must be 200, got %d: %s",
		resp.StatusCode, resp.String())
}
