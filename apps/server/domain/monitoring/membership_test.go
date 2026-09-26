package monitoring_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// MonitoringMembershipSuite exercises the header-scoped /api/monitoring group
// through the full in-process server. Handlers scope extraction jobs by
// user.ProjectID (the X-Project-ID header) with no membership check before the
// fix (issue #868, the #864 class), so an org-A member could list org B's
// extraction jobs.
type MonitoringMembershipSuite struct {
	testutil.BaseSuite
}

func TestMonitoringMembershipSuite(t *testing.T) {
	suite.Run(t, new(MonitoringMembershipSuite))
}

func (s *MonitoringMembershipSuite) SetupSuite() {
	s.SetDBSuffix("monitoring_membership")
	s.BaseSuite.SetupSuite()
}

func (s *MonitoringMembershipSuite) newForeignProject() string {
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

func (s *MonitoringMembershipSuite) TestCrossProjectListForbidden() {
	projectB := s.newForeignProject()
	resp := s.Client.GET("/api/monitoring/extraction-jobs",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(projectB))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project extraction-jobs list must be forbidden, got %d: %s", resp.StatusCode, resp.String())
}

func (s *MonitoringMembershipSuite) TestOwnProjectListOK() {
	resp := s.Client.GET("/api/monitoring/extraction-jobs",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"own-project extraction-jobs list must succeed, got %d: %s", resp.StatusCode, resp.String())
}

func (s *MonitoringMembershipSuite) TestNoUserUnauthorized() {
	resp := s.Client.GET("/api/monitoring/extraction-jobs", testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusUnauthorized, resp.StatusCode,
		"unauthenticated extraction-jobs list must be 401, got %d: %s", resp.StatusCode, resp.String())
}

func (s *MonitoringMembershipSuite) TestTokenProjectBindingForbidden() {
	projectB := s.newForeignProject()
	token := "emt_868_monitoring_binding"
	s.Require().NoError(testutil.CreateTestAPIToken(s.Ctx, s.DB(),
		testutil.AdminUser.ID, token, []string{"extraction:read"}, s.ProjectID))

	resp := s.Client.GET("/api/monitoring/extraction-jobs",
		testutil.WithAuth(token), testutil.WithProjectID(projectB))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"project token addressing a different project must be 403, got %d: %s", resp.StatusCode, resp.String())
}
