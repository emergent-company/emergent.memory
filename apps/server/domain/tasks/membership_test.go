package tasks_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// TasksMembershipSuite proves a caller's task access is derived from real
// organization membership, not from the client-supplied ?project_id query param
// or X-Project-ID header (issue #913).
type TasksMembershipSuite struct {
	testutil.BaseSuite
}

func TestTasksMembershipSuite(t *testing.T) {
	suite.Run(t, new(TasksMembershipSuite))
}

func (s *TasksMembershipSuite) SetupSuite() {
	s.SetDBSuffix("tasks_membership")
	s.BaseSuite.SetupSuite()
}

func (s *TasksMembershipSuite) newForeignProject() string {
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

func (s *TasksMembershipSuite) seedTask(projectID string) string {
	taskID := uuid.New().String()
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.tasks (id, project_id, title, type, status, created_at, updated_at)
		VALUES (?, ?, 'foreign task', 'review', 'pending', NOW(), NOW())
	`, taskID, projectID).Exec(s.Ctx)
	s.Require().NoError(err)
	return taskID
}

func (s *TasksMembershipSuite) TestCrossProjectTaskReadForbidden() {
	projectB := s.newForeignProject()
	taskB := s.seedTask(projectB)

	resp := s.Client.GET("/api/tasks?project_id="+projectB,
		testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project task list must be forbidden, got %d: %s", resp.StatusCode, resp.String())

	resp = s.Client.GET("/api/tasks/counts?project_id="+projectB,
		testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project task counts must be forbidden, got %d: %s", resp.StatusCode, resp.String())

	resp = s.Client.GET("/api/tasks/"+taskB+"?project_id="+projectB,
		testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project task get must be forbidden, got %d: %s", resp.StatusCode, resp.String())
}

func (s *TasksMembershipSuite) TestCrossProjectTaskWriteForbidden() {
	projectB := s.newForeignProject()
	taskB := s.seedTask(projectB)

	resp := s.Client.POST("/api/tasks/"+taskB+"/resolve?project_id="+projectB,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{"resolution": "accepted"}))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project task resolve must be forbidden, got %d: %s", resp.StatusCode, resp.String())

	resp = s.Client.POST("/api/tasks/"+taskB+"/cancel?project_id="+projectB,
		testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project task cancel must be forbidden, got %d: %s", resp.StatusCode, resp.String())
}

func (s *TasksMembershipSuite) TestOwnProjectTaskOK() {
	resp := s.Client.GET("/api/tasks?project_id="+s.ProjectID,
		testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"own-project task list must succeed, got %d: %s", resp.StatusCode, resp.String())

	resp = s.Client.GET("/api/tasks/counts?project_id="+s.ProjectID,
		testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"own-project task counts must succeed, got %d: %s", resp.StatusCode, resp.String())
}
