package embeddingpolicies_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// EmbeddingPoliciesMembershipSuite proves a caller's embedding-policy access is
// derived from real organization membership, not from the client-supplied
// ?project_id query param or request-body projectId field (issue #913).
type EmbeddingPoliciesMembershipSuite struct {
	testutil.BaseSuite
}

func TestEmbeddingPoliciesMembershipSuite(t *testing.T) {
	suite.Run(t, new(EmbeddingPoliciesMembershipSuite))
}

func (s *EmbeddingPoliciesMembershipSuite) SetupSuite() {
	s.SetDBSuffix("embeddingpolicies_membership")
	s.BaseSuite.SetupSuite()
}

func (s *EmbeddingPoliciesMembershipSuite) newForeignProject() string {
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

func (s *EmbeddingPoliciesMembershipSuite) TestCrossProjectPolicyReadForbidden() {
	projectB := s.newForeignProject()

	resp := s.Client.GET("/api/graph/embedding-policies?project_id="+projectB,
		testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project policy list must be forbidden, got %d: %s", resp.StatusCode, resp.String())
}

func (s *EmbeddingPoliciesMembershipSuite) TestCrossProjectPolicyWriteForbidden() {
	projectB := s.newForeignProject()

	resp := s.Client.POST("/api/graph/embedding-policies",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{"projectId": projectB, "objectType": "Person"}))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project policy create must be forbidden, got %d: %s", resp.StatusCode, resp.String())
}

func (s *EmbeddingPoliciesMembershipSuite) TestOwnProjectPolicyOK() {
	resp := s.Client.GET("/api/graph/embedding-policies?project_id="+s.ProjectID,
		testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"own-project policy list must succeed, got %d: %s", resp.StatusCode, resp.String())

	resp = s.Client.POST("/api/graph/embedding-policies",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{"projectId": s.ProjectID, "objectType": "Person"}))
	s.Require().Equal(http.StatusCreated, resp.StatusCode,
		"own-project policy create must succeed, got %d: %s", resp.StatusCode, resp.String())
}
