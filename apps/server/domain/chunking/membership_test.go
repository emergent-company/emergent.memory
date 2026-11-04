package chunking_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// ChunkingMembershipSuite exercises the header-scoped recreate-chunks surface
// (POST /api/documents/:id/recreate-chunks) through the full in-process server
// to prove that a caller's authorization is derived from real organization
// membership, not from the client-supplied X-Project-ID header (issue #868).
type ChunkingMembershipSuite struct {
	testutil.BaseSuite
}

func TestChunkingMembershipSuite(t *testing.T) {
	suite.Run(t, new(ChunkingMembershipSuite))
}

func (s *ChunkingMembershipSuite) SetupSuite() {
	s.SetDBSuffix("chunking_membership")
	s.BaseSuite.SetupSuite()
}

// newForeignProject creates an organization the admin user is NOT a member of,
// plus a project owned by it, and returns the foreign project ID.
func (s *ChunkingMembershipSuite) newForeignProject() string {
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

// TestCrossProjectRecreateChunksForbidden is the write reproducer: a member of
// org A who sends org B's project id via X-Project-ID must not recreate org B's
// chunks. Before the fix the handler ran scoped by the header-derived project ID
// with no membership check.
func (s *ChunkingMembershipSuite) TestCrossProjectRecreateChunksForbidden() {
	projectB := s.newForeignProject()

	resp := s.Client.POST("/api/documents/"+uuid.New().String()+"/recreate-chunks",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(projectB))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project recreate-chunks must be forbidden, got %d: %s", resp.StatusCode, resp.String())
}

// TestOwnProjectRecreateChunksWriteOK proves a member of the owning org can
// still write via recreate-chunks on their own project (a seeded document
// yields a 200 success).
func (s *ChunkingMembershipSuite) TestOwnProjectRecreateChunksWriteOK() {
	docID := uuid.New()
	s.Require().NoError(testutil.CreateTestDocument(s.Ctx, s.DB(), testutil.TestDocument{
		ID:        docID.String(),
		ProjectID: s.ProjectID,
		Filename:  testutil.StringPtr("own.txt"),
		Content:   testutil.StringPtr("own project content to recreate"),
	}))

	resp := s.Client.POST("/api/documents/"+docID.String()+"/recreate-chunks",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"own-project recreate-chunks must succeed, got %d: %s", resp.StatusCode, resp.String())
}

// TestChunkingNoUserUnauthorized proves an unauthenticated request is rejected
// with 401 before any membership resolution.
func (s *ChunkingMembershipSuite) TestChunkingNoUserUnauthorized() {
	resp := s.Client.POST("/api/documents/"+uuid.New().String()+"/recreate-chunks",
		testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusUnauthorized, resp.StatusCode,
		"unauthenticated recreate-chunks must be 401, got %d: %s", resp.StatusCode, resp.String())
}

// TestChunkingUnknownProjectNotFound proves a session caller addressing a
// non-existent project receives 404 (no existence oracle).
func (s *ChunkingMembershipSuite) TestChunkingUnknownProjectNotFound() {
	resp := s.Client.POST("/api/documents/"+uuid.New().String()+"/recreate-chunks",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(uuid.New().String()))
	s.Require().Equal(http.StatusNotFound, resp.StatusCode,
		"unknown-project recreate-chunks must be 404, got %d: %s", resp.StatusCode, resp.String())
}

// TestChunkingTokenProjectBindingForbidden proves a project-bound emt_* token
// that presents a different project's id via X-Project-ID is rejected 403.
func (s *ChunkingMembershipSuite) TestChunkingTokenProjectBindingForbidden() {
	projectB := s.newForeignProject()

	token := "emt_test_868_chunking_binding"
	s.Require().NoError(testutil.CreateTestAPIToken(s.Ctx, s.DB(),
		testutil.AdminUser.ID, token, []string{"documents:write"}, s.ProjectID))

	resp := s.Client.POST("/api/documents/"+uuid.New().String()+"/recreate-chunks",
		testutil.WithAuth(token), testutil.WithProjectID(projectB))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"project token addressing a different project via X-Project-ID must be 403, got %d: %s",
		resp.StatusCode, resp.String())
}

// TestChunkingTokenProjectBindingOK proves a project-bound emt_* token
// presenting its own project via X-Project-ID is still admitted (reaches the
// handler and returns 404 for a missing document, not 403).
func (s *ChunkingMembershipSuite) TestChunkingTokenProjectBindingOK() {
	token := "emt_test_868_chunking_binding_ok"
	s.Require().NoError(testutil.CreateTestAPIToken(s.Ctx, s.DB(),
		testutil.AdminUser.ID, token, []string{"documents:write"}, s.ProjectID))

	resp := s.Client.POST("/api/documents/"+uuid.New().String()+"/recreate-chunks",
		testutil.WithAuth(token), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusNotFound, resp.StatusCode,
		"project token addressing its own project must reach the handler (404), got %d: %s",
		resp.StatusCode, resp.String())
}
