package suites

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/emergent/api-tests/testutil"
)

// OrgsTestSuite tests the organizations API endpoints.
// Uses external API calls against a running server.
type OrgsTestSuite struct {
	BaseSuite
	createdOrgIDs []string // Track created orgs for cleanup
}

func TestOrgsSuite(t *testing.T) {
	RunSuite(t, new(OrgsTestSuite))
}

// SetupTest runs before each test.
func (s *OrgsTestSuite) SetupTest() {
	s.BaseSuite.SetupTest()
	s.createdOrgIDs = nil
}

// TearDownTest cleans up created resources after each test.
func (s *OrgsTestSuite) TearDownTest() {
	for _, id := range s.createdOrgIDs {
		_, _ = s.Client.DELETE("/api/v2/orgs/"+id, s.AdminAuth())
	}
}

// =============================================================================
// Helpers
// =============================================================================

// createOrg creates an organization via API and tracks for cleanup.
func (s *OrgsTestSuite) createOrg(name string) (string, error) {
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

// =============================================================================
// Test: List Organizations
// =============================================================================

func (s *OrgsTestSuite) TestListOrgs_RequiresAuth() {
	resp, err := s.Client.GET("/api/v2/orgs")
	s.Require().NoError(err)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *OrgsTestSuite) TestListOrgs_ReturnsUserOrgs() {
	// Admin user already has access to the default test org
	resp, err := s.Client.GET("/api/v2/orgs", s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	var orgs []map[string]any
	err = resp.JSON(&orgs)
	s.Require().NoError(err)
	s.GreaterOrEqual(len(orgs), 1, "Should return at least the default test org")
}

func (s *OrgsTestSuite) TestListOrgs_MultipleOrgs() {
	// Create multiple orgs
	for i := 1; i <= 3; i++ {
		_, err := s.createOrg(fmt.Sprintf("Multi Org %d", i))
		s.Require().NoError(err)
	}

	resp, err := s.Client.GET("/api/v2/orgs", s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	var orgs []map[string]any
	err = resp.JSON(&orgs)
	s.Require().NoError(err)
	// At least 3 new orgs + default org
	s.GreaterOrEqual(len(orgs), 3)
}

// =============================================================================
// Test: Get Organization by ID
// =============================================================================

func (s *OrgsTestSuite) TestGetOrg_RequiresAuth() {
	resp, err := s.Client.GET("/api/v2/orgs/" + testutil.DefaultTestOrg.ID)
	s.Require().NoError(err)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *OrgsTestSuite) TestGetOrg_InvalidUUID() {
	resp, err := s.Client.GET("/api/v2/orgs/invalid-uuid", s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *OrgsTestSuite) TestGetOrg_NotFound() {
	notFoundID := "00000000-0000-0000-0000-000000000999"
	resp, err := s.Client.GET("/api/v2/orgs/"+notFoundID, s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusNotFound, resp.StatusCode)
}

func (s *OrgsTestSuite) TestGetOrg_Success() {
	// Create an org
	orgID, err := s.createOrg("Get Test Org")
	s.Require().NoError(err)

	resp, err := s.Client.GET("/api/v2/orgs/"+orgID, s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	var org map[string]any
	err = resp.JSON(&org)
	s.Require().NoError(err)
	s.Equal(orgID, org["id"])
	s.Equal("Get Test Org", org["name"])
}

// =============================================================================
// Test: Create Organization
// =============================================================================

func (s *OrgsTestSuite) TestCreateOrg_RequiresAuth() {
	body := map[string]any{"name": "Unauth Org"}
	resp, err := s.Client.POST("/api/v2/orgs", body)
	s.Require().NoError(err)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *OrgsTestSuite) TestCreateOrg_Success() {
	body := map[string]any{"name": "New Organization"}
	resp, err := s.Client.POST("/api/v2/orgs", body, s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusCreated, resp.StatusCode)

	var org map[string]any
	err = resp.JSON(&org)
	s.Require().NoError(err)
	s.NotEmpty(org["id"])
	s.Equal("New Organization", org["name"])

	// Track for cleanup
	if id, ok := org["id"].(string); ok {
		s.createdOrgIDs = append(s.createdOrgIDs, id)
	}

	// Verify the org appears in list
	listResp, err := s.Client.GET("/api/v2/orgs", s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusOK, listResp.StatusCode)

	var orgs []map[string]any
	err = listResp.JSON(&orgs)
	s.Require().NoError(err)

	found := false
	for _, o := range orgs {
		if o["id"] == org["id"] {
			found = true
			break
		}
	}
	s.True(found, "Created org should appear in list")
}

func (s *OrgsTestSuite) TestCreateOrg_TrimsWhitespace() {
	body := map[string]any{"name": "  Trimmed Name  "}
	resp, err := s.Client.POST("/api/v2/orgs", body, s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusCreated, resp.StatusCode)

	var org map[string]any
	err = resp.JSON(&org)
	s.Require().NoError(err)
	s.Equal("Trimmed Name", org["name"])

	// Track for cleanup
	if id, ok := org["id"].(string); ok {
		s.createdOrgIDs = append(s.createdOrgIDs, id)
	}
}

func (s *OrgsTestSuite) TestCreateOrg_EmptyName() {
	body := map[string]any{"name": ""}
	resp, err := s.Client.POST("/api/v2/orgs", body, s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *OrgsTestSuite) TestCreateOrg_WhitespaceOnlyName() {
	body := map[string]any{"name": "   "}
	resp, err := s.Client.POST("/api/v2/orgs", body, s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *OrgsTestSuite) TestCreateOrg_NameTooLong() {
	// Create a name longer than 120 characters
	longName := strings.Repeat("a", 121)
	body := map[string]any{"name": longName}
	resp, err := s.Client.POST("/api/v2/orgs", body, s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *OrgsTestSuite) TestCreateOrg_MissingName() {
	body := map[string]any{}
	resp, err := s.Client.POST("/api/v2/orgs", body, s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

// =============================================================================
// Test: Update Organization
// =============================================================================

func (s *OrgsTestSuite) TestUpdateOrg_RequiresAuth() {
	body := map[string]any{"name": "Updated Name"}
	resp, err := s.Client.PATCH("/api/v2/orgs/"+testutil.DefaultTestOrg.ID, body)
	s.Require().NoError(err)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *OrgsTestSuite) TestUpdateOrg_InvalidUUID() {
	body := map[string]any{"name": "Updated Name"}
	resp, err := s.Client.PATCH("/api/v2/orgs/invalid-uuid", body, s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *OrgsTestSuite) TestUpdateOrg_NotFound() {
	notFoundID := "00000000-0000-0000-0000-000000000999"
	body := map[string]any{"name": "Updated Name"}
	resp, err := s.Client.PATCH("/api/v2/orgs/"+notFoundID, body, s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusNotFound, resp.StatusCode)
}

func (s *OrgsTestSuite) TestUpdateOrg_Success() {
	// Create an org
	orgID, err := s.createOrg("Original Name")
	s.Require().NoError(err)

	body := map[string]any{"name": "Updated Name"}
	resp, err := s.Client.PATCH("/api/v2/orgs/"+orgID, body, s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	var org map[string]any
	err = resp.JSON(&org)
	s.Require().NoError(err)
	s.Equal(orgID, org["id"])
	s.Equal("Updated Name", org["name"])
}

func (s *OrgsTestSuite) TestUpdateOrg_EmptyUpdate() {
	// Create an org
	orgID, err := s.createOrg("Empty Update Org")
	s.Require().NoError(err)

	// Empty update should still return OK
	body := map[string]any{}
	resp, err := s.Client.PATCH("/api/v2/orgs/"+orgID, body, s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	var org map[string]any
	err = resp.JSON(&org)
	s.Require().NoError(err)
	s.Equal("Empty Update Org", org["name"])
}

// =============================================================================
// Test: Delete Organization
// =============================================================================

func (s *OrgsTestSuite) TestDeleteOrg_RequiresAuth() {
	resp, err := s.Client.DELETE("/api/v2/orgs/" + testutil.DefaultTestOrg.ID)
	s.Require().NoError(err)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *OrgsTestSuite) TestDeleteOrg_InvalidUUID() {
	resp, err := s.Client.DELETE("/api/v2/orgs/invalid-uuid", s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *OrgsTestSuite) TestDeleteOrg_NotFound() {
	notFoundID := "00000000-0000-0000-0000-000000000999"
	resp, err := s.Client.DELETE("/api/v2/orgs/"+notFoundID, s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusNotFound, resp.StatusCode)
}

func (s *OrgsTestSuite) TestDeleteOrg_Success() {
	// Create an org
	orgID, err := s.createOrg("To Delete")
	s.Require().NoError(err)

	resp, err := s.Client.DELETE("/api/v2/orgs/"+orgID, s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	// Verify org is no longer accessible
	getResp, err := s.Client.GET("/api/v2/orgs/"+orgID, s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusNotFound, getResp.StatusCode)

	// Remove from cleanup since it's already deleted
	for i, id := range s.createdOrgIDs {
		if id == orgID {
			s.createdOrgIDs = append(s.createdOrgIDs[:i], s.createdOrgIDs[i+1:]...)
			break
		}
	}
}

// =============================================================================
// Test: List Organization Members
// =============================================================================

func (s *OrgsTestSuite) TestListOrgMembers_RequiresAuth() {
	resp, err := s.Client.GET("/api/v2/orgs/" + testutil.DefaultTestOrg.ID + "/members")
	s.Require().NoError(err)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *OrgsTestSuite) TestListOrgMembers_InvalidUUID() {
	resp, err := s.Client.GET("/api/v2/orgs/invalid-uuid/members", s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *OrgsTestSuite) TestListOrgMembers_OrgNotFound() {
	notFoundID := "00000000-0000-0000-0000-000000000999"
	resp, err := s.Client.GET("/api/v2/orgs/"+notFoundID+"/members", s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusNotFound, resp.StatusCode)
}

func (s *OrgsTestSuite) TestListOrgMembers_Success() {
	// Create an org (creator becomes admin member)
	orgID, err := s.createOrg("Members Test Org")
	s.Require().NoError(err)

	resp, err := s.Client.GET("/api/v2/orgs/"+orgID+"/members", s.AdminAuth())
	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	var members []map[string]any
	err = resp.JSON(&members)
	s.Require().NoError(err)
	s.GreaterOrEqual(len(members), 1, "Creator should be a member")
}
