package discoveryjobs_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// DiscoveryJobsMembershipSuite proves a caller's discovery-job access is derived
// from real organization membership, not from the client-supplied :projectId
// path param, X-Project-ID header, or :jobId (which is resolved server-side to
// its owning project) (issue #913).
type DiscoveryJobsMembershipSuite struct {
	testutil.BaseSuite
}

func TestDiscoveryJobsMembershipSuite(t *testing.T) {
	suite.Run(t, new(DiscoveryJobsMembershipSuite))
}

func (s *DiscoveryJobsMembershipSuite) SetupSuite() {
	s.SetDBSuffix("discoveryjobs_membership")
	s.BaseSuite.SetupSuite()
}

func (s *DiscoveryJobsMembershipSuite) newForeignProject() (string, string) {
	orgB := uuid.New().String()
	s.Require().NoError(testutil.CreateTestOrganization(s.Ctx, s.DB(), orgB, "Org B"))
	projectB := uuid.New().String()
	s.Require().NoError(testutil.CreateTestProject(s.Ctx, s.DB(), testutil.TestProject{
		ID:    projectB,
		OrgID: orgB,
		Name:  "Project B",
	}, testutil.AdminUser.ID))
	return orgB, projectB
}

func (s *DiscoveryJobsMembershipSuite) seedJob(orgID, projectID string) string {
	jobID := uuid.New().String()
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.discovery_jobs
			(id, organization_id, project_id, status, progress, config, kb_purpose, created_at, updated_at)
		VALUES (?, ?, ?, 'pending', '{}'::jsonb, '{}'::jsonb, 'purpose', NOW(), NOW())
	`, jobID, orgID, projectID).Exec(s.Ctx)
	s.Require().NoError(err)
	return jobID
}

func (s *DiscoveryJobsMembershipSuite) TestCrossProjectJobListForbidden() {
	_, projectB := s.newForeignProject()

	resp := s.Client.GET("/api/discovery-jobs/projects/"+projectB,
		testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project job list must be forbidden, got %d: %s", resp.StatusCode, resp.String())
}

func (s *DiscoveryJobsMembershipSuite) TestCrossProjectJobReadForbidden() {
	orgB, projectB := s.newForeignProject()
	jobB := s.seedJob(orgB, projectB)

	resp := s.Client.GET("/api/discovery-jobs/"+jobB,
		testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project job status must be forbidden, got %d: %s", resp.StatusCode, resp.String())
}

func (s *DiscoveryJobsMembershipSuite) TestCrossProjectJobWriteForbidden() {
	orgB, projectB := s.newForeignProject()
	jobB := s.seedJob(orgB, projectB)

	resp := s.Client.DELETE("/api/discovery-jobs/"+jobB,
		testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project job cancel must be forbidden, got %d: %s", resp.StatusCode, resp.String())

	resp = s.Client.POST("/api/discovery-jobs/projects/"+projectB+"/start",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{"document_ids": []string{uuid.New().String()}}))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project job start must be forbidden, got %d: %s", resp.StatusCode, resp.String())
}

func (s *DiscoveryJobsMembershipSuite) TestOwnProjectJobOK() {
	resp := s.Client.GET("/api/discovery-jobs/projects/"+s.ProjectID,
		testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"own-project job list must succeed, got %d: %s", resp.StatusCode, resp.String())
}
