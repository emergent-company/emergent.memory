package e2e

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/emergent/emergent-core/internal/testutil"
	"github.com/emergent/emergent-core/pkg/auth"
)

// AuthTestSuite tests authentication and authorization
type AuthTestSuite struct {
	testutil.BaseSuite
}

func TestAuthSuite(t *testing.T) {
	suite.Run(t, new(AuthTestSuite))
}

func (s *AuthTestSuite) SetupSuite() {
	s.SetDBSuffix("auth")
	s.BaseSuite.SetupSuite()
}

// =============================================================================
// Test: Missing Authentication
// =============================================================================

func (s *AuthTestSuite) TestMissingAuth() {
	// Request without Authorization header should fail
	resp := s.Client.GET("/api/v2/test/me")

	s.Equal(http.StatusUnauthorized, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	// Check error format matches NestJS
	errObj, ok := body["error"].(map[string]any)
	s.True(ok, "Expected error object in response")
	s.Equal("missing_token", errObj["code"])
}

// =============================================================================
// Test: Static Test Tokens (Development Mode)
// =============================================================================

func (s *AuthTestSuite) TestE2ETokenPattern() {
	// e2e-test-user maps to the AdminUser fixture (test-admin-user)
	resp := s.Client.GET("/api/v2/test/me", testutil.WithAuth("e2e-test-user"))

	s.Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	// Check user info - e2e-test-user maps to test-admin-user
	s.Equal("test-admin-user", body["sub"])
	s.Equal(testutil.AdminUser.ID, body["id"])

	// Should have all scopes
	scopes, ok := body["scopes"].([]any)
	s.True(ok)
	s.GreaterOrEqual(len(scopes), 10, "e2e token should have many scopes")
}

func (s *AuthTestSuite) TestWithScopeToken() {
	// "with-scope" token should have specific scopes
	resp := s.Client.GET("/api/v2/test/me", testutil.WithAuth("with-scope"))

	s.Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	s.Equal("test-user-with-scope", body["sub"])

	// Check specific scopes are present
	scopes, ok := body["scopes"].([]any)
	s.True(ok)
	scopeSet := make(map[string]bool)
	for _, sc := range scopes {
		scopeSet[sc.(string)] = true
	}
	s.True(scopeSet["documents:read"], "Should have documents:read")
	s.True(scopeSet["documents:write"], "Should have documents:write")
	s.True(scopeSet["project:read"], "Should have project:read")
}

func (s *AuthTestSuite) TestNoScopeToken() {
	// "no-scope" token should work but have no scopes
	resp := s.Client.GET("/api/v2/test/me", testutil.WithAuth("no-scope"))

	s.Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	s.Equal("test-user-no-scope", body["sub"])

	// Should have empty scopes
	scopes, ok := body["scopes"].([]any)
	s.True(ok)
	s.Len(scopes, 0, "no-scope token should have no scopes")
}

func (s *AuthTestSuite) TestAllScopesToken() {
	// "all-scopes" token should have all available scopes
	resp := s.Client.GET("/api/v2/test/me", testutil.WithAuth("all-scopes"))

	s.Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	s.Equal("test-user-all-scopes", body["sub"])

	// Should have all scopes
	scopes, ok := body["scopes"].([]any)
	s.True(ok)
	allScopes := auth.GetAllScopes()
	s.Len(scopes, len(allScopes), "all-scopes token should have all scopes")
}

// =============================================================================
// Test: Scope Validation
// =============================================================================

func (s *AuthTestSuite) TestScopeRequired_HasScope() {
	// User with documents:read scope should access /scoped endpoint
	resp := s.Client.GET("/api/v2/test/scoped", testutil.WithAuth("with-scope"))

	s.Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)
	s.Equal("You have documents:read scope", body["message"])
}

func (s *AuthTestSuite) TestScopeRequired_MissingScope() {
	// User without documents:read scope should be forbidden
	resp := s.Client.GET("/api/v2/test/scoped", testutil.WithAuth("no-scope"))

	s.Equal(http.StatusForbidden, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	errObj, ok := body["error"].(map[string]any)
	s.True(ok)
	s.Equal("forbidden", errObj["code"])
	s.Equal("Insufficient permissions", errObj["message"])

	// Check missing scopes in details
	details, ok := errObj["details"].(map[string]any)
	s.True(ok)
	missing, ok := details["missing"].([]any)
	s.True(ok)
	s.Contains(missing, "documents:read")
}

// =============================================================================
// Test: Project ID Header
// =============================================================================

func (s *AuthTestSuite) TestProjectIDRequired_HasProjectID() {
	// Request with X-Project-ID should work
	resp := s.Client.GET("/api/v2/test/project",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID("test-project-123"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)
	s.Equal("test-project-123", body["projectId"])
}

func (s *AuthTestSuite) TestProjectIDRequired_MissingProjectID() {
	// Request without X-Project-ID should fail with 400
	resp := s.Client.GET("/api/v2/test/project",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *AuthTestSuite) TestHeaderExtraction() {
	// Both X-Project-ID and X-Org-ID should be extracted
	resp := s.Client.GET("/api/v2/test/me",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID("project-123"),
		testutil.WithOrgID("org-456"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	s.Equal("project-123", body["projectId"])
	s.Equal("org-456", body["orgId"])
}

// =============================================================================
// Test: API Tokens (emt_* prefix)
// =============================================================================

func (s *AuthTestSuite) TestAPIToken_Valid() {
	// Create API token via API
	resp := s.Client.POST("/api/v2/projects/"+s.ProjectID+"/tokens",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"name":   "Test API Token",
			"scopes": []string{"data:read", "data:write"},
		}),
	)

	s.Require().Equal(http.StatusCreated, resp.StatusCode, "Failed to create token: %s", resp.String())

	var tokenResult map[string]any
	err := json.Unmarshal(resp.Body, &tokenResult)
	s.Require().NoError(err)

	token := tokenResult["token"].(string)
	s.Require().True(len(token) > 0, "Token should be returned")

	// Use the API token
	resp = s.Client.GET("/api/v2/test/me", testutil.WithAuth(token))

	s.Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	err = json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	// Should have the token's scopes
	scopesArr, ok := body["scopes"].([]any)
	s.True(ok)
	s.Len(scopesArr, 2)
}

func (s *AuthTestSuite) TestAPIToken_Expired() {
	// Note: Cannot create expired tokens via API - tokens are created with future expiration
	// This test would require direct DB access to set a past expiration date
	s.T().Skip("Test requires creating expired tokens which cannot be done via API")
}

func (s *AuthTestSuite) TestAPIToken_Deleted() {
	// Create and then revoke an API token via API
	resp := s.Client.POST("/api/v2/projects/"+s.ProjectID+"/tokens",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"name":   "Token to Revoke",
			"scopes": []string{"data:read"},
		}),
	)

	s.Require().Equal(http.StatusCreated, resp.StatusCode, "Failed to create token: %s", resp.String())

	var tokenResult map[string]any
	err := json.Unmarshal(resp.Body, &tokenResult)
	s.Require().NoError(err)

	token := tokenResult["token"].(string)
	tokenID := tokenResult["id"].(string)

	// Revoke the token via API
	resp = s.Client.DELETE("/api/v2/projects/"+s.ProjectID+"/tokens/"+tokenID,
		testutil.WithAuth("e2e-test-user"),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "Failed to revoke token: %s", resp.String())

	// Use the revoked token - should fail with 401
	resp = s.Client.GET("/api/v2/test/me", testutil.WithAuth(token))

	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *AuthTestSuite) TestAPIToken_Invalid() {
	// Use a token that doesn't exist
	resp := s.Client.GET("/api/v2/test/me", testutil.WithAuth("emt_nonexistent_token"))

	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

// =============================================================================
// Test: Invalid Auth Header Formats
// =============================================================================

func (s *AuthTestSuite) TestInvalidAuthHeader_NoBearer() {
	// Auth header without "Bearer " prefix
	resp := s.Client.GET("/api/v2/test/me", testutil.WithRawAuth("invalid-token"))

	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *AuthTestSuite) TestInvalidAuthHeader_EmptyBearer() {
	// Auth header with empty token after Bearer
	resp := s.Client.GET("/api/v2/test/me", testutil.WithRawAuth("Bearer "))

	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *AuthTestSuite) TestInvalidToken() {
	// Random invalid token (not matching any pattern)
	resp := s.Client.GET("/api/v2/test/me", testutil.WithAuth("random-invalid-token"))

	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

// =============================================================================
// Test: Token from Query Parameter
// =============================================================================

func (s *AuthTestSuite) TestTokenFromQueryParam() {
	// Token can be passed via ?token= query parameter (for SSE endpoints)
	// e2e-query-token maps to test-admin-user
	resp := s.Client.GET("/api/v2/test/me?token=e2e-query-token")

	s.Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)
	s.Equal("test-admin-user", body["sub"])
}

// =============================================================================
// Test: Cached Introspection
// =============================================================================

// =============================================================================
// Test: Auth Errors on Real Endpoints (ported from security.auth-errors.e2e.spec.ts)
// These tests verify auth behavior on actual API endpoints, not test endpoints
// =============================================================================

func (s *AuthTestSuite) TestDocuments_MissingAuth_Returns401() {
	// Request without Authorization header should fail with 401
	resp := s.Client.POST("/api/v2/documents",
		testutil.WithProjectID("test-project-id"),
		testutil.WithJSONBody(map[string]any{
			"filename": "test.txt",
			"content":  "test content",
		}),
	)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *AuthTestSuite) TestDocuments_MalformedToken_Returns401() {
	// Request with malformed token should fail with 401
	resp := s.Client.POST("/api/v2/documents",
		testutil.WithRawAuth("Bearer !!!broken!!!"),
		testutil.WithProjectID("test-project-id"),
		testutil.WithJSONBody(map[string]any{
			"filename": "test.txt",
			"content":  "test content",
		}),
	)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *AuthTestSuite) TestDocuments_NoScopeToken_Returns403() {
	// Request with no-scope token should fail with 403 (scope enforcement)
	resp := s.Client.POST("/api/v2/documents",
		testutil.WithAuth("no-scope"),
		testutil.WithProjectID("test-project-id"),
		testutil.WithJSONBody(map[string]any{
			"filename": "test.txt",
			"content":  "test content",
		}),
	)
	s.Equal(http.StatusForbidden, resp.StatusCode)
}

func (s *AuthTestSuite) TestDocuments_MissingProjectHeader_Returns400() {
	// Request without X-Project-ID should fail with 400
	resp := s.Client.POST("/api/v2/documents",
		testutil.WithAuth("all-scopes"),
		testutil.WithJSONBody(map[string]any{
			"filename": "test.txt",
			"content":  "test content",
		}),
	)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

// =============================================================================
// Test: Cached Introspection
// =============================================================================

func (s *AuthTestSuite) TestCachedIntrospection() {
	// This test verifies that token caching works correctly.
	// Since we can't directly manipulate the cache via API, we test indirectly:
	// Making multiple requests with the same token should work consistently.
	// Create a valid API token via API
	resp := s.Client.POST("/api/v2/projects/"+s.ProjectID+"/tokens",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"name":   "Cache Test Token",
			"scopes": []string{"data:read"},
		}),
	)

	s.Require().Equal(http.StatusCreated, resp.StatusCode, "Failed to create token: %s", resp.String())

	var tokenResult map[string]any
	err := json.Unmarshal(resp.Body, &tokenResult)
	s.Require().NoError(err)

	token := tokenResult["token"].(string)

	// Make multiple requests with the same token - should all succeed
	// This tests that caching doesn't break repeated requests
	for i := 0; i < 3; i++ {
		resp = s.Client.GET("/api/v2/test/me", testutil.WithAuth(token))
		s.Equal(http.StatusOK, resp.StatusCode, "Request %d should succeed", i+1)

		var body map[string]any
		err = json.Unmarshal(resp.Body, &body)
		s.NoError(err)

		// Check scopes are consistent
		scopes, ok := body["scopes"].([]any)
		s.True(ok)
		s.Len(scopes, 1)
		s.Equal("data:read", scopes[0])
	}
}
