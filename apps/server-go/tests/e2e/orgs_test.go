package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/emergent/emergent-core/internal/testutil"
)

// OrgsTestSuite tests the organizations API endpoints
type OrgsTestSuite struct {
	testutil.BaseSuite
}

func TestOrgsSuite(t *testing.T) {
	suite.Run(t, new(OrgsTestSuite))
}

func (s *OrgsTestSuite) SetupSuite() {
	s.SetDBSuffix("orgs")
	s.BaseSuite.SetupSuite()
}

// =============================================================================
// Test: List Organizations
// =============================================================================

func (s *OrgsTestSuite) TestListOrgs_RequiresAuth() {
	resp := s.Client.GET("/api/v2/orgs")
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *OrgsTestSuite) TestListOrgs_ReturnsUserOrgs() {
	// BaseSuite already creates an org via API, user should see it
	resp := s.Client.GET("/api/v2/orgs",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var orgs []map[string]any
	err := json.Unmarshal(resp.Body, &orgs)
	s.NoError(err)
	s.GreaterOrEqual(len(orgs), 1, "User should see at least the default org")

	// Find our default org
	var found bool
	for _, org := range orgs {
		if org["id"] == s.OrgID {
			found = true
			break
		}
	}
	s.True(found, "Should find the default org in list")
}

func (s *OrgsTestSuite) TestListOrgs_MultipleOrgs() {
	// Create additional orgs via API
	for i := 1; i <= 2; i++ {
		resp := s.Client.POST("/api/v2/orgs",
			testutil.WithAuth("e2e-test-user"),
			testutil.WithJSON(),
			testutil.WithBody(fmt.Sprintf(`{"name": "Additional Org %d"}`, i)),
		)
		s.Equal(http.StatusCreated, resp.StatusCode)
	}

	resp := s.Client.GET("/api/v2/orgs",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var orgs []map[string]any
	err := json.Unmarshal(resp.Body, &orgs)
	s.NoError(err)
	s.GreaterOrEqual(len(orgs), 3, "Should have at least 3 orgs (default + 2 created)")
}

// =============================================================================
// Test: Get Organization by ID
// =============================================================================

func (s *OrgsTestSuite) TestGetOrg_RequiresAuth() {
	resp := s.Client.GET("/api/v2/orgs/some-id")
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *OrgsTestSuite) TestGetOrg_NotFound() {
	resp := s.Client.GET("/api/v2/orgs/00000000-0000-0000-0000-000000000000",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusNotFound, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	errObj, ok := body["error"].(map[string]any)
	s.True(ok)
	s.Equal("not_found", errObj["code"])
}

func (s *OrgsTestSuite) TestGetOrg_Success() {
	// Use the default org created by BaseSuite
	resp := s.Client.GET("/api/v2/orgs/"+s.OrgID,
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var org map[string]any
	err := json.Unmarshal(resp.Body, &org)
	s.NoError(err)
	s.Equal(s.OrgID, org["id"])
	s.NotEmpty(org["name"])
}

// =============================================================================
// Test: Create Organization
// =============================================================================

func (s *OrgsTestSuite) TestCreateOrg_RequiresAuth() {
	resp := s.Client.POST("/api/v2/orgs",
		testutil.WithJSON(),
		testutil.WithBody(`{"name": "New Org"}`),
	)

	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *OrgsTestSuite) TestCreateOrg_Success() {
	resp := s.Client.POST("/api/v2/orgs",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSON(),
		testutil.WithBody(`{"name": "New Organization"}`),
	)

	s.Equal(http.StatusCreated, resp.StatusCode)

	var org map[string]any
	err := json.Unmarshal(resp.Body, &org)
	s.NoError(err)
	s.NotEmpty(org["id"])
	s.Equal("New Organization", org["name"])

	// Verify the org appears in list
	listResp := s.Client.GET("/api/v2/orgs",
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusOK, listResp.StatusCode)

	var orgs []map[string]any
	err = json.Unmarshal(listResp.Body, &orgs)
	s.NoError(err)

	// Find our created org
	var found bool
	for _, o := range orgs {
		if o["id"] == org["id"] {
			found = true
			break
		}
	}
	s.True(found, "Created org should appear in list")
}

func (s *OrgsTestSuite) TestCreateOrg_TrimsWhitespace() {
	resp := s.Client.POST("/api/v2/orgs",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSON(),
		testutil.WithBody(`{"name": "  Trimmed Name  "}`),
	)

	s.Equal(http.StatusCreated, resp.StatusCode)

	var org map[string]any
	err := json.Unmarshal(resp.Body, &org)
	s.NoError(err)
	s.Equal("Trimmed Name", org["name"])
}

func (s *OrgsTestSuite) TestCreateOrg_EmptyName() {
	resp := s.Client.POST("/api/v2/orgs",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSON(),
		testutil.WithBody(`{"name": ""}`),
	)

	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *OrgsTestSuite) TestCreateOrg_WhitespaceOnlyName() {
	resp := s.Client.POST("/api/v2/orgs",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSON(),
		testutil.WithBody(`{"name": "   "}`),
	)

	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *OrgsTestSuite) TestCreateOrg_NameTooLong() {
	// Create a name longer than 120 characters
	longName := make([]byte, 121)
	for i := range longName {
		longName[i] = 'a'
	}

	resp := s.Client.POST("/api/v2/orgs",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSON(),
		testutil.WithBody(fmt.Sprintf(`{"name": "%s"}`, string(longName))),
	)

	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *OrgsTestSuite) TestCreateOrg_InvalidJSON() {
	resp := s.Client.POST("/api/v2/orgs",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSON(),
		testutil.WithBody(`{invalid json`),
	)

	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *OrgsTestSuite) TestCreateOrg_MissingName() {
	resp := s.Client.POST("/api/v2/orgs",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSON(),
		testutil.WithBody(`{}`),
	)

	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *OrgsTestSuite) TestCreateOrg_CreatorBecomesAdmin() {
	// Create org
	resp := s.Client.POST("/api/v2/orgs",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSON(),
		testutil.WithBody(`{"name": "Admin Test Org"}`),
	)

	s.Equal(http.StatusCreated, resp.StatusCode)

	var org map[string]any
	err := json.Unmarshal(resp.Body, &org)
	s.NoError(err)

	// Verify user can access org (proves they have membership)
	getResp := s.Client.GET("/api/v2/orgs/"+org["id"].(string),
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusOK, getResp.StatusCode)

	// User should see this org in their list
	listResp := s.Client.GET("/api/v2/orgs",
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusOK, listResp.StatusCode)

	var orgs []map[string]any
	err = json.Unmarshal(listResp.Body, &orgs)
	s.NoError(err)

	var found bool
	for _, o := range orgs {
		if o["id"] == org["id"] {
			found = true
			break
		}
	}
	s.True(found, "Creator should see org in their list (has membership)")
}

// =============================================================================
// Test: Delete Organization
// =============================================================================

func (s *OrgsTestSuite) TestDeleteOrg_RequiresAuth() {
	resp := s.Client.DELETE("/api/v2/orgs/some-id")
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *OrgsTestSuite) TestDeleteOrg_NotFound() {
	resp := s.Client.DELETE("/api/v2/orgs/00000000-0000-0000-0000-000000000000",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusNotFound, resp.StatusCode)
}

func (s *OrgsTestSuite) TestDeleteOrg_Success() {
	// Create an org to delete
	createResp := s.Client.POST("/api/v2/orgs",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSON(),
		testutil.WithBody(`{"name": "To Delete"}`),
	)
	s.Equal(http.StatusCreated, createResp.StatusCode)

	var org map[string]any
	err := json.Unmarshal(createResp.Body, &org)
	s.NoError(err)
	orgID := org["id"].(string)

	// Delete it
	resp := s.Client.DELETE("/api/v2/orgs/"+orgID,
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	err = json.Unmarshal(resp.Body, &body)
	s.NoError(err)
	s.Equal("deleted", body["status"])

	// Verify org is gone
	getResp := s.Client.GET("/api/v2/orgs/"+orgID,
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusNotFound, getResp.StatusCode)
}
