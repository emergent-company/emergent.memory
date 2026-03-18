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
	s.SkipIfExternalServer("requires direct database access")

	// Arrange - create a superadmin with full role
	userID := testutil.AdminUser.ID
	_, err := s.DB().NewRaw(`
		INSERT INTO core.superadmins (user_id, role, granted_by, granted_at)
		VALUES (?, 'superadmin_full', ?, NOW())
		ON CONFLICT (user_id) DO UPDATE SET role = 'superadmin_full', revoked_at = NULL
	`, userID, userID).Exec(s.Ctx)
	s.Require().NoError(err)

	// Act
	resp := s.Client.GET("/api/superadmin/me",
		testutil.WithAuth("e2e-test-user"),
	)

	// Assert
	s.Equal(http.StatusOK, resp.StatusCode)

	var result SuperadminMeResponse
	err = json.Unmarshal(resp.Body, &result)
	s.NoError(err)
	s.True(result.IsSuperadmin, "User should be a superadmin")
	s.Equal("superadmin_full", result.Role, "Role should be superadmin_full")
}

func (s *SuperadminTestSuite) TestGetMe_WithReadonlyRole_ReturnsRoleInResponse() {
	s.SkipIfExternalServer("requires direct database access")

	// Arrange - create a superadmin with readonly role
	// Use WithScopeUser since we need a different user than AdminUser
	userID := testutil.WithScopeUser.ID
	_, err := s.DB().NewRaw(`
		INSERT INTO core.superadmins (user_id, role, granted_by, granted_at)
		VALUES (?, 'superadmin_readonly', ?, NOW())
		ON CONFLICT (user_id) DO UPDATE SET role = 'superadmin_readonly', revoked_at = NULL
	`, userID, userID).Exec(s.Ctx)
	s.Require().NoError(err)

	// Act - use with-scope token which maps to WithScopeUser
	resp := s.Client.GET("/api/superadmin/me",
		testutil.WithAuth("with-scope"),
	)

	// Assert
	s.Equal(http.StatusOK, resp.StatusCode)

	var result SuperadminMeResponse
	err = json.Unmarshal(resp.Body, &result)
	s.NoError(err)
	s.True(result.IsSuperadmin, "User should be a superadmin")
	s.Equal("superadmin_readonly", result.Role, "Role should be superadmin_readonly")
}

func (s *SuperadminTestSuite) TestFullRole_CanAccessWriteEndpoints() {
	s.SkipIfExternalServer("requires direct database access")

	// Arrange - create a superadmin with full role
	adminUserID := testutil.AdminUser.ID
	targetUserID := testutil.WithScopeUser.ID

	_, err := s.DB().NewRaw(`
		INSERT INTO core.superadmins (user_id, role, granted_by, granted_at)
		VALUES (?, 'superadmin_full', ?, NOW())
		ON CONFLICT (user_id) DO UPDATE SET role = 'superadmin_full', revoked_at = NULL
	`, adminUserID, adminUserID).Exec(s.Ctx)
	s.Require().NoError(err)

	// Act - attempt to delete a user (write operation)
	resp := s.Client.DELETE("/api/superadmin/users/"+targetUserID,
		testutil.WithAuth("e2e-test-user"),
	)

	// Assert - should succeed
	s.Equal(http.StatusOK, resp.StatusCode, "Full role should allow write operations")
}

func (s *SuperadminTestSuite) TestReadonlyRole_CannotAccessWriteEndpoints() {
	s.SkipIfExternalServer("requires direct database access")

	// Arrange - create a superadmin with readonly role
	adminUserID := testutil.AllScopesUser.ID
	targetUserID := testutil.WithScopeUser.ID

	_, err := s.DB().NewRaw(`
		INSERT INTO core.superadmins (user_id, role, granted_by, granted_at)
		VALUES (?, 'superadmin_readonly', ?, NOW())
		ON CONFLICT (user_id) DO UPDATE SET role = 'superadmin_readonly', revoked_at = NULL
	`, adminUserID, adminUserID).Exec(s.Ctx)
	s.Require().NoError(err)

	// Act - attempt to delete a user (write operation)
	// Use all-scopes token which maps to AllScopesUser
	resp := s.Client.DELETE("/api/superadmin/users/"+targetUserID,
		testutil.WithAuth("all-scopes"),
	)

	// Assert - should be forbidden
	s.Equal(http.StatusForbidden, resp.StatusCode, "Readonly role should not allow write operations")
}

func (s *SuperadminTestSuite) TestReadonlyRole_CanAccessReadEndpoints() {
	s.SkipIfExternalServer("requires direct database access")

	// Arrange - create a superadmin with readonly role
	userID := testutil.GraphReadUser.ID
	_, err := s.DB().NewRaw(`
		INSERT INTO core.superadmins (user_id, role, granted_by, granted_at)
		VALUES (?, 'superadmin_readonly', ?, NOW())
		ON CONFLICT (user_id) DO UPDATE SET role = 'superadmin_readonly', revoked_at = NULL
	`, userID, userID).Exec(s.Ctx)
	s.Require().NoError(err)

	// Act - list users (read operation)
	// Use graph-read token which maps to GraphReadUser
	resp := s.Client.GET("/api/superadmin/users",
		testutil.WithAuth("graph-read"),
	)

	// Assert - should succeed
	s.Equal(http.StatusOK, resp.StatusCode, "Readonly role should allow read operations")
}

func (s *SuperadminTestSuite) TestFullRole_CanAccessReadEndpoints() {
	s.SkipIfExternalServer("requires direct database access")

	// Arrange - create a superadmin with full role
	userID := testutil.AdminUser.ID
	_, err := s.DB().NewRaw(`
		INSERT INTO core.superadmins (user_id, role, granted_by, granted_at)
		VALUES (?, 'superadmin_full', ?, NOW())
		ON CONFLICT (user_id) DO UPDATE SET role = 'superadmin_full', revoked_at = NULL
	`, userID, userID).Exec(s.Ctx)
	s.Require().NoError(err)

	// Act - list users (read operation)
	resp := s.Client.GET("/api/superadmin/users",
		testutil.WithAuth("e2e-test-user"),
	)

	// Assert - should succeed
	s.Equal(http.StatusOK, resp.StatusCode, "Full role should allow read operations")
}

func (s *SuperadminTestSuite) TestReadonlyRole_MultipleWriteEndpointsDenied() {
	s.SkipIfExternalServer("requires direct database access")

	// Arrange - create a superadmin with readonly role
	userID := testutil.ReadOnlyUser.ID
	_, err := s.DB().NewRaw(`
		INSERT INTO core.superadmins (user_id, role, granted_by, granted_at)
		VALUES (?, 'superadmin_readonly', ?, NOW())
		ON CONFLICT (user_id) DO UPDATE SET role = 'superadmin_readonly', revoked_at = NULL
	`, userID, userID).Exec(s.Ctx)
	s.Require().NoError(err)

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
				testutil.WithAuth("read-only"),
			)
		} else {
			resp = s.Client.POST(endpoint.path,
				testutil.WithAuth("read-only"),
				testutil.WithJSONBody(map[string]interface{}{"ids": []string{"test-id"}}),
			)
		}

		// Assert
		s.Equal(http.StatusForbidden, resp.StatusCode,
			"Readonly role should deny %s %s", endpoint.method, endpoint.path)
	}
}

func (s *SuperadminTestSuite) TestRevokedSuperadmin_CannotAccessEndpoints() {
	s.SkipIfExternalServer("requires direct database access")

	// Arrange - create and then revoke a superadmin
	userID := testutil.NoScopeUser.ID
	_, err := s.DB().NewRaw(`
		INSERT INTO core.superadmins (user_id, role, granted_by, granted_at, revoked_at, revoked_by)
		VALUES (?, 'superadmin_full', ?, NOW(), NOW(), ?)
		ON CONFLICT (user_id) DO UPDATE 
		SET role = 'superadmin_full', revoked_at = NOW(), revoked_by = ?
	`, userID, userID, userID, userID).Exec(s.Ctx)
	s.Require().NoError(err)

	// Act - attempt to access read endpoint
	// Use no-scope token which maps to NoScopeUser
	resp := s.Client.GET("/api/superadmin/users",
		testutil.WithAuth("no-scope"),
	)

	// Assert - should be forbidden
	s.Equal(http.StatusForbidden, resp.StatusCode, "Revoked superadmin should not have access")
}
