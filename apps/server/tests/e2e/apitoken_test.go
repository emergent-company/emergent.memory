package e2e

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/domain/apitoken"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

type ApiTokenSuite struct {
	testutil.BaseSuite
}

func (s *ApiTokenSuite) SetupSuite() {
	s.SetDBSuffix("apitoken")
	s.BaseSuite.SetupSuite()
}

// ============ Create Token Tests ============

func (s *ApiTokenSuite) TestCreateToken_Success() {
	rec := s.Client.POST(
		"/api/projects/"+s.ProjectID+"/tokens",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"name":   "Test Token",
			"scopes": []string{"schema:read", "data:read"},
		}),
	)

	s.Equal(http.StatusCreated, rec.StatusCode)

	var response apitoken.CreateApiTokenResponseDTO
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	s.NotEmpty(response.ID)
	s.Equal("Test Token", response.Name)
	s.Equal("emt_", response.TokenPrefix[:4])
	s.Equal([]string{"schema:read", "data:read"}, response.Scopes)
	s.NotEmpty(response.Token)
	s.True(len(response.Token) == 68) // emt_ + 64 hex chars
	s.False(response.IsRevoked)
}

func (s *ApiTokenSuite) TestCreateToken_RequiresAuth() {
	rec := s.Client.POST(
		"/api/projects/"+s.ProjectID+"/tokens",
		testutil.WithJSONBody(map[string]any{
			"name":   "Test Token",
			"scopes": []string{"schema:read"},
		}),
	)

	s.Equal(http.StatusUnauthorized, rec.StatusCode)
}

func (s *ApiTokenSuite) TestCreateToken_MissingName() {
	rec := s.Client.POST(
		"/api/projects/"+s.ProjectID+"/tokens",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"scopes": []string{"schema:read"},
		}),
	)

	s.Equal(http.StatusBadRequest, rec.StatusCode)
}

func (s *ApiTokenSuite) TestCreateToken_MissingScopes() {
	rec := s.Client.POST(
		"/api/projects/"+s.ProjectID+"/tokens",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"name": "Test Token",
		}),
	)

	s.Equal(http.StatusBadRequest, rec.StatusCode)
}

func (s *ApiTokenSuite) TestCreateToken_EmptyScopes() {
	rec := s.Client.POST(
		"/api/projects/"+s.ProjectID+"/tokens",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"name":   "Test Token",
			"scopes": []string{},
		}),
	)

	s.Equal(http.StatusBadRequest, rec.StatusCode)
}

func (s *ApiTokenSuite) TestCreateToken_InvalidScope() {
	rec := s.Client.POST(
		"/api/projects/"+s.ProjectID+"/tokens",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"name":   "Test Token",
			"scopes": []string{"invalid:scope"},
		}),
	)

	s.Equal(http.StatusBadRequest, rec.StatusCode)
}

func (s *ApiTokenSuite) TestCreateToken_DuplicateName() {
	// Create first token with unique name
	tokenName := "Duplicate Name " + uuid.New().String()[:8]
	rec := s.Client.POST(
		"/api/projects/"+s.ProjectID+"/tokens",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"name":   tokenName,
			"scopes": []string{"schema:read"},
		}),
	)
	s.Equal(http.StatusCreated, rec.StatusCode)

	// Try to create second token with same name
	rec = s.Client.POST(
		"/api/projects/"+s.ProjectID+"/tokens",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"name":   tokenName,
			"scopes": []string{"schema:read"},
		}),
	)
	s.Equal(http.StatusConflict, rec.StatusCode)
}

func (s *ApiTokenSuite) TestCreateToken_NameTooLong() {
	longName := make([]byte, 256)
	for i := range longName {
		longName[i] = 'a'
	}

	rec := s.Client.POST(
		"/api/projects/"+s.ProjectID+"/tokens",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"name":   string(longName),
			"scopes": []string{"schema:read"},
		}),
	)

	s.Equal(http.StatusBadRequest, rec.StatusCode)
}

// ============ List Tokens Tests ============

func (s *ApiTokenSuite) TestListTokens_ReturnsTokens() {
	// Create two tokens with unique names
	token1Name := "Token 1 " + uuid.New().String()[:8]
	token2Name := "Token 2 " + uuid.New().String()[:8]

	s.Client.POST(
		"/api/projects/"+s.ProjectID+"/tokens",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"name":   token1Name,
			"scopes": []string{"schema:read"},
		}),
	)
	s.Client.POST(
		"/api/projects/"+s.ProjectID+"/tokens",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"name":   token2Name,
			"scopes": []string{"data:read"},
		}),
	)

	rec := s.Client.GET(
		"/api/projects/"+s.ProjectID+"/tokens",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, rec.StatusCode)

	var response apitoken.ApiTokenListResponseDTO
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	// Should have at least 2 tokens (may have more from other tests in external mode)
	s.GreaterOrEqual(response.Total, 2)
	s.GreaterOrEqual(len(response.Tokens), 2)
}

func (s *ApiTokenSuite) TestListTokens_RequiresAuth() {
	rec := s.Client.GET("/api/projects/" + s.ProjectID + "/tokens")

	s.Equal(http.StatusUnauthorized, rec.StatusCode)
}

func (s *ApiTokenSuite) TestListTokens_DoesNotIncludeTokenValue() {
	// Create a token
	tokenName := "Test Token " + uuid.New().String()[:8]
	createRec := s.Client.POST(
		"/api/projects/"+s.ProjectID+"/tokens",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"name":   tokenName,
			"scopes": []string{"schema:read"},
		}),
	)
	s.Equal(http.StatusCreated, createRec.StatusCode)

	// List tokens
	rec := s.Client.GET(
		"/api/projects/"+s.ProjectID+"/tokens",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, rec.StatusCode)

	// Check that the raw response doesn't contain a "token" field
	var raw map[string]any
	err := json.Unmarshal(rec.Body, &raw)
	s.Require().NoError(err)

	tokens := raw["tokens"].([]any)
	s.GreaterOrEqual(len(tokens), 1)

	tokenMap := tokens[0].(map[string]any)
	_, hasToken := tokenMap["token"]
	s.False(hasToken, "List response should not include token value")
}

// ============ Get Token Tests ============

func (s *ApiTokenSuite) TestGetToken_Success() {
	// Create a token
	tokenName := "Test Token " + uuid.New().String()[:8]
	createRec := s.Client.POST(
		"/api/projects/"+s.ProjectID+"/tokens",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"name":   tokenName,
			"scopes": []string{"schema:read", "data:write"},
		}),
	)
	s.Equal(http.StatusCreated, createRec.StatusCode)

	var createResponse apitoken.CreateApiTokenResponseDTO
	json.Unmarshal(createRec.Body, &createResponse)

	// Get the token
	rec := s.Client.GET(
		"/api/projects/"+s.ProjectID+"/tokens/"+createResponse.ID,
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, rec.StatusCode)

	var response apitoken.GetApiTokenResponseDTO
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	s.Equal(createResponse.ID, response.ID)
	s.Equal(tokenName, response.Name)
	s.Equal(createResponse.TokenPrefix, response.TokenPrefix)
	s.Equal([]string{"schema:read", "data:write"}, response.Scopes)
	s.False(response.IsRevoked)

	// The full token should be retrievable if encryption is configured
	if response.Token != "" {
		s.Equal(createResponse.Token, response.Token, "retrieved token should match the one returned at creation")
	}
}

func (s *ApiTokenSuite) TestGetToken_NotFound() {
	rec := s.Client.GET(
		"/api/projects/"+s.ProjectID+"/tokens/"+uuid.New().String(),
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusNotFound, rec.StatusCode)
}

func (s *ApiTokenSuite) TestGetToken_RequiresAuth() {
	rec := s.Client.GET("/api/projects/" + s.ProjectID + "/tokens/" + uuid.New().String())

	s.Equal(http.StatusUnauthorized, rec.StatusCode)
}

// ============ Revoke Token Tests ============

func (s *ApiTokenSuite) TestRevokeToken_Success() {
	// Create a token
	tokenName := "Token to Revoke " + uuid.New().String()[:8]
	createRec := s.Client.POST(
		"/api/projects/"+s.ProjectID+"/tokens",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"name":   tokenName,
			"scopes": []string{"schema:read"},
		}),
	)
	s.Equal(http.StatusCreated, createRec.StatusCode)

	var createResponse apitoken.CreateApiTokenResponseDTO
	json.Unmarshal(createRec.Body, &createResponse)

	// Revoke the token
	rec := s.Client.DELETE(
		"/api/projects/"+s.ProjectID+"/tokens/"+createResponse.ID,
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, rec.StatusCode)

	var response map[string]string
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)
	s.Equal("revoked", response["status"])

	// Verify token is now revoked
	getRecPost := s.Client.GET(
		"/api/projects/"+s.ProjectID+"/tokens/"+createResponse.ID,
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusOK, getRecPost.StatusCode)

	var getResponse apitoken.ApiTokenDTO
	json.Unmarshal(getRecPost.Body, &getResponse)
	s.True(getResponse.IsRevoked)
}

func (s *ApiTokenSuite) TestRevokeToken_NotFound() {
	rec := s.Client.DELETE(
		"/api/projects/"+s.ProjectID+"/tokens/"+uuid.New().String(),
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusNotFound, rec.StatusCode)
}

func (s *ApiTokenSuite) TestRevokeToken_AlreadyRevoked() {
	// Create a token
	tokenName := "Token to Revoke Twice " + uuid.New().String()[:8]
	createRec := s.Client.POST(
		"/api/projects/"+s.ProjectID+"/tokens",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"name":   tokenName,
			"scopes": []string{"schema:read"},
		}),
	)
	s.Equal(http.StatusCreated, createRec.StatusCode)

	var createResponse apitoken.CreateApiTokenResponseDTO
	json.Unmarshal(createRec.Body, &createResponse)

	// Revoke once
	rec := s.Client.DELETE(
		"/api/projects/"+s.ProjectID+"/tokens/"+createResponse.ID,
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusOK, rec.StatusCode)

	// Try to revoke again
	rec = s.Client.DELETE(
		"/api/projects/"+s.ProjectID+"/tokens/"+createResponse.ID,
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusConflict, rec.StatusCode)
}

func (s *ApiTokenSuite) TestRevokeToken_RequiresAuth() {
	rec := s.Client.DELETE("/api/projects/" + s.ProjectID + "/tokens/" + uuid.New().String())

	s.Equal(http.StatusUnauthorized, rec.StatusCode)
}

// ============ Duplicate Name After Revoke Tests ============

func (s *ApiTokenSuite) TestCreateToken_DuplicateNameNotAllowedAfterRevoke() {
	// The database constraint doesn't allow duplicate names even after revoke
	// This is by design - token names must be unique within a project

	// Create first token
	tokenName := "Unique Name " + uuid.New().String()[:8]
	createRec := s.Client.POST(
		"/api/projects/"+s.ProjectID+"/tokens",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"name":   tokenName,
			"scopes": []string{"schema:read"},
		}),
	)
	s.Equal(http.StatusCreated, createRec.StatusCode)

	var createResponse apitoken.CreateApiTokenResponseDTO
	json.Unmarshal(createRec.Body, &createResponse)

	// Revoke it
	revokeRec := s.Client.DELETE(
		"/api/projects/"+s.ProjectID+"/tokens/"+createResponse.ID,
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusOK, revokeRec.StatusCode)

	// Try to create new token with same name - should fail (names must be globally unique in project)
	rec := s.Client.POST(
		"/api/projects/"+s.ProjectID+"/tokens",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"name":   tokenName,
			"scopes": []string{"data:read"},
		}),
	)
	s.Equal(http.StatusConflict, rec.StatusCode)
}

func TestApiTokenSuite(t *testing.T) {
	suite.Run(t, new(ApiTokenSuite))
}

// ============ Account Token Suite ============

type AccountApiTokenSuite struct {
	testutil.BaseSuite
	// SecondProjectID is a second project used in cross-project access tests.
	SecondProjectID string
}

func (s *AccountApiTokenSuite) SetupSuite() {
	s.SetDBSuffix("accountapitoken")
	s.BaseSuite.SetupSuite()
}

func (s *AccountApiTokenSuite) SetupTest() {
	s.BaseSuite.SetupTest()

	if s.IsExternal() {
		// Create a second project via API for external mode
		projectID, err := s.Client.CreateProject("Test Project 2", s.OrgID, "e2e-test-user")
		s.Require().NoError(err, "Failed to create second project via API")
		s.SecondProjectID = projectID
		return
	}

	// Create a second project in the same org for cross-project tests
	s.SecondProjectID = uuid.New().String()
	err := testutil.CreateTestProject(s.Ctx, s.DB(), testutil.TestProject{
		ID:    s.SecondProjectID,
		OrgID: s.OrgID,
		Name:  "Second Test Project",
	}, testutil.AdminUser.ID)
	s.Require().NoError(err)

	err = testutil.CreateTestProjectMembership(s.Ctx, s.DB(), s.SecondProjectID, testutil.AdminUser.ID, "project_admin")
	s.Require().NoError(err)
}

// TestCreateAccountToken_Success verifies the account-token creation endpoint
// returns a 201 with a valid emt_* token and no project_id.
func (s *AccountApiTokenSuite) TestCreateAccountToken_Success() {
	s.SkipIfExternalServer("account token routes require direct test-server access")

	rec := s.Client.POST(
		"/api/tokens",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"name":   "My Account Token " + uuid.New().String()[:8],
			"scopes": []string{"projects:read"},
		}),
	)

	s.Equal(http.StatusCreated, rec.StatusCode)

	var response apitoken.CreateApiTokenResponseDTO
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	s.NotEmpty(response.ID)
	s.True(len(response.Token) == 68, "token should be 68 chars (emt_ + 64 hex)")
	s.Equal("emt_", response.Token[:4])
	s.Nil(response.ProjectID, "account token should have nil project_id")
	s.Equal([]string{"projects:read"}, response.Scopes)
	s.False(response.IsRevoked)
}

// TestAccountToken_AccessesTwoProjects verifies an account token (with projects:read)
// can access resources in two different projects.
func (s *AccountApiTokenSuite) TestAccountToken_AccessesTwoProjects() {
	s.SkipIfExternalServer("account token routes require direct test-server access")

	// Create an account-level token in the DB using the testutil helper
	rawToken := "emt_acct_crossproject_testtoken0000000000000000000000000000"
	err := testutil.CreateTestAccountAPIToken(
		s.Ctx, s.DB(),
		testutil.AdminUser.ID,
		rawToken,
		[]string{"projects:read"},
	)
	s.Require().NoError(err)

	// Access project 1 via GET /api/projects/:projectId
	rec1 := s.Client.GET(
		"/api/projects/"+s.ProjectID,
		testutil.WithAPIToken(rawToken),
	)
	s.Equal(http.StatusOK, rec1.StatusCode, "account token should access project 1")

	// Access project 2
	rec2 := s.Client.GET(
		"/api/projects/"+s.SecondProjectID,
		testutil.WithAPIToken(rawToken),
	)
	s.Equal(http.StatusOK, rec2.StatusCode, "account token should access project 2")
}

// TestAccountToken_WithoutProjectsRead_CannotListProjects verifies that an account
// token lacking projects:read is rejected with 403 on GET /api/projects.
func (s *AccountApiTokenSuite) TestAccountToken_WithoutProjectsRead_CannotListProjects() {
	s.SkipIfExternalServer("account token routes require direct test-server access")

	rawToken := "emt_acct_noscope_testtoken00000000000000000000000000000000"
	err := testutil.CreateTestAccountAPIToken(
		s.Ctx, s.DB(),
		testutil.AdminUser.ID,
		rawToken,
		[]string{"data:read"}, // no projects:read
	)
	s.Require().NoError(err)

	rec := s.Client.GET(
		"/api/projects/"+s.ProjectID,
		testutil.WithAPIToken(rawToken),
	)
	// RequireAPITokenScopes("projects:read") should block this
	s.Equal(http.StatusForbidden, rec.StatusCode)
}

// TestAccountToken_RevokedTokenReturns401 verifies that after revoking an account
// token via DELETE /api/tokens/:tokenId, subsequent requests return 401.
func (s *AccountApiTokenSuite) TestAccountToken_RevokedTokenReturns401() {
	s.SkipIfExternalServer("account token routes require direct test-server access")

	// 1. Create account token via the API
	tokenName := "Revoke Me " + uuid.New().String()[:8]
	createRec := s.Client.POST(
		"/api/tokens",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"name":   tokenName,
			"scopes": []string{"projects:read"},
		}),
	)
	s.Require().Equal(http.StatusCreated, createRec.StatusCode)

	var created apitoken.CreateApiTokenResponseDTO
	s.Require().NoError(json.Unmarshal(createRec.Body, &created))

	// 2. Revoke via the account token endpoint
	revokeRec := s.Client.DELETE(
		"/api/tokens/"+created.ID,
		testutil.WithAuth("e2e-test-user"),
	)
	s.Require().Equal(http.StatusOK, revokeRec.StatusCode)

	// 3. Using the raw token now should return 401
	rec := s.Client.GET(
		"/api/projects/"+s.ProjectID,
		testutil.WithAPIToken(created.Token),
	)
	s.Equal(http.StatusUnauthorized, rec.StatusCode)
}

// TestListAccountTokens_ReturnsCreatedToken verifies GET /api/tokens returns the
// tokens created by the authenticated user.
func (s *AccountApiTokenSuite) TestListAccountTokens_ReturnsCreatedToken() {
	s.SkipIfExternalServer("account token routes require direct test-server access")

	tokenName := "List Me " + uuid.New().String()[:8]
	s.Client.POST(
		"/api/tokens",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"name":   tokenName,
			"scopes": []string{"projects:read"},
		}),
	)

	listRec := s.Client.GET(
		"/api/tokens",
		testutil.WithAuth("e2e-test-user"),
	)
	s.Require().Equal(http.StatusOK, listRec.StatusCode)

	var listResp apitoken.ApiTokenListResponseDTO
	s.Require().NoError(json.Unmarshal(listRec.Body, &listResp))

	s.GreaterOrEqual(listResp.Total, 1)
	found := false
	for _, tok := range listResp.Tokens {
		if tok.Name == tokenName {
			found = true
			s.Nil(tok.ProjectID, "account token should have nil project_id in list")
		}
	}
	s.True(found, "created account token should appear in list")
}

func TestAccountApiTokenSuite(t *testing.T) {
	suite.Run(t, new(AccountApiTokenSuite))
}
