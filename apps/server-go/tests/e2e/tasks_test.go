package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/emergent/emergent-core/internal/testutil"
)

// TasksTestSuite tests the tasks API endpoints
type TasksTestSuite struct {
	testutil.BaseSuite
}

func TestTasksSuite(t *testing.T) {
	suite.Run(t, new(TasksTestSuite))
}

func (s *TasksTestSuite) SetupSuite() {
	s.SetDBSuffix("tasks")
	s.BaseSuite.SetupSuite()
}

// createOrgViaAPI creates an org via API and returns its ID
func (s *TasksTestSuite) createOrgViaAPI(name string) string {
	resp := s.Client.POST("/api/orgs",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"name": name,
		}),
	)
	s.Require().Equal(http.StatusCreated, resp.StatusCode, "Failed to create org: %s", resp.String())

	var org map[string]any
	err := json.Unmarshal(resp.Body, &org)
	s.Require().NoError(err)
	return org["id"].(string)
}

// createProjectViaAPI creates a project via API and returns its ID
func (s *TasksTestSuite) createProjectViaAPI(name, orgID string) string {
	resp := s.Client.POST("/api/projects",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"name":  name,
			"orgId": orgID,
		}),
	)
	s.Require().Equal(http.StatusCreated, resp.StatusCode, "Failed to create project: %s", resp.String())

	var project map[string]any
	err := json.Unmarshal(resp.Body, &project)
	s.Require().NoError(err)
	return project["id"].(string)
}

// createTaskViaDB creates a task directly in the database for testing
// This is necessary because tasks are created internally by the system
func (s *TasksTestSuite) createTaskViaDB(projectID, title, taskType, status string) string {
	s.Require().NotNil(s.DB(), "Database connection required for task creation")

	var taskID string
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.tasks (project_id, title, type, status)
		VALUES (?, ?, ?, ?)
		RETURNING id
	`, projectID, title, taskType, status).Exec(context.Background(), &taskID)
	s.Require().NoError(err)

	return taskID
}

// =============================================================================
// Test: List Tasks (GET /api/tasks)
// =============================================================================

func (s *TasksTestSuite) TestList_RequiresAuth() {
	resp := s.Client.GET("/api/tasks")
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *TasksTestSuite) TestList_RequiresProjectID() {
	resp := s.Client.GET("/api/tasks",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusBadRequest, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)
	s.Contains(body["error"].(map[string]any)["message"], "project_id")
}

func (s *TasksTestSuite) TestList_AcceptsProjectIDHeader() {
	// Create a project
	orgID := s.createOrgViaAPI("Task List Test Org")
	projectID := s.createProjectViaAPI("Task List Test Project", orgID)

	resp := s.Client.GET("/api/tasks",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(projectID),
	)

	s.Equal(http.StatusOK, resp.StatusCode)
}

func (s *TasksTestSuite) TestList_AcceptsProjectIDQueryParam() {
	// Create a project
	orgID := s.createOrgViaAPI("Task List Query Org")
	projectID := s.createProjectViaAPI("Task List Query Project", orgID)

	resp := s.Client.GET("/api/tasks?project_id="+projectID,
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)
}

func (s *TasksTestSuite) TestList_ReturnsEmptyArrayWhenNoTasks() {
	// Create a new project with no tasks
	orgID := s.createOrgViaAPI("Empty Tasks Org")
	projectID := s.createProjectViaAPI("Empty Tasks Project", orgID)

	resp := s.Client.GET("/api/tasks",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(projectID),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	s.Contains(result, "data")
	s.Contains(result, "total")
	s.Equal(float64(0), result["total"])
	s.Empty(result["data"])
}

func (s *TasksTestSuite) TestList_ReturnsTasks() {
	// Create a project and a task
	orgID := s.createOrgViaAPI("Tasks List Org")
	projectID := s.createProjectViaAPI("Tasks List Project", orgID)

	// Create a task via DB
	taskID := s.createTaskViaDB(projectID, "Test Task", "review", "pending")
	s.Require().NotEmpty(taskID)

	resp := s.Client.GET("/api/tasks",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(projectID),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	s.GreaterOrEqual(result["total"].(float64), float64(1))
	tasks := result["data"].([]any)
	s.GreaterOrEqual(len(tasks), 1)

	// Find our task
	found := false
	for _, t := range tasks {
		task := t.(map[string]any)
		if task["id"] == taskID {
			found = true
			s.Equal("Test Task", task["title"])
			s.Equal("review", task["type"])
			s.Equal("pending", task["status"])
			break
		}
	}
	s.True(found, "Should find the created task")
}

func (s *TasksTestSuite) TestList_FiltersByStatus() {
	// Create a project with tasks in different statuses
	orgID := s.createOrgViaAPI("Tasks Filter Org")
	projectID := s.createProjectViaAPI("Tasks Filter Project", orgID)

	// Create tasks with different statuses
	s.createTaskViaDB(projectID, "Pending Task", "review", "pending")
	s.createTaskViaDB(projectID, "Accepted Task", "review", "accepted")

	// Filter by pending status
	resp := s.Client.GET("/api/tasks?status=pending",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(projectID),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	tasks := result["data"].([]any)
	for _, t := range tasks {
		task := t.(map[string]any)
		s.Equal("pending", task["status"], "All returned tasks should be pending")
	}
}

func (s *TasksTestSuite) TestList_FiltersByType() {
	// Create a project with tasks of different types
	orgID := s.createOrgViaAPI("Tasks Type Filter Org")
	projectID := s.createProjectViaAPI("Tasks Type Filter Project", orgID)

	// Create tasks with different types
	s.createTaskViaDB(projectID, "Review Task", "review", "pending")
	s.createTaskViaDB(projectID, "Approval Task", "approval", "pending")

	// Filter by review type
	resp := s.Client.GET("/api/tasks?type=review",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(projectID),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	tasks := result["data"].([]any)
	for _, t := range tasks {
		task := t.(map[string]any)
		s.Equal("review", task["type"], "All returned tasks should be of type review")
	}
}

func (s *TasksTestSuite) TestList_SupportsPagination() {
	// Create a project with multiple tasks
	orgID := s.createOrgViaAPI("Tasks Pagination Org")
	projectID := s.createProjectViaAPI("Tasks Pagination Project", orgID)

	// Create multiple tasks
	for i := 0; i < 5; i++ {
		s.createTaskViaDB(projectID, "Task "+string(rune('A'+i)), "review", "pending")
	}

	// Request with limit
	resp := s.Client.GET("/api/tasks?limit=2",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(projectID),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	tasks := result["data"].([]any)
	s.Equal(2, len(tasks), "Should return only 2 tasks with limit=2")
	s.GreaterOrEqual(result["total"].(float64), float64(5), "Total should be >= 5")
}

// =============================================================================
// Test: Get Task Counts (GET /api/tasks/counts)
// =============================================================================

func (s *TasksTestSuite) TestGetCounts_RequiresAuth() {
	resp := s.Client.GET("/api/tasks/counts")
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *TasksTestSuite) TestGetCounts_RequiresProjectID() {
	resp := s.Client.GET("/api/tasks/counts",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *TasksTestSuite) TestGetCounts_ReturnsZeroCounts() {
	// Create a new project with no tasks
	orgID := s.createOrgViaAPI("Counts Zero Org")
	projectID := s.createProjectViaAPI("Counts Zero Project", orgID)

	resp := s.Client.GET("/api/tasks/counts",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(projectID),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	s.Equal(float64(0), result["pending"])
	s.Equal(float64(0), result["accepted"])
	s.Equal(float64(0), result["rejected"])
	s.Equal(float64(0), result["cancelled"])
}

func (s *TasksTestSuite) TestGetCounts_ReturnsCorrectCounts() {
	// Create a project with tasks in different statuses
	orgID := s.createOrgViaAPI("Counts Test Org")
	projectID := s.createProjectViaAPI("Counts Test Project", orgID)

	// Create tasks with different statuses
	s.createTaskViaDB(projectID, "Pending 1", "review", "pending")
	s.createTaskViaDB(projectID, "Pending 2", "review", "pending")
	s.createTaskViaDB(projectID, "Accepted 1", "review", "accepted")

	resp := s.Client.GET("/api/tasks/counts",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(projectID),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	s.GreaterOrEqual(result["pending"].(float64), float64(2))
	s.GreaterOrEqual(result["accepted"].(float64), float64(1))
}

// =============================================================================
// Test: List All Tasks (GET /api/tasks/all)
// =============================================================================

func (s *TasksTestSuite) TestListAll_RequiresAuth() {
	resp := s.Client.GET("/api/tasks/all")
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *TasksTestSuite) TestListAll_ReturnsTasksAcrossProjects() {
	// Create two projects with tasks
	orgID := s.createOrgViaAPI("ListAll Test Org")
	projectID1 := s.createProjectViaAPI("ListAll Project 1", orgID)
	projectID2 := s.createProjectViaAPI("ListAll Project 2", orgID)

	// Create tasks in both projects
	s.createTaskViaDB(projectID1, "Task in Project 1", "review", "pending")
	s.createTaskViaDB(projectID2, "Task in Project 2", "review", "pending")

	resp := s.Client.GET("/api/tasks/all",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	// Should return tasks from both projects
	tasks := result["data"].([]any)
	projectIDs := make(map[string]bool)
	for _, t := range tasks {
		task := t.(map[string]any)
		projectIDs[task["projectId"].(string)] = true
	}

	s.True(projectIDs[projectID1], "Should include tasks from project 1")
	s.True(projectIDs[projectID2], "Should include tasks from project 2")
}

// =============================================================================
// Test: Get All Counts (GET /api/tasks/all/counts)
// =============================================================================

func (s *TasksTestSuite) TestGetAllCounts_RequiresAuth() {
	resp := s.Client.GET("/api/tasks/all/counts")
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *TasksTestSuite) TestGetAllCounts_ReturnsCountsAcrossProjects() {
	// Create two projects with tasks
	orgID := s.createOrgViaAPI("AllCounts Test Org")
	projectID1 := s.createProjectViaAPI("AllCounts Project 1", orgID)
	projectID2 := s.createProjectViaAPI("AllCounts Project 2", orgID)

	// Create pending tasks in both projects
	s.createTaskViaDB(projectID1, "Pending in Project 1", "review", "pending")
	s.createTaskViaDB(projectID2, "Pending in Project 2", "review", "pending")

	resp := s.Client.GET("/api/tasks/all/counts",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	// Should have at least 2 pending from our created tasks
	s.GreaterOrEqual(result["pending"].(float64), float64(2))
}

// =============================================================================
// Test: Get Task By ID (GET /api/tasks/:id)
// =============================================================================

func (s *TasksTestSuite) TestGetByID_RequiresAuth() {
	resp := s.Client.GET("/api/tasks/some-id")
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *TasksTestSuite) TestGetByID_RequiresProjectID() {
	resp := s.Client.GET("/api/tasks/some-id",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *TasksTestSuite) TestGetByID_ReturnsNotFoundForInvalidID() {
	orgID := s.createOrgViaAPI("GetByID NotFound Org")
	projectID := s.createProjectViaAPI("GetByID NotFound Project", orgID)

	resp := s.Client.GET("/api/tasks/00000000-0000-0000-0000-000000000000",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(projectID),
	)

	s.Equal(http.StatusNotFound, resp.StatusCode)
}

func (s *TasksTestSuite) TestGetByID_ReturnsTask() {
	// Create a project and a task
	orgID := s.createOrgViaAPI("GetByID Test Org")
	projectID := s.createProjectViaAPI("GetByID Test Project", orgID)
	taskID := s.createTaskViaDB(projectID, "Get By ID Task", "review", "pending")

	resp := s.Client.GET("/api/tasks/"+taskID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(projectID),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	data := result["data"].(map[string]any)
	s.Equal(taskID, data["id"])
	s.Equal("Get By ID Task", data["title"])
	s.Equal("review", data["type"])
	s.Equal("pending", data["status"])
}

// =============================================================================
// Test: Resolve Task (POST /api/tasks/:id/resolve)
// =============================================================================

func (s *TasksTestSuite) TestResolve_RequiresAuth() {
	resp := s.Client.POST("/api/tasks/some-id/resolve",
		testutil.WithJSONBody(map[string]any{
			"resolution": "accepted",
		}),
	)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *TasksTestSuite) TestResolve_RequiresProjectID() {
	resp := s.Client.POST("/api/tasks/some-id/resolve",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"resolution": "accepted",
		}),
	)

	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *TasksTestSuite) TestResolve_RequiresValidResolution() {
	orgID := s.createOrgViaAPI("Resolve Invalid Org")
	projectID := s.createProjectViaAPI("Resolve Invalid Project", orgID)
	taskID := s.createTaskViaDB(projectID, "Resolve Invalid Task", "review", "pending")

	resp := s.Client.POST("/api/tasks/"+taskID+"/resolve",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(projectID),
		testutil.WithJSONBody(map[string]any{
			"resolution": "invalid",
		}),
	)

	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *TasksTestSuite) TestResolve_AcceptsTask() {
	orgID := s.createOrgViaAPI("Resolve Accept Org")
	projectID := s.createProjectViaAPI("Resolve Accept Project", orgID)
	taskID := s.createTaskViaDB(projectID, "Accept Task", "review", "pending")

	resp := s.Client.POST("/api/tasks/"+taskID+"/resolve",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(projectID),
		testutil.WithJSONBody(map[string]any{
			"resolution": "accepted",
		}),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	// Verify the task was updated
	getResp := s.Client.GET("/api/tasks/"+taskID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(projectID),
	)

	var result map[string]any
	err := json.Unmarshal(getResp.Body, &result)
	s.NoError(err)

	data := result["data"].(map[string]any)
	s.Equal("accepted", data["status"])
}

func (s *TasksTestSuite) TestResolve_RejectsTask() {
	orgID := s.createOrgViaAPI("Resolve Reject Org")
	projectID := s.createProjectViaAPI("Resolve Reject Project", orgID)
	taskID := s.createTaskViaDB(projectID, "Reject Task", "review", "pending")

	resp := s.Client.POST("/api/tasks/"+taskID+"/resolve",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(projectID),
		testutil.WithJSONBody(map[string]any{
			"resolution":      "rejected",
			"resolutionNotes": "Not approved",
		}),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	// Verify the task was updated
	getResp := s.Client.GET("/api/tasks/"+taskID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(projectID),
	)

	var result map[string]any
	err := json.Unmarshal(getResp.Body, &result)
	s.NoError(err)

	data := result["data"].(map[string]any)
	s.Equal("rejected", data["status"])
	s.Equal("Not approved", data["resolutionNotes"])
}

func (s *TasksTestSuite) TestResolve_CannotResolveAlreadyResolved() {
	orgID := s.createOrgViaAPI("Resolve Already Org")
	projectID := s.createProjectViaAPI("Resolve Already Project", orgID)
	taskID := s.createTaskViaDB(projectID, "Already Resolved Task", "review", "accepted")

	resp := s.Client.POST("/api/tasks/"+taskID+"/resolve",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(projectID),
		testutil.WithJSONBody(map[string]any{
			"resolution": "rejected",
		}),
	)

	s.Equal(http.StatusNotFound, resp.StatusCode)
}

// =============================================================================
// Test: Cancel Task (POST /api/tasks/:id/cancel)
// =============================================================================

func (s *TasksTestSuite) TestCancel_RequiresAuth() {
	resp := s.Client.POST("/api/tasks/some-id/cancel")
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *TasksTestSuite) TestCancel_RequiresProjectID() {
	resp := s.Client.POST("/api/tasks/some-id/cancel",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *TasksTestSuite) TestCancel_CancelsTask() {
	orgID := s.createOrgViaAPI("Cancel Test Org")
	projectID := s.createProjectViaAPI("Cancel Test Project", orgID)
	taskID := s.createTaskViaDB(projectID, "Cancel Task", "review", "pending")

	resp := s.Client.POST("/api/tasks/"+taskID+"/cancel",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(projectID),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	// Verify the task was cancelled
	getResp := s.Client.GET("/api/tasks/"+taskID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(projectID),
	)

	var result map[string]any
	err := json.Unmarshal(getResp.Body, &result)
	s.NoError(err)

	data := result["data"].(map[string]any)
	s.Equal("cancelled", data["status"])
}

func (s *TasksTestSuite) TestCancel_CannotCancelAlreadyResolved() {
	orgID := s.createOrgViaAPI("Cancel Already Org")
	projectID := s.createProjectViaAPI("Cancel Already Project", orgID)
	taskID := s.createTaskViaDB(projectID, "Already Accepted Task", "review", "accepted")

	resp := s.Client.POST("/api/tasks/"+taskID+"/cancel",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(projectID),
	)

	s.Equal(http.StatusNotFound, resp.StatusCode)
}
