package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/emergent/emergent-core/internal/testutil"
)

// UserAccessTestSuite tests the user access API endpoints
type UserAccessTestSuite struct {
	testutil.BaseSuite
}

func TestUserAccessSuite(t *testing.T) {
	suite.Run(t, new(UserAccessTestSuite))
}

func (s *UserAccessTestSuite) SetupSuite() {
	s.SetDBSuffix("useraccess")
	s.BaseSuite.SetupSuite()
}

// Helper to create an org via API and return its ID
func (s *UserAccessTestSuite) createOrgViaAPI(name string) string {
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
func (s *UserAccessTestSuite) createProjectViaAPI(orgID, name string) string {
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
// Test: Get Orgs and Projects (Access Tree)
// =============================================================================

func (s *UserAccessTestSuite) TestGetOrgsAndProjects_RequiresAuth() {
	// Request without Authorization header should fail
	resp := s.Client.GET("/user/orgs-and-projects")

	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *UserAccessTestSuite) TestGetOrgsAndProjects_ReturnsOrgStructure() {
	// BaseSuite creates an org via API, so user should see it
	resp := s.Client.GET("/user/orgs-and-projects",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result []map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	s.GreaterOrEqual(len(result), 1, "Should have at least one org")

	// Find our default org
	var foundOrg map[string]any
	for _, org := range result {
		if org["id"] == s.OrgID {
			foundOrg = org
			break
		}
	}
	s.NotNil(foundOrg, "Should find the default org")
	s.NotEmpty(foundOrg["name"])
	s.NotEmpty(foundOrg["role"])
	s.Contains(foundOrg, "projects", "Org should have projects array")
}

func (s *UserAccessTestSuite) TestGetOrgsAndProjects_OrgHasProjects() {
	// Create a new org with a project
	orgID := s.createOrgViaAPI("Org With Projects Test")
	projectID := s.createProjectViaAPI(orgID, "Test Project")

	resp := s.Client.GET("/user/orgs-and-projects",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result []map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	// Find our created org
	var foundOrg map[string]any
	for _, org := range result {
		if org["id"] == orgID {
			foundOrg = org
			break
		}
	}
	s.Require().NotNil(foundOrg, "Should find the created org")

	projects, ok := foundOrg["projects"].([]any)
	s.True(ok, "projects should be an array")
	s.Len(projects, 1)

	project := projects[0].(map[string]any)
	s.Equal(projectID, project["id"])
	s.Equal("Test Project", project["name"])
	s.Equal(orgID, project["orgId"])
	s.NotEmpty(project["role"])
}

func (s *UserAccessTestSuite) TestGetOrgsAndProjects_MultipleOrgs() {
	// Create additional orgs
	s.createOrgViaAPI("Multi Org Test 1")
	s.createOrgViaAPI("Multi Org Test 2")

	resp := s.Client.GET("/user/orgs-and-projects",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result []map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	// Should have at least 3 orgs (default + 2 created)
	s.GreaterOrEqual(len(result), 3, "Should return at least 3 orgs")
}

func (s *UserAccessTestSuite) TestGetOrgsAndProjects_MultipleProjects() {
	// Create org with multiple projects
	orgID := s.createOrgViaAPI("Multi Project Org")
	s.createProjectViaAPI(orgID, "Project 1")
	s.createProjectViaAPI(orgID, "Project 2")
	s.createProjectViaAPI(orgID, "Project 3")

	resp := s.Client.GET("/user/orgs-and-projects",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result []map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	// Find the org
	var foundOrg map[string]any
	for _, org := range result {
		if org["id"] == orgID {
			foundOrg = org
			break
		}
	}
	s.Require().NotNil(foundOrg, "Should find the org")

	projects, ok := foundOrg["projects"].([]any)
	s.True(ok)
	s.Len(projects, 3, "Org should have 3 projects")
}

func (s *UserAccessTestSuite) TestGetOrgsAndProjects_RoleIsIncluded() {
	// Create an org - user should be admin (creator)
	orgID := s.createOrgViaAPI("Role Test Org")

	resp := s.Client.GET("/user/orgs-and-projects",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result []map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	// Find the org
	var foundOrg map[string]any
	for _, org := range result {
		if org["id"] == orgID {
			foundOrg = org
			break
		}
	}
	s.Require().NotNil(foundOrg, "Should find the org")
	s.NotEmpty(foundOrg["role"], "Role should be included")
}

func (s *UserAccessTestSuite) TestGetOrgsAndProjects_ProjectRoleIsIncluded() {
	// Create org and project - user should be admin (creator) for both
	orgID := s.createOrgViaAPI("Project Role Test Org")
	projectID := s.createProjectViaAPI(orgID, "Project Role Test")

	resp := s.Client.GET("/user/orgs-and-projects",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result []map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	// Find the org
	var foundOrg map[string]any
	for _, org := range result {
		if org["id"] == orgID {
			foundOrg = org
			break
		}
	}
	s.Require().NotNil(foundOrg, "Should find the org")

	projects, ok := foundOrg["projects"].([]any)
	s.Require().True(ok)
	s.Require().Len(projects, 1)

	project := projects[0].(map[string]any)
	s.Equal(projectID, project["id"])
	s.NotEmpty(project["role"], "Project role should be included")
}
