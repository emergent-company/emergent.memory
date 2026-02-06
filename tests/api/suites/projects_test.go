package suites

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/emergent/api-tests/testutil"
	"github.com/google/uuid"
)

// ProjectsTestSuite tests the projects API endpoints.
// Uses external API calls against a running server.
type ProjectsTestSuite struct {
	BaseSuite
	createdOrgIDs     []string // Track created orgs for cleanup
	createdProjectIDs []string // Track created projects for cleanup
}

func TestProjectsSuite(t *testing.T) {
	RunSuite(t, new(ProjectsTestSuite))
}

// SetupTest runs before each test.
func (s *ProjectsTestSuite) SetupTest() {
	s.BaseSuite.SetupTest()
	s.createdOrgIDs = nil
	s.createdProjectIDs = nil
}

// TearDownTest cleans up created resources after each test.
func (s *ProjectsTestSuite) TearDownTest() {
	// Clean up projects first (due to FK constraints)
	for _, id := range s.createdProjectIDs {
		_, _ = s.Client.DELETE("/api/v2/projects/"+id, s.AdminAuth())
	}
	// Clean up orgs
	for _, id := range s.createdOrgIDs {
		_, _ = s.Client.DELETE("/api/v2/orgs/"+id, s.AdminAuth())
	}
}

// =============================================================================
// Helpers
// =============================================================================

// createOrg creates an organization via API and tracks for cleanup.
func (s *ProjectsTestSuite) createOrg(name string) (string, error) {
	body := map[string]any{"name": name}
	resp, err := s.Client.POST("/api/v2/orgs", body, s.AdminAuth())
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("failed to create org: status %d, body: %s", resp.StatusCode, resp.BodyString())
	}

	var org map[string]any
	if err := resp.JSON(&org); err != nil {
		return "", err
	}

	orgID, ok := org["id"].(string)
	if !ok {
		return "", fmt.Errorf("org response missing id")
	}
	s.createdOrgIDs = append(s.createdOrgIDs, orgID)
	return orgID, nil
}

// createProject creates a project via API and tracks for cleanup.
func (s *ProjectsTestSuite) createProject(orgID, name string) (string, error) {
	body := map[string]any{"name": name, "orgId": orgID}
	resp, err := s.Client.POST("/api/v2/projects", body, s.AdminAuth())
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("failed to create project: status %d, body: %s", resp.StatusCode, resp.BodyString())
	}

	var project map[string]any
	if err := resp.JSON(&project); err != nil {
		return "", err
	}

	projectID, ok := project["id"].(string)
	if !ok {
		return "", fmt.Errorf("project response missing id")
	}
	s.createdProjectIDs = append(s.createdProjectIDs, projectID)
	return projectID, nil
}

// addProjectMember adds a user to a project with the specified role.
func (s *ProjectsTestSuite) addProjectMember(projectID, userID, role string) error {
	return testutil.CreateProjectMembership(s.Ctx, s.DB, projectID, userID, role)
}

// getAdminUserID returns the admin user's actual ID from the database.
func (s *ProjectsTestSuite) getAdminUserID() string {
	id, err := testutil.GetUserIDByZitadelID(s.Ctx, s.DB, testutil.AdminUser.ZitadelUserID)
	s.Require().NoError(err)
	return id
}

// =============================================================================
// Test: List Projects
// =============================================================================

func (s *ProjectsTestSuite) TestListProjects_RequiresAuth() {
	resp, err := s.Client.GET("/api/v2/projects")
	s.Require().NoError(err)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *ProjectsTestSuite) TestListProjects_ReturnsUserProjects() {
	// Admin user already has access to the default test project
	resp, err := s.Client.GET("/api/v2/projects", s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	// Should get a valid response
	var projects []map[string]any
	err = resp.JSON(&projects)
	s.Require().NoError(err)
	s.GreaterOrEqual(len(projects), 1, "Should return at least the default test project")
}

func (s *ProjectsTestSuite) TestListProjects_FilterByOrgId() {
	// Create two orgs with projects
	org1ID, err := s.createOrg("Filter Org 1")
	s.Require().NoError(err)
	org2ID, err := s.createOrg("Filter Org 2")
	s.Require().NoError(err)

	project1ID, err := s.createProject(org1ID, "Project in Org1")
	s.Require().NoError(err)
	_, err = s.createProject(org2ID, "Project in Org2")
	s.Require().NoError(err)

	// Filter by org1 should only return project1
	resp, err := s.Client.GET("/api/v2/projects?orgId="+org1ID, s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	var projects []map[string]any
	err = resp.JSON(&projects)
	s.Require().NoError(err)
	s.Len(projects, 1)
	s.Equal(project1ID, projects[0]["id"])
}

func (s *ProjectsTestSuite) TestListProjects_WithLimit() {
	// Create an org with multiple projects
	orgID, err := s.createOrg("Limit Test Org")
	s.Require().NoError(err)

	for i := 1; i <= 5; i++ {
		_, err := s.createProject(orgID, fmt.Sprintf("Limit Project %d", i))
		s.Require().NoError(err)
	}

	// Request with limit=3
	resp, err := s.Client.GET("/api/v2/projects?orgId="+orgID+"&limit=3", s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	var projects []map[string]any
	err = resp.JSON(&projects)
	s.Require().NoError(err)
	s.Len(projects, 3)
}

// =============================================================================
// Test: Get Project by ID
// =============================================================================

func (s *ProjectsTestSuite) TestGetProject_RequiresAuth() {
	resp, err := s.Client.GET("/api/v2/projects/" + testutil.DefaultTestProject.ID)
	s.Require().NoError(err)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *ProjectsTestSuite) TestGetProject_InvalidUUID() {
	resp, err := s.Client.GET("/api/v2/projects/invalid-uuid", s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *ProjectsTestSuite) TestGetProject_NotFound() {
	notFoundID := "00000000-0000-0000-0000-000000000999"
	resp, err := s.Client.GET("/api/v2/projects/"+notFoundID, s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusNotFound, resp.StatusCode)
}

func (s *ProjectsTestSuite) TestGetProject_Success() {
	// Create org and project
	orgID, err := s.createOrg("Get Test Org")
	s.Require().NoError(err)
	projectID, err := s.createProject(orgID, "Get Test Project")
	s.Require().NoError(err)

	resp, err := s.Client.GET("/api/v2/projects/"+projectID, s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	var project map[string]any
	err = resp.JSON(&project)
	s.Require().NoError(err)
	s.Equal(projectID, project["id"])
	s.Equal("Get Test Project", project["name"])
	s.Equal(orgID, project["orgId"])
}

// =============================================================================
// Test: Create Project
// =============================================================================

func (s *ProjectsTestSuite) TestCreateProject_RequiresAuth() {
	body := map[string]any{"name": "Unauth Project", "orgId": uuid.New().String()}
	resp, err := s.Client.POST("/api/v2/projects", body)
	s.Require().NoError(err)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *ProjectsTestSuite) TestCreateProject_Success() {
	// Create org first
	orgID, err := s.createOrg("Create Test Org")
	s.Require().NoError(err)

	body := map[string]any{"name": "New Test Project", "orgId": orgID}
	resp, err := s.Client.POST("/api/v2/projects", body, s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusCreated, resp.StatusCode)

	var project map[string]any
	err = resp.JSON(&project)
	s.Require().NoError(err)
	s.NotEmpty(project["id"])
	s.Equal("New Test Project", project["name"])
	s.Equal(orgID, project["orgId"])

	// Track for cleanup
	if id, ok := project["id"].(string); ok {
		s.createdProjectIDs = append(s.createdProjectIDs, id)
	}
}

func (s *ProjectsTestSuite) TestCreateProject_EmptyName() {
	// Create org first
	orgID, err := s.createOrg("Empty Name Test Org")
	s.Require().NoError(err)

	body := map[string]any{"name": "", "orgId": orgID}
	resp, err := s.Client.POST("/api/v2/projects", body, s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *ProjectsTestSuite) TestCreateProject_MissingOrgId() {
	body := map[string]any{"name": "Missing Org Project"}
	resp, err := s.Client.POST("/api/v2/projects", body, s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *ProjectsTestSuite) TestCreateProject_OrgNotFound() {
	nonExistentOrgID := "00000000-0000-0000-0000-000000000999"
	body := map[string]any{"name": "Orphan Project", "orgId": nonExistentOrgID}
	resp, err := s.Client.POST("/api/v2/projects", body, s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *ProjectsTestSuite) TestCreateProject_DuplicateName() {
	// Create org
	orgID, err := s.createOrg("Duplicate Test Org")
	s.Require().NoError(err)

	// Create first project
	_, err = s.createProject(orgID, "Duplicate Name")
	s.Require().NoError(err)

	// Try to create second project with same name in same org
	body := map[string]any{"name": "Duplicate Name", "orgId": orgID}
	resp, err := s.Client.POST("/api/v2/projects", body, s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *ProjectsTestSuite) TestCreateProject_TrimsWhitespace() {
	// Create org
	orgID, err := s.createOrg("Whitespace Org")
	s.Require().NoError(err)

	body := map[string]any{"name": "  Trimmed Name  ", "orgId": orgID}
	resp, err := s.Client.POST("/api/v2/projects", body, s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusCreated, resp.StatusCode)

	var project map[string]any
	err = resp.JSON(&project)
	s.Require().NoError(err)
	s.Equal("Trimmed Name", project["name"])

	// Track for cleanup
	if id, ok := project["id"].(string); ok {
		s.createdProjectIDs = append(s.createdProjectIDs, id)
	}
}

// =============================================================================
// Test: Update Project
// =============================================================================

func (s *ProjectsTestSuite) TestUpdateProject_RequiresAuth() {
	body := map[string]any{"name": "Updated Name"}
	resp, err := s.Client.PATCH("/api/v2/projects/"+testutil.DefaultTestProject.ID, body)
	s.Require().NoError(err)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *ProjectsTestSuite) TestUpdateProject_InvalidUUID() {
	body := map[string]any{"name": "Updated Name"}
	resp, err := s.Client.PATCH("/api/v2/projects/invalid-uuid", body, s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *ProjectsTestSuite) TestUpdateProject_NotFound() {
	notFoundID := "00000000-0000-0000-0000-000000000999"
	body := map[string]any{"name": "Updated Name"}
	resp, err := s.Client.PATCH("/api/v2/projects/"+notFoundID, body, s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusNotFound, resp.StatusCode)
}

func (s *ProjectsTestSuite) TestUpdateProject_Success() {
	// Create org and project
	orgID, err := s.createOrg("Update Test Org")
	s.Require().NoError(err)
	projectID, err := s.createProject(orgID, "Original Name")
	s.Require().NoError(err)

	body := map[string]any{"name": "Updated Name"}
	resp, err := s.Client.PATCH("/api/v2/projects/"+projectID, body, s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	var project map[string]any
	err = resp.JSON(&project)
	s.Require().NoError(err)
	s.Equal(projectID, project["id"])
	s.Equal("Updated Name", project["name"])
}

func (s *ProjectsTestSuite) TestUpdateProject_PartialUpdate() {
	// Create org and project
	orgID, err := s.createOrg("Partial Update Org")
	s.Require().NoError(err)
	projectID, err := s.createProject(orgID, "Partial Update Project")
	s.Require().NoError(err)

	// Update only kb_purpose
	body := map[string]any{"kb_purpose": "Test purpose"}
	resp, err := s.Client.PATCH("/api/v2/projects/"+projectID, body, s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	var project map[string]any
	err = resp.JSON(&project)
	s.Require().NoError(err)
	// Name should remain unchanged
	s.Equal("Partial Update Project", project["name"])
	s.Equal("Test purpose", project["kb_purpose"])
}

func (s *ProjectsTestSuite) TestUpdateProject_EmptyUpdate() {
	// Create org and project
	orgID, err := s.createOrg("Empty Update Org")
	s.Require().NoError(err)
	projectID, err := s.createProject(orgID, "Empty Update Project")
	s.Require().NoError(err)

	// Empty update should still return OK
	body := map[string]any{}
	resp, err := s.Client.PATCH("/api/v2/projects/"+projectID, body, s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	var project map[string]any
	err = resp.JSON(&project)
	s.Require().NoError(err)
	// Name should remain unchanged
	s.Equal("Empty Update Project", project["name"])
}

// =============================================================================
// Test: Delete Project
// =============================================================================

func (s *ProjectsTestSuite) TestDeleteProject_RequiresAuth() {
	resp, err := s.Client.DELETE("/api/v2/projects/" + testutil.DefaultTestProject.ID)
	s.Require().NoError(err)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *ProjectsTestSuite) TestDeleteProject_InvalidUUID() {
	resp, err := s.Client.DELETE("/api/v2/projects/invalid-uuid", s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *ProjectsTestSuite) TestDeleteProject_NotFound() {
	notFoundID := "00000000-0000-0000-0000-000000000999"
	resp, err := s.Client.DELETE("/api/v2/projects/"+notFoundID, s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusNotFound, resp.StatusCode)
}

func (s *ProjectsTestSuite) TestDeleteProject_Success() {
	// Create org and project
	orgID, err := s.createOrg("Delete Test Org")
	s.Require().NoError(err)
	projectID, err := s.createProject(orgID, "To Delete")
	s.Require().NoError(err)

	resp, err := s.Client.DELETE("/api/v2/projects/"+projectID, s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	// Verify project is no longer accessible
	getResp, err := s.Client.GET("/api/v2/projects/"+projectID, s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusNotFound, getResp.StatusCode)

	// Remove from cleanup since it's already deleted
	for i, id := range s.createdProjectIDs {
		if id == projectID {
			s.createdProjectIDs = append(s.createdProjectIDs[:i], s.createdProjectIDs[i+1:]...)
			break
		}
	}
}

// =============================================================================
// Test: List Project Members
// =============================================================================

func (s *ProjectsTestSuite) TestListMembers_RequiresAuth() {
	resp, err := s.Client.GET("/api/v2/projects/" + testutil.DefaultTestProject.ID + "/members")
	s.Require().NoError(err)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *ProjectsTestSuite) TestListMembers_InvalidUUID() {
	resp, err := s.Client.GET("/api/v2/projects/invalid-uuid/members", s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *ProjectsTestSuite) TestListMembers_ProjectNotFound() {
	notFoundID := "00000000-0000-0000-0000-000000000999"
	resp, err := s.Client.GET("/api/v2/projects/"+notFoundID+"/members", s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusNotFound, resp.StatusCode)
}

func (s *ProjectsTestSuite) TestListMembers_Success() {
	// Create org and project
	orgID, err := s.createOrg("Members Test Org")
	s.Require().NoError(err)
	projectID, err := s.createProject(orgID, "Members Test Project")
	s.Require().NoError(err)

	resp, err := s.Client.GET("/api/v2/projects/"+projectID+"/members", s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	// Creator should be a member (project_admin)
	var members []map[string]any
	err = resp.JSON(&members)
	s.Require().NoError(err)
	s.GreaterOrEqual(len(members), 1)
}

// =============================================================================
// Test: Remove Project Member
// =============================================================================

func (s *ProjectsTestSuite) TestRemoveMember_RequiresAuth() {
	userID := uuid.New().String()
	resp, err := s.Client.DELETE("/api/v2/projects/" + testutil.DefaultTestProject.ID + "/members/" + userID)
	s.Require().NoError(err)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *ProjectsTestSuite) TestRemoveMember_ProjectNotFound() {
	notFoundID := "00000000-0000-0000-0000-000000000999"
	userID := uuid.New().String()
	resp, err := s.Client.DELETE("/api/v2/projects/"+notFoundID+"/members/"+userID, s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusNotFound, resp.StatusCode)
}

func (s *ProjectsTestSuite) TestRemoveMember_MemberNotFound() {
	// Create org and project
	orgID, err := s.createOrg("Remove Member Org")
	s.Require().NoError(err)
	projectID, err := s.createProject(orgID, "Remove Member Project")
	s.Require().NoError(err)

	// Try to remove a user who is not a member
	nonMemberID := "99999999-9999-9999-9999-999999999999"
	resp, err := s.Client.DELETE("/api/v2/projects/"+projectID+"/members/"+nonMemberID, s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusNotFound, resp.StatusCode)
}

func (s *ProjectsTestSuite) TestRemoveMember_Success() {
	// Create org and project
	orgID, err := s.createOrg("Remove Success Org")
	s.Require().NoError(err)
	projectID, err := s.createProject(orgID, "Remove Success Project")
	s.Require().NoError(err)

	// Add a second admin via database
	secondAdminID := testutil.AllScopesUser.ID
	err = s.addProjectMember(projectID, secondAdminID, "project_admin")
	s.Require().NoError(err)

	// Remove the second admin
	resp, err := s.Client.DELETE("/api/v2/projects/"+projectID+"/members/"+secondAdminID, s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)
}

func (s *ProjectsTestSuite) TestRemoveMember_CannotRemoveLastAdmin() {
	// Create org and project (admin is the only member)
	orgID, err := s.createOrg("Last Admin Org")
	s.Require().NoError(err)
	projectID, err := s.createProject(orgID, "Last Admin Project")
	s.Require().NoError(err)

	// Get admin user ID
	adminID := s.getAdminUserID()

	// Try to remove the only admin - should fail
	resp, err := s.Client.DELETE("/api/v2/projects/"+projectID+"/members/"+adminID, s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusForbidden, resp.StatusCode)
}
