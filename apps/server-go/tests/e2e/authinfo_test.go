package e2e

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent/internal/testutil"
)

// AuthInfoTestSuite tests the GET /api/auth/me endpoint
type AuthInfoTestSuite struct {
	testutil.BaseSuite
}

func TestAuthInfoSuite(t *testing.T) {
	suite.Run(t, new(AuthInfoTestSuite))
}

func (s *AuthInfoTestSuite) SetupSuite() {
	s.SetDBSuffix("authinfo")
	s.BaseSuite.SetupSuite()
}

// TestAuthMeReturnsUserInfo verifies that a valid token returns user identity.
// This exercises the account-confirmation step used by `emergent register`.
func (s *AuthInfoTestSuite) TestAuthMeReturnsUserInfo() {
	resp := s.Client.GET("/api/auth/me", testutil.WithAuth("e2e-test-user"))

	s.Require().Equal(http.StatusOK, resp.StatusCode, "Expected 200 from /api/auth/me, got: %s", resp.String())

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.Require().NoError(err)

	// user_id must be present and non-empty
	userID, ok := body["user_id"].(string)
	s.True(ok, "user_id should be a string")
	s.NotEmpty(userID, "user_id should not be empty")

	// type field must be present
	tokenType, ok := body["type"].(string)
	s.True(ok, "type should be a string")
	s.NotEmpty(tokenType, "type should not be empty")
}

// TestAuthMeReturns401WithoutToken verifies that the endpoint rejects unauthenticated requests.
func (s *AuthInfoTestSuite) TestAuthMeReturns401WithoutToken() {
	resp := s.Client.GET("/api/auth/me")

	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

// TestIssuerEndpointIsPublic verifies that GET /api/auth/issuer is accessible
// without authentication and returns a valid JSON response.
func (s *AuthInfoTestSuite) TestIssuerEndpointIsPublic() {
	resp := s.Client.GET("/api/auth/issuer") // no auth token

	s.Require().Equal(http.StatusOK, resp.StatusCode, "Expected 200 from /api/auth/issuer without auth")

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.Require().NoError(err, "Response should be valid JSON")

	// The response must contain a "standalone" boolean field
	_, ok := body["standalone"]
	s.True(ok, "response should contain 'standalone' field")
}
