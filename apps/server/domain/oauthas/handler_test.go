package oauthas

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/apitoken"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

func newHandlerFixture(t *testing.T, cfg config.OAuthASConfig) (*Handler, *fakeStore, *fakeMinter, *fakeTokenLookup, *time.Time) {
	t.Helper()
	svc, store, minter, lookup, now := newServiceFixture(t, cfg)
	full := &config.Config{OAuthAS: cfg}
	return NewHandler(svc, full, svc.log), store, minter, lookup, now
}

func doJSON(e *echo.Echo, method, path string, body any) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.RemoteAddr = "203.0.113.9:1234"
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func doForm(e *echo.Echo, path string, values url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(values.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	req.RemoteAddr = "203.0.113.9:1234"
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func withUser(userID string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			user := &auth.AuthUser{ID: userID}
			c.Set(string(auth.UserContextKey), user)
			c.SetRequest(c.Request().WithContext(auth.ContextWithUser(c.Request().Context(), user)))
			return next(c)
		}
	}
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body
}

func TestMetadataShape(t *testing.T) {
	h, _, _, _, _ := newHandlerFixture(t, config.OAuthASConfig{
		IssuerURL:    "https://api.memory.example/",
		AuthorizeURL: "https://app.memory.example/",
	})
	e := echo.New()
	e.GET("/.well-known/oauth-authorization-server", h.Metadata)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	body := decodeBody(t, rec)
	require.Equal(t, "https://api.memory.example", body["issuer"])
	require.Equal(t, "https://app.memory.example/oauth/authorize", body["authorization_endpoint"])
	require.Equal(t, "https://api.memory.example/api/oauth/token", body["token_endpoint"])
	require.Equal(t, "https://api.memory.example/api/oauth/register", body["registration_endpoint"])
	require.Equal(t, "https://api.memory.example/api/oauth/revoke", body["revocation_endpoint"])
	require.Equal(t, []any{"code"}, body["response_types_supported"])
	require.Equal(t, []any{"authorization_code"}, body["grant_types_supported"])
	require.Equal(t, []any{"S256"}, body["code_challenge_methods_supported"])
	require.Equal(t, []any{"none"}, body["token_endpoint_auth_methods_supported"])
	require.Equal(t, []any{"graph:read", "graph:write", "schema:read", "projects:read"}, body["scopes_supported"])
}

func TestMetadataDerivesIssuerFromRequest(t *testing.T) {
	h, _, _, _, _ := newHandlerFixture(t, config.OAuthASConfig{})
	e := echo.New()
	e.GET("/.well-known/oauth-authorization-server", h.Metadata)

	req := httptest.NewRequest(http.MethodGet, "http://api.example.com/.well-known/oauth-authorization-server", nil)
	req.Host = "api.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	body := decodeBody(t, rec)
	require.Equal(t, "https://api.example.com", body["issuer"])
	require.Equal(t, "https://api.example.com/oauth/authorize", body["authorization_endpoint"])
}

func TestRegisterHandlerSuccess(t *testing.T) {
	h, store, _, _, _ := newHandlerFixture(t, config.OAuthASConfig{RegistrationEnabled: true})
	e := echo.New()
	e.POST("/api/oauth/register", h.Register)

	rec := doJSON(e, http.MethodPost, "/api/oauth/register", ClientRegistrationRequest{
		ClientName:   "emergent.feedback",
		RedirectURIs: []string{"https://app.feedback.example/callback"},
		Scope:        "graph:read projects:read",
	})
	require.Equal(t, http.StatusCreated, rec.Code)

	body := decodeBody(t, rec)
	clientID, _ := body["client_id"].(string)
	require.Len(t, clientID, 43)
	require.Equal(t, "emergent.feedback", body["client_name"])
	require.Equal(t, "none", body["token_endpoint_auth_method"])
	require.Equal(t, []any{"authorization_code"}, body["grant_types"])
	require.Equal(t, "graph:read projects:read", body["scope"])
	require.NotContains(t, body, "client_secret")
	require.Contains(t, store.clients, clientID)
}

func TestRegisterHandlerValidation(t *testing.T) {
	h, _, _, _, _ := newHandlerFixture(t, config.OAuthASConfig{RegistrationEnabled: true})
	e := echo.New()
	e.POST("/api/oauth/register", h.Register)

	rec := doJSON(e, http.MethodPost, "/api/oauth/register", ClientRegistrationRequest{
		ClientName: "app",
	})
	require.Equal(t, http.StatusBadRequest, rec.Code)
	body := decodeBody(t, rec)
	require.Equal(t, "invalid_redirect_uri", body["error"])
}

func TestRegisterHandlerDisabled(t *testing.T) {
	h, _, _, _, _ := newHandlerFixture(t, config.OAuthASConfig{RegistrationEnabled: false})
	e := echo.New()
	e.POST("/api/oauth/register", h.Register)

	rec := doJSON(e, http.MethodPost, "/api/oauth/register", ClientRegistrationRequest{})
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Equal(t, "registration_disabled", decodeBody(t, rec)["error"])
}

func TestRegisterHandlerSecret(t *testing.T) {
	h, _, _, _, _ := newHandlerFixture(t, config.OAuthASConfig{
		RegistrationEnabled: true,
		RegistrationSecret:  "s3cret",
	})
	e := echo.New()
	e.POST("/api/oauth/register", h.Register)

	body := ClientRegistrationRequest{ClientName: "app", RedirectURIs: []string{"https://app.example/cb"}}

	rec := doJSON(e, http.MethodPost, "/api/oauth/register", body)
	require.Equal(t, http.StatusUnauthorized, rec.Code)

	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/oauth/register", bytes.NewReader(b))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.Header.Set("X-Registration-Secret", "wrong")
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)

	req = httptest.NewRequest(http.MethodPost, "/api/oauth/register", bytes.NewReader(b))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.Header.Set("X-Registration-Secret", "s3cret")
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code)
}

func TestRegisterHandlerRateLimit(t *testing.T) {
	h, _, _, _, _ := newHandlerFixture(t, config.OAuthASConfig{RegistrationEnabled: true})
	e := echo.New()
	e.POST("/api/oauth/register", h.Register)

	for i := 0; i < 10; i++ {
		rec := doJSON(e, http.MethodPost, "/api/oauth/register", ClientRegistrationRequest{
			ClientName:   "app",
			RedirectURIs: []string{"https://app.example/cb"},
		})
		require.Equal(t, http.StatusCreated, rec.Code, "request %d should be allowed", i+1)
	}

	rec := doJSON(e, http.MethodPost, "/api/oauth/register", ClientRegistrationRequest{
		ClientName:   "app",
		RedirectURIs: []string{"https://app.example/cb"},
	})
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.Equal(t, "rate_limited", decodeBody(t, rec)["error"])
}

func TestTokenHandlerSuccess(t *testing.T) {
	h, store, _, _, _ := newHandlerFixture(t, config.OAuthASConfig{})
	e := echo.New()
	e.POST("/api/oauth/token", h.Token)

	client := registerTestClient(t, h.svc)
	userID, projectID := uuid.NewString(), uuid.NewString()
	store.projects[projectID] = "Demo Project"
	store.writable[projectID+"|"+userID] = true
	code := codeFromRedirect(t, authorizeTestCode(t, h.svc, client.ClientID, userID, projectID).RedirectURL)

	rec := doForm(e, "/api/oauth/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {"https://app.feedback.example/callback?existing=1"},
		"client_id":     {client.ClientID},
		"code_verifier": {rfcVerifier},
	})
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	require.Equal(t, "no-cache", rec.Header().Get("Pragma"))

	body := decodeBody(t, rec)
	require.Equal(t, "emt_test_access_token", body["access_token"])
	require.Equal(t, "Bearer", body["token_type"])
	require.Equal(t, "graph:read projects:read", body["scope"])
	require.Equal(t, projectID, body["project_id"])
	require.Equal(t, "Demo Project", body["project_name"])
	require.NotContains(t, body, "refresh_token")
}

func TestTokenHandlerErrors(t *testing.T) {
	h, _, _, _, _ := newHandlerFixture(t, config.OAuthASConfig{})
	e := echo.New()
	e.POST("/api/oauth/token", h.Token)

	rec := doForm(e, "/api/oauth/token", url.Values{
		"grant_type": {"client_credentials"},
	})
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "unsupported_grant_type", decodeBody(t, rec)["error"])

	rec = doForm(e, "/api/oauth/token", url.Values{
		"grant_type": {"authorization_code"},
		"code":       {"nope"},
		"client_id":  {"unknown"},
	})
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, "invalid_client", decodeBody(t, rec)["error"])

	rec = doForm(e, "/api/oauth/token", url.Values{
		"grant_type": {"authorization_code"},
	})
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "invalid_request", decodeBody(t, rec)["error"])
}

func TestAuthorizeHandlers(t *testing.T) {
	h, store, _, _, _ := newHandlerFixture(t, config.OAuthASConfig{})
	client := registerTestClient(t, h.svc)
	userID, projectID := uuid.NewString(), uuid.NewString()
	store.projects[projectID] = "Demo Project"
	store.writable[projectID+"|"+userID] = true

	e := echo.New()
	e.GET("/api/oauth/authorize/context", h.AuthorizeContext, withUser(userID))
	e.POST("/api/oauth/authorize", h.Authorize, withUser(userID))

	ctxPath := "/api/oauth/authorize/context?client_id=" + url.QueryEscape(client.ClientID) +
		"&redirect_uri=" + url.QueryEscape("https://app.feedback.example/callback?existing=1")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, ctxPath, nil))
	require.Equal(t, http.StatusOK, rec.Code)
	body := decodeBody(t, rec)
	require.Equal(t, client.ClientID, body["client_id"])
	require.Equal(t, "emergent.feedback", body["client_name"])
	projects := body["projects"].([]any)
	require.Len(t, projects, 1)
	require.Equal(t, "demo-project", projects[0].(map[string]any)["slug"])

	rec = doJSON(e, http.MethodPost, "/api/oauth/authorize", AuthorizeRequest{
		ClientID:            client.ClientID,
		RedirectURI:         "https://app.feedback.example/callback?existing=1",
		ProjectID:           projectID,
		Scopes:              []string{"graph:read"},
		CodeChallenge:       rfcChallenge,
		CodeChallengeMethod: "S256",
		State:               "abc",
	})
	require.Equal(t, http.StatusOK, rec.Code)
	redirectURL, _ := decodeBody(t, rec)["redirect_url"].(string)
	require.Contains(t, redirectURL, "state=abc")
	require.Contains(t, redirectURL, "code=")
}

func TestGrantsHandlers(t *testing.T) {
	h, store, minter, _, _ := newHandlerFixture(t, config.OAuthASConfig{})
	client := registerTestClient(t, h.svc)
	userID, projectID := uuid.NewString(), uuid.NewString()
	store.projects[projectID] = "Demo Project"
	store.writable[projectID+"|"+userID] = true
	code := codeFromRedirect(t, authorizeTestCode(t, h.svc, client.ClientID, userID, projectID).RedirectURL)
	_, err := h.svc.ExchangeCode(t.Context(), TokenRequest{
		GrantType: "authorization_code", Code: code,
		RedirectURI: "https://app.feedback.example/callback?existing=1",
		ClientID:    client.ClientID, CodeVerifier: rfcVerifier,
	})
	require.NoError(t, err)

	e := echo.New()
	e.GET("/api/oauth/grants", h.ListGrants, withUser(userID))
	e.DELETE("/api/oauth/grants/:id", h.RevokeGrant, withUser(userID))

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/oauth/grants", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var grants []GrantDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &grants))
	require.Len(t, grants, 1)
	require.Equal(t, "emergent.feedback", grants[0].ClientName)
	require.Equal(t, "Demo Project", grants[0].ProjectName)
	require.Equal(t, []string{"graph:read", "projects:read"}, grants[0].Scopes)

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/oauth/grants/"+grants[0].ID, nil))
	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Len(t, minter.revoked, 1)

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/oauth/grants/"+uuid.NewString(), nil))
	require.Equal(t, http.StatusNotFound, rec.Code)

	// Without an authenticated user the handler fails closed.
	e2 := echo.New()
	e2.GET("/api/oauth/grants", h.ListGrants)
	rec = httptest.NewRecorder()
	e2.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/oauth/grants", nil))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRevokeHandler(t *testing.T) {
	h, store, minter, lookup, _ := newHandlerFixture(t, config.OAuthASConfig{})
	client := registerTestClient(t, h.svc)
	userID, projectID := uuid.NewString(), uuid.NewString()
	store.projects[projectID] = "Demo"
	store.writable[projectID+"|"+userID] = true
	code := codeFromRedirect(t, authorizeTestCode(t, h.svc, client.ClientID, userID, projectID).RedirectURL)
	_, err := h.svc.ExchangeCode(t.Context(), TokenRequest{
		GrantType: "authorization_code", Code: code,
		RedirectURI: "https://app.feedback.example/callback?existing=1",
		ClientID:    client.ClientID, CodeVerifier: rfcVerifier,
	})
	require.NoError(t, err)

	var tokenID string
	for _, g := range store.grants {
		tokenID = g.TokenID
	}
	lookup.tokens[hashCode("emt_test_access_token")] = &apitoken.ApiToken{
		ID: tokenID, ProjectID: &projectID, UserID: &userID,
	}

	e := echo.New()
	e.POST("/api/oauth/revoke", h.Revoke)

	rec := doForm(e, "/api/oauth/revoke", url.Values{
		"client_id": {client.ClientID},
		"token":     {"emt_test_access_token"},
	})
	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, minter.revoked, 1)
	for _, g := range store.grants {
		require.NotNil(t, g.RevokedAt)
	}

	// Unknown tokens are accepted silently.
	rec = doForm(e, "/api/oauth/revoke", url.Values{
		"client_id": {client.ClientID},
		"token":     {"emt_unknown"},
	})
	require.Equal(t, http.StatusOK, rec.Code)

	// Unknown clients are rejected.
	rec = doForm(e, "/api/oauth/revoke", url.Values{
		"client_id": {"unknown"},
		"token":     {"emt_test_access_token"},
	})
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, "invalid_client", decodeBody(t, rec)["error"])
}
