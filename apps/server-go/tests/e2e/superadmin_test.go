package e2e

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/emergent/emergent-core/internal/testutil"
)

// SuperadminTestSuite tests superadmin endpoints
type SuperadminTestSuite struct {
	testutil.BaseSuite
}

func TestSuperadminSuite(t *testing.T) {
	suite.Run(t, new(SuperadminTestSuite))
}

func (s *SuperadminTestSuite) SetupSuite() {
	s.SetDBSuffix("superadmin")
	s.BaseSuite.SetupSuite()
}

// =============================================================================
// GET /api/superadmin/me - Get current user's superadmin status
// =============================================================================

func (s *SuperadminTestSuite) TestGetMe_RequiresAuth() {
	// Act - request without auth
	resp := s.Client.GET("/api/superadmin/me")

	// Assert - should require authentication
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *SuperadminTestSuite) TestGetMe_ReturnsNullForRegularUser() {
	// Act - request as regular user
	resp := s.Client.GET("/api/superadmin/me",
		testutil.WithAuth("e2e-test-user"),
	)

	// Assert - should return 200 with null body
	s.Equal(http.StatusOK, resp.StatusCode)

	// The response should be null (not a superadmin)
	var result interface{}
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)
	s.Nil(result, "Regular user should not be a superadmin")
}

func (s *SuperadminTestSuite) TestGetMe_ResponseContentType() {
	// Act
	resp := s.Client.GET("/api/superadmin/me",
		testutil.WithAuth("e2e-test-user"),
	)

	// Assert - should return JSON content type
	s.Equal(http.StatusOK, resp.StatusCode)
	s.Contains(resp.Headers.Get("Content-Type"), "application/json")
}

func (s *SuperadminTestSuite) TestGetMe_DifferentUsersGetSameResult() {
	// Act - test with different users
	users := []string{"e2e-test-user", "e2e-user-two", "e2e-user-three"}

	for _, user := range users {
		resp := s.Client.GET("/api/superadmin/me",
			testutil.WithAuth(user),
		)

		// Assert - all users should get 200 with null
		s.Equal(http.StatusOK, resp.StatusCode, "User %s should get 200", user)

		var result interface{}
		err := json.Unmarshal(resp.Body, &result)
		s.NoError(err)
		s.Nil(result, "User %s should not be a superadmin", user)
	}
}

func (s *SuperadminTestSuite) TestGetMe_WithInvalidToken() {
	// Act - request with invalid token
	resp := s.Client.GET("/api/superadmin/me",
		testutil.WithAuth("invalid-token-that-does-not-exist"),
	)

	// Assert - should return unauthorized
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *SuperadminTestSuite) TestGetMe_WithEmptyToken() {
	// Act - request with empty token
	resp := s.Client.GET("/api/superadmin/me",
		testutil.WithRawAuth("Bearer "),
	)

	// Assert - should return unauthorized
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *SuperadminTestSuite) TestGetMe_WithMalformedAuthHeader() {
	// Act - request with malformed auth header (no Bearer prefix)
	resp := s.Client.GET("/api/superadmin/me",
		testutil.WithRawAuth("e2e-test-user"),
	)

	// Assert - should return unauthorized
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *SuperadminTestSuite) TestGetMe_DoesNotRequireProjectID() {
	// Act - request without project ID header (should still work)
	resp := s.Client.GET("/api/superadmin/me",
		testutil.WithAuth("e2e-test-user"),
		// Note: no WithProjectID
	)

	// Assert - should succeed without project ID
	s.Equal(http.StatusOK, resp.StatusCode)
}

func (s *SuperadminTestSuite) TestGetMe_DoesNotRequireOrgID() {
	// Act - request without org ID header (should still work)
	resp := s.Client.GET("/api/superadmin/me",
		testutil.WithAuth("e2e-test-user"),
		// Note: no WithOrgID
	)

	// Assert - should succeed without org ID
	s.Equal(http.StatusOK, resp.StatusCode)
}

func (s *SuperadminTestSuite) TestGetMe_IgnoresProjectIDHeader() {
	// Act - request with project ID header (should be ignored)
	resp := s.Client.GET("/api/superadmin/me",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	// Assert - should still return null (project ID doesn't affect superadmin status)
	s.Equal(http.StatusOK, resp.StatusCode)

	var result interface{}
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)
	s.Nil(result)
}
