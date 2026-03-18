package e2e

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
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

// =============================================================================
// Superadmin Role Tests
// =============================================================================

// SuperadminMeResponse matches the DTO from apps/server/domain/superadmin/dto.go
type SuperadminMeResponse struct {
	IsSuperadmin bool   `json:"isSuperadmin"`
	Role         string `json:"role,omitempty"`
}

func (s *SuperadminTestSuite) TestGetMe_WithFullRole_ReturnsRoleInResponse() {
	// Arrange - create a superadmin with full role
	userID := s.GetUserID("e2e-test-user")
	s.DB.Exec(`
		INSERT INTO core.superadmins (user_id, role, granted_by, granted_at)
		VALUES ($1, 'superadmin_full', $1, NOW())
		ON CONFLICT (user_id) DO UPDATE SET role = 'superadmin_full', revoked_at = NULL
	`, userID)

	// Act
	resp := s.Client.GET("/api/superadmin/me",
		testutil.WithAuth("e2e-test-user"),
	)

	// Assert
	s.Equal(http.StatusOK, resp.StatusCode)

	var result SuperadminMeResponse
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)
	s.True(result.IsSuperadmin, "User should be a superadmin")
	s.Equal("superadmin_full", result.Role, "Role should be superadmin_full")
}

func (s *SuperadminTestSuite) TestGetMe_WithReadonlyRole_ReturnsRoleInResponse() {
	// Arrange - create a superadmin with readonly role
	userID := s.GetUserID("e2e-user-two")
	s.DB.Exec(`
		INSERT INTO core.superadmins (user_id, role, granted_by, granted_at)
		VALUES ($1, 'superadmin_readonly', $1, NOW())
		ON CONFLICT (user_id) DO UPDATE SET role = 'superadmin_readonly', revoked_at = NULL
	`, userID)

	// Act
	resp := s.Client.GET("/api/superadmin/me",
		testutil.WithAuth("e2e-user-two"),
	)

	// Assert
	s.Equal(http.StatusOK, resp.StatusCode)

	var result SuperadminMeResponse
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)
	s.True(result.IsSuperadmin, "User should be a superadmin")
	s.Equal("superadmin_readonly", result.Role, "Role should be superadmin_readonly")
}

func (s *SuperadminTestSuite) TestFullRole_CanAccessWriteEndpoints() {
	// Arrange - create a superadmin with full role and a test user to delete
	adminUserID := s.GetUserID("e2e-test-user")
	targetUserID := s.GetUserID("e2e-user-three")

	s.DB.Exec(`
		INSERT INTO core.superadmins (user_id, role, granted_by, granted_at)
		VALUES ($1, 'superadmin_full', $1, NOW())
		ON CONFLICT (user_id) DO UPDATE SET role = 'superadmin_full', revoked_at = NULL
	`, adminUserID)

	// Act - attempt to delete a user (write operation)
	resp := s.Client.DELETE("/api/superadmin/users/"+targetUserID,
		testutil.WithAuth("e2e-test-user"),
	)

	// Assert - should succeed
	s.Equal(http.StatusOK, resp.StatusCode, "Full role should allow write operations")
}

func (s *SuperadminTestSuite) TestReadonlyRole_CannotAccessWriteEndpoints() {
	// Arrange - create a superadmin with readonly role
	adminUserID := s.GetUserID("e2e-user-two")
	targetUserID := s.GetUserID("e2e-user-three")

	s.DB.Exec(`
		INSERT INTO core.superadmins (user_id, role, granted_by, granted_at)
		VALUES ($1, 'superadmin_readonly', $1, NOW())
		ON CONFLICT (user_id) DO UPDATE SET role = 'superadmin_readonly', revoked_at = NULL
	`, adminUserID)

	// Act - attempt to delete a user (write operation)
	resp := s.Client.DELETE("/api/superadmin/users/"+targetUserID,
		testutil.WithAuth("e2e-user-two"),
	)

	// Assert - should be forbidden
	s.Equal(http.StatusForbidden, resp.StatusCode, "Readonly role should not allow write operations")
}

func (s *SuperadminTestSuite) TestReadonlyRole_CanAccessReadEndpoints() {
	// Arrange - create a superadmin with readonly role
	userID := s.GetUserID("e2e-user-two")
	s.DB.Exec(`
		INSERT INTO core.superadmins (user_id, role, granted_by, granted_at)
		VALUES ($1, 'superadmin_readonly', $1, NOW())
		ON CONFLICT (user_id) DO UPDATE SET role = 'superadmin_readonly', revoked_at = NULL
	`, userID)

	// Act - list users (read operation)
	resp := s.Client.GET("/api/superadmin/users",
		testutil.WithAuth("e2e-user-two"),
	)

	// Assert - should succeed
	s.Equal(http.StatusOK, resp.StatusCode, "Readonly role should allow read operations")
}

func (s *SuperadminTestSuite) TestFullRole_CanAccessReadEndpoints() {
	// Arrange - create a superadmin with full role
	userID := s.GetUserID("e2e-test-user")
	s.DB.Exec(`
		INSERT INTO core.superadmins (user_id, role, granted_by, granted_at)
		VALUES ($1, 'superadmin_full', $1, NOW())
		ON CONFLICT (user_id) DO UPDATE SET role = 'superadmin_full', revoked_at = NULL
	`, userID)

	// Act - list users (read operation)
	resp := s.Client.GET("/api/superadmin/users",
		testutil.WithAuth("e2e-test-user"),
	)

	// Assert - should succeed
	s.Equal(http.StatusOK, resp.StatusCode, "Full role should allow read operations")
}

func (s *SuperadminTestSuite) TestReadonlyRole_MultipleWriteEndpointsDenied() {
	// Arrange - create a superadmin with readonly role
	userID := s.GetUserID("e2e-user-two")
	s.DB.Exec(`
		INSERT INTO core.superadmins (user_id, role, granted_by, granted_at)
		VALUES ($1, 'superadmin_readonly', $1, NOW())
		ON CONFLICT (user_id) DO UPDATE SET role = 'superadmin_readonly', revoked_at = NULL
	`, userID)

	// Test multiple write endpoints
	writeEndpoints := []struct {
		method string
		path   string
	}{
		{"DELETE", "/api/superadmin/users/" + userID},
		{"DELETE", "/api/superadmin/organizations/" + s.OrgID},
		{"DELETE", "/api/superadmin/projects/" + s.ProjectID},
		{"POST", "/api/superadmin/embedding-jobs/delete"},
		{"POST", "/api/superadmin/extraction-jobs/delete"},
	}

	for _, endpoint := range writeEndpoints {
		// Act
		var resp *testutil.HTTPResponse
		if endpoint.method == "DELETE" {
			resp = s.Client.DELETE(endpoint.path,
				testutil.WithAuth("e2e-user-two"),
			)
		} else {
			resp = s.Client.POST(endpoint.path, map[string]interface{}{"ids": []string{"test-id"}},
				testutil.WithAuth("e2e-user-two"),
			)
		}

		// Assert
		s.Equal(http.StatusForbidden, resp.StatusCode,
			"Readonly role should deny %s %s", endpoint.method, endpoint.path)
	}
}

func (s *SuperadminTestSuite) TestRevokedSuperadmin_CannotAccessEndpoints() {
	// Arrange - create and then revoke a superadmin
	userID := s.GetUserID("e2e-test-user")
	s.DB.Exec(`
		INSERT INTO core.superadmins (user_id, role, granted_by, granted_at, revoked_at, revoked_by)
		VALUES ($1, 'superadmin_full', $1, NOW(), NOW(), $1)
		ON CONFLICT (user_id) DO UPDATE 
		SET role = 'superadmin_full', revoked_at = NOW(), revoked_by = $1
	`, userID)

	// Act - attempt to access read endpoint
	resp := s.Client.GET("/api/superadmin/users",
		testutil.WithAuth("e2e-test-user"),
	)

	// Assert - should be forbidden
	s.Equal(http.StatusForbidden, resp.StatusCode, "Revoked superadmin should not have access")
}
