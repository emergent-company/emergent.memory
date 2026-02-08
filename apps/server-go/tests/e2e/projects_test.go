package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/emergent/emergent-core/internal/testutil"
)

// ProjectsTestSuite tests the projects API endpoints
type ProjectsTestSuite struct {
	testutil.BaseSuite
}

func TestProjectsSuite(t *testing.T) {
	suite.Run(t, new(ProjectsTestSuite))
}

func (s *ProjectsTestSuite) SetupSuite() {
	s.SetDBSuffix("projects")
	s.BaseSuite.SetupSuite()
}

// Helper to create an org via API and return its ID
func (s *ProjectsTestSuite) createOrgViaAPI(name string) string {
	resp := s.Client.POST("/api/v2/orgs",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSON(),
		testutil.WithBody(fmt.Sprintf(`{"name": "%s"}`, name)),
	)
	s.Require().Equal(http.StatusCreated, resp.StatusCode)

	var org map[string]any
	err := json.Unmarshal(resp.Body, &org)
	s.Require().NoError(err)
	return org["id"].(string)
}

// Helper to create a project via API and return its ID
func (s *ProjectsTestSuite) createProjectViaAPI(orgID, name string) string {
	resp := s.Client.POST("/api/projects",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSON(),
		testutil.WithBody(fmt.Sprintf(`{"name": "%s", "orgId": "%s"}`, name, orgID)),
	)
	s.Require().Equal(http.StatusCreated, resp.StatusCode)

	var project map[string]any
	err := json.Unmarshal(resp.Body, &project)
	s.Require().NoError(err)
	return project["id"].(string)
}

// =============================================================================
// Test: List Projects
// =============================================================================

func (s *ProjectsTestSuite) TestListProjects_RequiresAuth() {
	resp := s.Client.GET("/api/projects")
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *ProjectsTestSuite) TestListProjects_ReturnsUserProjects() {
	// BaseSuite already creates a project via API
	resp := s.Client.GET("/api/projects",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var projects []map[string]any
	err := json.Unmarshal(resp.Body, &projects)
	s.NoError(err)
	s.GreaterOrEqual(len(projects), 1, "User should see at least the default project")

	// Find our default project
	var found bool
	for _, p := range projects {
		if p["id"] == s.ProjectID {
			found = true
			s.Equal(s.OrgID, p["orgId"])
			break
		}
	}
	s.True(found, "Should find the default project in list")
}

func (s *ProjectsTestSuite) TestListProjects_FilterByOrgId() {
	// Create a new org with a project
	org1ID := s.createOrgViaAPI("Org 1 for Filter")
	project1ID := s.createProjectViaAPI(org1ID, "Project in Org 1")

	// Filter by the new org
	resp := s.Client.GET("/api/projects?orgId="+org1ID,
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var projects []map[string]any
	err := json.Unmarshal(resp.Body, &projects)
	s.NoError(err)
	s.Len(projects, 1)
	s.Equal(project1ID, projects[0]["id"])
}

func (s *ProjectsTestSuite) TestListProjects_InvalidOrgIdReturnsEmpty() {
	resp := s.Client.GET("/api/projects?orgId=invalid-uuid",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var projects []map[string]any
	err := json.Unmarshal(resp.Body, &projects)
	s.NoError(err)
	s.Empty(projects)
}

func (s *ProjectsTestSuite) TestListProjects_WithLimit() {
	// Create an org with multiple projects
	orgID := s.createOrgViaAPI("Org for Limit Test")
	for i := 1; i <= 5; i++ {
		s.createProjectViaAPI(orgID, fmt.Sprintf("Limit Test Project %d", i))
	}

	resp := s.Client.GET("/api/projects?orgId="+orgID+"&limit=3",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var projects []map[string]any
	err := json.Unmarshal(resp.Body, &projects)
	s.NoError(err)
	s.Len(projects, 3)
}

// =============================================================================
// Test: Get Project by ID
// =============================================================================

func (s *ProjectsTestSuite) TestGetProject_RequiresAuth() {
	resp := s.Client.GET("/api/projects/some-id")
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *ProjectsTestSuite) TestGetProject_InvalidUUID() {
	resp := s.Client.GET("/api/projects/invalid-uuid",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusBadRequest, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	errObj, ok := body["error"].(map[string]any)
	s.True(ok)
	s.Equal("invalid-uuid", errObj["code"])
}

func (s *ProjectsTestSuite) TestGetProject_NotFound() {
	resp := s.Client.GET("/api/projects/00000000-0000-0000-0000-000000000000",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusNotFound, resp.StatusCode)
}

func (s *ProjectsTestSuite) TestGetProject_Success() {
	// Use the default project created by BaseSuite
	resp := s.Client.GET("/api/projects/"+s.ProjectID,
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var project map[string]any
	err := json.Unmarshal(resp.Body, &project)
	s.NoError(err)
	s.Equal(s.ProjectID, project["id"])
	s.Equal(s.OrgID, project["orgId"])
}

// =============================================================================
// Test: Create Project
// =============================================================================

func (s *ProjectsTestSuite) TestCreateProject_RequiresAuth() {
	resp := s.Client.POST("/api/projects",
		testutil.WithJSON(),
		testutil.WithBody(fmt.Sprintf(`{"name": "New Project", "orgId": "%s"}`, s.OrgID)),
	)

	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *ProjectsTestSuite) TestCreateProject_Success() {
	resp := s.Client.POST("/api/projects",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSON(),
		testutil.WithBody(fmt.Sprintf(`{"name": "New Project", "orgId": "%s"}`, s.OrgID)),
	)

	s.Equal(http.StatusCreated, resp.StatusCode)

	var project map[string]any
	err := json.Unmarshal(resp.Body, &project)
	s.NoError(err)
	s.NotEmpty(project["id"])
	s.Equal("New Project", project["name"])
	s.Equal(s.OrgID, project["orgId"])

	// Verify the project appears in list
	listResp := s.Client.GET("/api/projects",
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusOK, listResp.StatusCode)

	var projects []map[string]any
	err = json.Unmarshal(listResp.Body, &projects)
	s.NoError(err)

	var found bool
	for _, p := range projects {
		if p["id"] == project["id"] {
			found = true
			break
		}
	}
	s.True(found, "Created project should appear in list")
}

func (s *ProjectsTestSuite) TestCreateProject_TrimsWhitespace() {
	resp := s.Client.POST("/api/projects",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSON(),
		testutil.WithBody(fmt.Sprintf(`{"name": "  Trimmed Name  ", "orgId": "%s"}`, s.OrgID)),
	)

	s.Equal(http.StatusCreated, resp.StatusCode)

	var project map[string]any
	err := json.Unmarshal(resp.Body, &project)
	s.NoError(err)
	s.Equal("Trimmed Name", project["name"])
}

func (s *ProjectsTestSuite) TestCreateProject_EmptyName() {
	resp := s.Client.POST("/api/projects",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSON(),
		testutil.WithBody(fmt.Sprintf(`{"name": "", "orgId": "%s"}`, s.OrgID)),
	)

	s.Equal(http.StatusBadRequest, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	errObj, ok := body["error"].(map[string]any)
	s.True(ok)
	s.Equal("validation-failed", errObj["code"])
}

func (s *ProjectsTestSuite) TestCreateProject_MissingOrgId() {
	resp := s.Client.POST("/api/projects",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSON(),
		testutil.WithBody(`{"name": "New Project"}`),
	)

	s.Equal(http.StatusBadRequest, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	errObj, ok := body["error"].(map[string]any)
	s.True(ok)
	s.Equal("org-required", errObj["code"])
}

func (s *ProjectsTestSuite) TestCreateProject_OrgNotFound() {
	resp := s.Client.POST("/api/projects",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSON(),
		testutil.WithBody(`{"name": "New Project", "orgId": "00000000-0000-0000-0000-000000000000"}`),
	)

	s.Equal(http.StatusBadRequest, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	errObj, ok := body["error"].(map[string]any)
	s.True(ok)
	s.Equal("org-not-found", errObj["code"])
}

func (s *ProjectsTestSuite) TestCreateProject_DuplicateName() {
	// Create first project
	resp := s.Client.POST("/api/projects",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSON(),
		testutil.WithBody(fmt.Sprintf(`{"name": "Duplicate Name", "orgId": "%s"}`, s.OrgID)),
	)
	s.Equal(http.StatusCreated, resp.StatusCode)

	// Try to create second project with same name in same org
	resp = s.Client.POST("/api/projects",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSON(),
		testutil.WithBody(fmt.Sprintf(`{"name": "Duplicate Name", "orgId": "%s"}`, s.OrgID)),
	)

	s.Equal(http.StatusBadRequest, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	errObj, ok := body["error"].(map[string]any)
	s.True(ok)
	s.Equal("duplicate", errObj["code"])
}

func (s *ProjectsTestSuite) TestCreateProject_CreatorBecomesAdmin() {
	resp := s.Client.POST("/api/projects",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSON(),
		testutil.WithBody(fmt.Sprintf(`{"name": "Admin Test Project", "orgId": "%s"}`, s.OrgID)),
	)

	s.Equal(http.StatusCreated, resp.StatusCode)

	var project map[string]any
	err := json.Unmarshal(resp.Body, &project)
	s.NoError(err)
	projectID := project["id"].(string)

	// Verify user can list members (proves they have admin access)
	membersResp := s.Client.GET("/api/projects/"+projectID+"/members",
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusOK, membersResp.StatusCode)

	var members []map[string]any
	err = json.Unmarshal(membersResp.Body, &members)
	s.NoError(err)
	s.Len(members, 1, "Should have exactly 1 member (the creator)")
	s.Equal("project_admin", members[0]["role"])
}

// =============================================================================
// Test: Update Project
// =============================================================================

func (s *ProjectsTestSuite) TestUpdateProject_RequiresAuth() {
	resp := s.Client.PATCH("/api/projects/some-id",
		testutil.WithJSON(),
		testutil.WithBody(`{"name": "Updated Name"}`),
	)

	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *ProjectsTestSuite) TestUpdateProject_InvalidUUID() {
	resp := s.Client.PATCH("/api/projects/invalid-uuid",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSON(),
		testutil.WithBody(`{"name": "Updated Name"}`),
	)

	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *ProjectsTestSuite) TestUpdateProject_NotFound() {
	resp := s.Client.PATCH("/api/projects/00000000-0000-0000-0000-000000000000",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSON(),
		testutil.WithBody(`{"name": "Updated Name"}`),
	)

	s.Equal(http.StatusNotFound, resp.StatusCode)
}

func (s *ProjectsTestSuite) TestUpdateProject_Success() {
	// Create a project to update
	projectID := s.createProjectViaAPI(s.OrgID, "Original Name")

	resp := s.Client.PATCH("/api/projects/"+projectID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSON(),
		testutil.WithBody(`{"name": "Updated Name"}`),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var project map[string]any
	err := json.Unmarshal(resp.Body, &project)
	s.NoError(err)
	s.Equal(projectID, project["id"])
	s.Equal("Updated Name", project["name"])
}

func (s *ProjectsTestSuite) TestUpdateProject_PartialUpdate() {
	// Create a project to update
	projectID := s.createProjectViaAPI(s.OrgID, "Original Name for Partial")

	// Update only kb_purpose
	resp := s.Client.PATCH("/api/projects/"+projectID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSON(),
		testutil.WithBody(`{"kb_purpose": "Test purpose"}`),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var project map[string]any
	err := json.Unmarshal(resp.Body, &project)
	s.NoError(err)
	s.Equal("Original Name for Partial", project["name"]) // Name unchanged
	s.Equal("Test purpose", project["kb_purpose"])
}

func (s *ProjectsTestSuite) TestUpdateProject_EmptyUpdate() {
	// Create a project to update
	projectID := s.createProjectViaAPI(s.OrgID, "Name for Empty Update")

	// Empty update should return current project
	resp := s.Client.PATCH("/api/projects/"+projectID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSON(),
		testutil.WithBody(`{}`),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var project map[string]any
	err := json.Unmarshal(resp.Body, &project)
	s.NoError(err)
	s.Equal("Name for Empty Update", project["name"])
}

// =============================================================================
// Test: Delete Project
// =============================================================================

func (s *ProjectsTestSuite) TestDeleteProject_RequiresAuth() {
	resp := s.Client.DELETE("/api/projects/some-id")
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *ProjectsTestSuite) TestDeleteProject_InvalidUUID() {
	resp := s.Client.DELETE("/api/projects/invalid-uuid",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *ProjectsTestSuite) TestDeleteProject_NotFound() {
	resp := s.Client.DELETE("/api/projects/00000000-0000-0000-0000-000000000000",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusNotFound, resp.StatusCode)
}

func (s *ProjectsTestSuite) TestDeleteProject_Success() {
	// Create a project to delete
	projectID := s.createProjectViaAPI(s.OrgID, "To Delete")

	resp := s.Client.DELETE("/api/projects/"+projectID,
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)
	s.Equal("deleted", body["status"])

	// Verify project is soft-deleted (not visible via GET)
	getResp := s.Client.GET("/api/projects/"+projectID,
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusNotFound, getResp.StatusCode)
}

// =============================================================================
// Test: List Project Members
// =============================================================================

func (s *ProjectsTestSuite) TestListMembers_RequiresAuth() {
	resp := s.Client.GET("/api/projects/some-id/members")
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *ProjectsTestSuite) TestListMembers_ProjectNotFound() {
	resp := s.Client.GET("/api/projects/00000000-0000-0000-0000-000000000000/members",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusNotFound, resp.StatusCode)
}

func (s *ProjectsTestSuite) TestListMembers_Success() {
	// Use the default project - creator should be a member
	resp := s.Client.GET("/api/projects/"+s.ProjectID+"/members",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var members []map[string]any
	err := json.Unmarshal(resp.Body, &members)
	s.NoError(err)
	s.GreaterOrEqual(len(members), 1, "Should have at least 1 member")

	// Find the admin member
	var hasAdmin bool
	for _, m := range members {
		if m["role"] == "project_admin" {
			hasAdmin = true
			break
		}
	}
	s.True(hasAdmin, "Should have a project_admin member")
}

// =============================================================================
// Test: Remove Project Member
// =============================================================================

func (s *ProjectsTestSuite) TestRemoveMember_RequiresAuth() {
	resp := s.Client.DELETE("/api/projects/some-id/members/some-user-id")
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *ProjectsTestSuite) TestRemoveMember_ProjectNotFound() {
	resp := s.Client.DELETE("/api/projects/00000000-0000-0000-0000-000000000000/members/00000000-0000-0000-0000-000000000001",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusNotFound, resp.StatusCode)
}

func (s *ProjectsTestSuite) TestRemoveMember_MemberNotFound() {
	// Create a project
	projectID := s.createProjectViaAPI(s.OrgID, "Remove Member Test")

	// Try to remove a user who is not a member
	resp := s.Client.DELETE("/api/projects/"+projectID+"/members/99999999-9999-9999-9999-999999999999",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusNotFound, resp.StatusCode)
}

func (s *ProjectsTestSuite) TestRemoveMember_CannotRemoveLastAdmin() {
	// Create a project (creator is the only admin)
	projectID := s.createProjectViaAPI(s.OrgID, "Last Admin Test")

	// Get member list to find the user ID
	membersResp := s.Client.GET("/api/projects/"+projectID+"/members",
		testutil.WithAuth("e2e-test-user"),
	)
	s.Require().Equal(http.StatusOK, membersResp.StatusCode)

	var members []map[string]any
	err := json.Unmarshal(membersResp.Body, &members)
	s.Require().NoError(err)
	s.Require().Len(members, 1)
	userID := members[0]["id"].(string)

	// Try to remove the only admin
	resp := s.Client.DELETE("/api/projects/"+projectID+"/members/"+userID,
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusForbidden, resp.StatusCode)

	var body map[string]any
	err = json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	errObj, ok := body["error"].(map[string]any)
	s.True(ok)
	s.Equal("last-admin", errObj["code"])
}
