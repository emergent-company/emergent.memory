package chunks_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// ChunksMembershipSuite exercises the header-scoped /api/chunks group through
// the full in-process server to prove that a caller's authorization is derived
// from real organization membership, not from the client-supplied X-Project-ID
// header (issue #868).
type ChunksMembershipSuite struct {
	testutil.BaseSuite
}

func TestChunksMembershipSuite(t *testing.T) {
	suite.Run(t, new(ChunksMembershipSuite))
}

func (s *ChunksMembershipSuite) SetupSuite() {
	s.SetDBSuffix("chunks_membership")
	s.BaseSuite.SetupSuite()
}

// newForeignProject creates an organization the admin user is NOT a member of,
// plus a project owned by it, and returns the foreign project ID.
func (s *ChunksMembershipSuite) newForeignProject() string {
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

// seedChunk inserts a document + chunk owned by projectID directly in the DB.
func (s *ChunksMembershipSuite) seedChunk(projectID string) (docID, chunkID string) {
	docID = uuid.New().String()
	chunkID = uuid.New().String()
	s.Require().NoError(testutil.CreateTestDocument(s.Ctx, s.DB(), testutil.TestDocument{
		ID:        docID,
		ProjectID: projectID,
		Filename:  testutil.StringPtr("foreign-secret.txt"),
		Content:   testutil.StringPtr("foreign secret content"),
	}))
	s.Require().NoError(testutil.CreateTestChunk(s.Ctx, s.DB(), testutil.TestChunk{
		ID:         chunkID,
		DocumentID: docID,
		ChunkIndex: 0,
		Text:       "foreign secret chunk",
	}))
	return docID, chunkID
}

// TestCrossProjectChunkReadForbidden is the read reproducer: a member of org A
// who sends org B's project id via X-Project-ID must not list org B's chunks.
// Before the fix this returned 200 with org B's chunk data.
func (s *ChunksMembershipSuite) TestCrossProjectChunkReadForbidden() {
	projectB := s.newForeignProject()
	_, _ = s.seedChunk(projectB)

	resp := s.Client.GET("/api/chunks",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(projectB))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project chunk list must be forbidden, got %d: %s", resp.StatusCode, resp.String())
}

// TestCrossProjectChunkWriteForbidden proves a member of org A must not delete
// chunks in org B's project via X-Project-ID.
func (s *ChunksMembershipSuite) TestCrossProjectChunkWriteForbidden() {
	projectB := s.newForeignProject()
	_, chunkB := s.seedChunk(projectB)

	resp := s.Client.DELETE("/api/chunks/"+chunkB,
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(projectB))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project chunk delete must be forbidden, got %d: %s", resp.StatusCode, resp.String())
}

// TestOwnProjectChunkAccessOK proves the membership derivation still admits the
// caller's own project.
func (s *ChunksMembershipSuite) TestOwnProjectChunkAccessOK() {
	resp := s.Client.GET("/api/chunks",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"own-project chunk list must succeed, got %d: %s", resp.StatusCode, resp.String())
}

// TestChunksNoUserUnauthorized proves an unauthenticated request is rejected
// with 401 before any membership resolution.
func (s *ChunksMembershipSuite) TestChunksNoUserUnauthorized() {
	resp := s.Client.GET("/api/chunks", testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusUnauthorized, resp.StatusCode,
		"unauthenticated chunk list must be 401, got %d: %s", resp.StatusCode, resp.String())
}

// TestChunksUnknownProjectNotFound proves a session caller addressing a
// non-existent project receives 404 (no existence oracle).
func (s *ChunksMembershipSuite) TestChunksUnknownProjectNotFound() {
	resp := s.Client.GET("/api/chunks",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(uuid.New().String()))
	s.Require().Equal(http.StatusNotFound, resp.StatusCode,
		"unknown-project chunk list must be 404, got %d: %s", resp.StatusCode, resp.String())
}

// TestChunksTokenProjectBindingForbidden proves a project-bound emt_* token
// that presents a different project's id via X-Project-ID is rejected 403.
func (s *ChunksMembershipSuite) TestChunksTokenProjectBindingForbidden() {
	projectB := s.newForeignProject()

	token := "emt_test_868_chunks_binding"
	s.Require().NoError(testutil.CreateTestAPIToken(s.Ctx, s.DB(),
		testutil.AdminUser.ID, token, []string{"chunks:read"}, s.ProjectID))

	resp := s.Client.GET("/api/chunks",
		testutil.WithAuth(token), testutil.WithProjectID(projectB))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"project token addressing a different project via X-Project-ID must be 403, got %d: %s",
		resp.StatusCode, resp.String())
}

// TestChunksTokenProjectBindingOK proves a project-bound emt_* token presenting
// its own project via X-Project-ID is still admitted.
func (s *ChunksMembershipSuite) TestChunksTokenProjectBindingOK() {
	token := "emt_test_868_chunks_binding_ok"
	s.Require().NoError(testutil.CreateTestAPIToken(s.Ctx, s.DB(),
		testutil.AdminUser.ID, token, []string{"chunks:read"}, s.ProjectID))

	resp := s.Client.GET("/api/chunks",
		testutil.WithAuth(token), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"project token addressing its own project must be 200, got %d: %s",
		resp.StatusCode, resp.String())
}
