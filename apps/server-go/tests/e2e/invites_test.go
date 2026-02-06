package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/emergent/emergent-core/internal/testutil"
)

// InvitesTestSuite tests the invites API endpoints
type InvitesTestSuite struct {
	testutil.BaseSuite
}

func TestInvitesSuite(t *testing.T) {
	suite.Run(t, new(InvitesTestSuite))
}

func (s *InvitesTestSuite) SetupSuite() {
	s.SetDBSuffix("invites")
	s.BaseSuite.SetupSuite()
}

// createOrgViaAPI creates an org via API and returns its ID
func (s *InvitesTestSuite) createOrgViaAPI(name string) string {
	resp := s.Client.POST("/api/v2/orgs",
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
func (s *InvitesTestSuite) createProjectViaAPI(name, orgID string) string {
	resp := s.Client.POST("/api/v2/projects",
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

// createInviteViaAPI creates an invite via API and returns the invite
func (s *InvitesTestSuite) createInviteViaAPI(email, orgID, projectID, role string) map[string]any {
	body := map[string]any{
		"email": email,
		"orgId": orgID,
		"role":  role,
	}
	if projectID != "" {
		body["projectId"] = projectID
	}

	resp := s.Client.POST("/api/v2/invites",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(body),
	)
	s.Require().Equal(http.StatusCreated, resp.StatusCode, "Failed to create invite: %s", resp.String())

	var invite map[string]any
	err := json.Unmarshal(resp.Body, &invite)
	s.Require().NoError(err)
	return invite
}

// =============================================================================
// Test: List Pending Invites
// =============================================================================

func (s *InvitesTestSuite) TestListPending_RequiresAuth() {
	// Request without Authorization header should fail
	resp := s.Client.GET("/api/v2/invites/pending")

	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *InvitesTestSuite) TestListPending_EmptyArrayWhenNoInvites() {
	// User with no pending invites should get empty array
	resp := s.Client.GET("/api/v2/invites/pending",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result []map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	s.Empty(result, "Should return empty array when no pending invites")
}

func (s *InvitesTestSuite) TestListPending_WithInvite() {
	// Create an org via API
	orgID := s.createOrgViaAPI("Test Org for Invite")

	// Create a pending invite for the test user via API
	// Valid roles are: org_admin, project_admin, project_user
	s.createInviteViaAPI(testutil.AdminUser.Email, orgID, "", "org_admin")

	resp := s.Client.GET("/api/v2/invites/pending",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result []map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	// Should have one pending invite
	s.GreaterOrEqual(len(result), 1, "Should return at least one pending invite")

	// Find our invite
	found := false
	for _, invite := range result {
		if invite["organizationId"] == orgID && invite["role"] == "org_admin" {
			found = true
			break
		}
	}
	s.True(found, "Should find our created invite")
}

func (s *InvitesTestSuite) TestListPending_InviteStructure() {
	// Create org and project for the invite
	orgID := s.createOrgViaAPI(fmt.Sprintf("Structured Org %d", time.Now().UnixNano()))
	projectID := s.createProjectViaAPI("Test Project", orgID)

	// Create a pending invite with project via API
	// Valid roles are: org_admin, project_admin, project_user
	s.createInviteViaAPI(testutil.AdminUser.Email, orgID, projectID, "project_user")

	resp := s.Client.GET("/api/v2/invites/pending",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result []map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	s.GreaterOrEqual(len(result), 1)

	// Find our invite
	var invite map[string]any
	for _, inv := range result {
		if inv["organizationId"] == orgID && inv["projectId"] == projectID {
			invite = inv
			break
		}
	}
	s.Require().NotNil(invite, "Should find our created invite")

	// Verify structure
	s.Contains(invite, "id", "Invite should have id")
	s.Contains(invite, "organizationId", "Invite should have organizationId")
	s.Contains(invite, "role", "Invite should have role")
	s.Contains(invite, "token", "Invite should have token")
	s.Contains(invite, "createdAt", "Invite should have createdAt")

	// Optional fields when project invite
	s.Contains(invite, "projectId", "Project invite should have projectId")
	s.Equal(projectID, invite["projectId"])
}

func (s *InvitesTestSuite) TestListPending_ExcludesExpiredInvites() {
	// This test requires creating expired invites which cannot be done via API
	// (API creates invites with future expiration dates)
	// Skip this test for API-only mode
	s.T().Skip("Test requires creating expired invites which cannot be done via API")
}

func (s *InvitesTestSuite) TestListPending_ExcludesAcceptedInvites() {
	// Skip in in-process mode because invite acceptance requires kb.org_memberships table
	// which is not part of the test schema
	if !s.IsExternal() {
		s.T().Skip("Test requires kb.org_memberships table - skipping in in-process mode")
	}

	// Create an org via API
	orgID := s.createOrgViaAPI(fmt.Sprintf("Accepted Org %d", time.Now().UnixNano()))

	// Create a pending invite
	// Valid roles are: org_admin, project_admin, project_user
	invite := s.createInviteViaAPI(testutil.AdminUser.Email, orgID, "", "org_admin")
	token := invite["token"].(string)

	// Accept the invite via API
	resp := s.Client.POST("/api/v2/invites/accept",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"token": token,
		}),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode)

	// List pending invites
	resp = s.Client.GET("/api/v2/invites/pending",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result []map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	// Accepted invite should not be included
	for _, inv := range result {
		s.NotEqual(invite["id"], inv["id"], "Should not include accepted invite")
	}
}

func (s *InvitesTestSuite) TestListPending_CaseInsensitiveEmail() {
	// Note: This test assumes email matching is case-insensitive in the API
	// The invite is created with the actual email, so case sensitivity depends on implementation
	s.T().Skip("Test requires creating invites with different case emails - API normalizes emails")
}

func (s *InvitesTestSuite) TestListPending_MultipleInvites() {
	// Create two orgs so we can have multiple distinct invites
	orgID1 := s.createOrgViaAPI(fmt.Sprintf("Multi Org 1 %d", time.Now().UnixNano()))
	orgID2 := s.createOrgViaAPI(fmt.Sprintf("Multi Org 2 %d", time.Now().UnixNano()))

	// Create pending invites via API for different orgs
	// Valid roles are: org_admin, project_admin, project_user
	s.createInviteViaAPI(testutil.AdminUser.Email, orgID1, "", "org_admin")
	s.createInviteViaAPI(testutil.AdminUser.Email, orgID2, "", "org_admin")

	resp := s.Client.GET("/api/v2/invites/pending",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result []map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	// Count invites for these orgs
	count := 0
	for _, inv := range result {
		if inv["organizationId"] == orgID1 || inv["organizationId"] == orgID2 {
			count++
		}
	}
	s.GreaterOrEqual(count, 2, "Should return at least 2 pending invites for these orgs")
}

func (s *InvitesTestSuite) TestListPending_UserWithNoEmailsReturnsEmpty() {
	// Create an org
	orgID := s.createOrgViaAPI(fmt.Sprintf("Some Org %d", time.Now().UnixNano()))

	// Create an invite for a specific email (not e2e-other-user's email)
	// Valid roles are: org_admin, project_admin, project_user
	s.createInviteViaAPI("other@example.com", orgID, "", "org_admin")

	// e2e-other-user should not see this invite since it's for a different email
	resp := s.Client.GET("/api/v2/invites/pending",
		testutil.WithAuth("e2e-other-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result []map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	// User should not see invites for other emails
	for _, inv := range result {
		s.NotEqual(orgID, inv["organizationId"], "User should not see invites for other emails")
	}
}
