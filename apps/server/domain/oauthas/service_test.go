package oauthas

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/apitoken"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// RFC 7636 Appendix B test vector.
const (
	rfcVerifier  = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	rfcChallenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
)

type fakeStore struct {
	mu       sync.Mutex
	clients  map[string]*OAuthClient
	codes    map[string]*OAuthAuthorizationCode
	grants   map[string]*OAuthGrant
	projects map[string]string
	writable map[string]bool // projectID + "|" + userID
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		clients:  map[string]*OAuthClient{},
		codes:    map[string]*OAuthAuthorizationCode{},
		grants:   map[string]*OAuthGrant{},
		projects: map[string]string{},
		writable: map[string]bool{},
	}
}

func (f *fakeStore) CreateClient(_ context.Context, client *OAuthClient) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if client.ID == "" {
		client.ID = uuid.NewString()
	}
	f.clients[client.ClientID] = client
	return nil
}

func (f *fakeStore) GetClient(_ context.Context, clientID string) (*OAuthClient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.clients[clientID], nil
}

func (f *fakeStore) TouchClient(_ context.Context, clientID string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c := f.clients[clientID]; c != nil {
		c.LastUsedAt = &at
	}
	return nil
}

func (f *fakeStore) CreateCode(_ context.Context, code *OAuthAuthorizationCode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if code.ID == "" {
		code.ID = uuid.NewString()
	}
	f.codes[code.CodeHash] = code
	return nil
}

func (f *fakeStore) GetCodeByHash(_ context.Context, hash string) (*OAuthAuthorizationCode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.codes[hash], nil
}

func (f *fakeStore) ConsumeCode(_ context.Context, hash, clientID, redirectURI string, now time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	code := f.codes[hash]
	if code == nil || code.ClientID != clientID || code.RedirectURI != redirectURI ||
		code.ConsumedAt != nil || !now.Before(code.ExpiresAt) {
		return false, nil
	}
	code.ConsumedAt = &now
	return true, nil
}

func (f *fakeStore) CreateGrant(_ context.Context, grant *OAuthGrant) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if grant.ID == "" {
		grant.ID = uuid.NewString()
	}
	f.grants[grant.ID] = grant
	return nil
}

func (f *fakeStore) GetGrantByIDForUser(_ context.Context, grantID, userID string) (*OAuthGrant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	g := f.grants[grantID]
	if g == nil || g.UserID != userID {
		return nil, nil
	}
	return g, nil
}

func (f *fakeStore) GetGrantByTokenIDAndClient(_ context.Context, tokenID, clientID string) (*OAuthGrant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, g := range f.grants {
		if g.TokenID == tokenID && g.ClientID == clientID {
			return g, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) ListGrantsByUser(_ context.Context, userID string) ([]grantRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var rows []grantRow
	for _, g := range f.grants {
		if g.UserID != userID {
			continue
		}
		clientName := ""
		if c := f.clients[g.ClientID]; c != nil {
			clientName = c.ClientName
		}
		rows = append(rows, grantRow{
			ID: g.ID, ClientID: g.ClientID, ClientName: clientName,
			UserID: g.UserID, ProjectID: g.ProjectID, ProjectName: f.projects[g.ProjectID],
			Scopes: g.Scopes, TokenID: g.TokenID, CreatedAt: g.CreatedAt,
			LastUsedAt: g.LastUsedAt, RevokedAt: g.RevokedAt,
		})
	}
	return rows, nil
}

func (f *fakeStore) RevokeGrant(_ context.Context, grantID, userID string, now time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	g := f.grants[grantID]
	if g == nil || g.UserID != userID || g.RevokedAt != nil {
		return false, nil
	}
	g.RevokedAt = &now
	return true, nil
}

func (f *fakeStore) RevokeGrantByTokenID(_ context.Context, tokenID, clientID string, now time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, g := range f.grants {
		if g.TokenID == tokenID && g.ClientID == clientID && g.RevokedAt == nil {
			g.RevokedAt = &now
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeStore) MarkGrantUsed(_ context.Context, grantID string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if g := f.grants[grantID]; g != nil {
		g.LastUsedAt = &at
	}
	return nil
}

func (f *fakeStore) GetProjectName(_ context.Context, projectID string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.projects[projectID], nil
}

func (f *fakeStore) CanWriteProject(_ context.Context, projectID, userID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writable[projectID+"|"+userID], nil
}

func (f *fakeStore) ListWritableProjects(_ context.Context, userID string) ([]OAuthProject, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []OAuthProject{}
	for pid, name := range f.projects {
		if f.writable[pid+"|"+userID] {
			out = append(out, OAuthProject{ID: pid, Name: name})
		}
	}
	return out, nil
}

type createdToken struct {
	projectID string
	userID    string
	name      string
	scopes    []string
}

type fakeMinter struct {
	mu      sync.Mutex
	created []createdToken
	revoked []string
	token   string
	err     error
}

func (f *fakeMinter) Create(_ context.Context, projectID, userID, name string, scopes []string) (*apitoken.CreateApiTokenResponseDTO, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		err := f.err
		f.err = nil
		return nil, err
	}
	f.created = append(f.created, createdToken{projectID: projectID, userID: userID, name: name, scopes: scopes})
	return &apitoken.CreateApiTokenResponseDTO{
		ApiTokenDTO: apitoken.ApiTokenDTO{
			ID:        uuid.NewString(),
			ProjectID: &projectID,
			Name:      name,
			Scopes:    scopes,
		},
		Token: f.token,
	}, nil
}

func (f *fakeMinter) Revoke(_ context.Context, tokenID, _, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revoked = append(f.revoked, tokenID)
	return nil
}

type fakeTokenLookup struct {
	tokens map[string]*apitoken.ApiToken
}

func (f *fakeTokenLookup) FindByHash(_ context.Context, hash string) (*apitoken.ApiToken, error) {
	return f.tokens[hash], nil
}

func newServiceFixture(t *testing.T, cfg config.OAuthASConfig) (*Service, *fakeStore, *fakeMinter, *fakeTokenLookup, *time.Time) {
	t.Helper()
	store := newFakeStore()
	minter := &fakeMinter{token: "emt_test_access_token"}
	lookup := &fakeTokenLookup{tokens: map[string]*apitoken.ApiToken{}}
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	svc := &Service{
		repo:      store,
		tokens:    minter,
		tokenRepo: lookup,
		cfg:       cfg,
		log:       slog.New(slog.NewTextHandler(testWriter{t}, &slog.HandlerOptions{Level: slog.LevelError})),
		now:       func() time.Time { return now },
	}
	return svc, store, minter, lookup, &now
}

type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(strings.TrimSpace(string(p)))
	return len(p), nil
}

func registerTestClient(t *testing.T, svc *Service) *ClientRegistrationResponse {
	t.Helper()
	resp, err := svc.RegisterClient(context.Background(), ClientRegistrationRequest{
		ClientName:   "emergent.feedback",
		RedirectURIs: []string{"https://app.feedback.example/callback?existing=1"},
	})
	require.NoError(t, err)
	return resp
}

func authorizeTestCode(t *testing.T, svc *Service, clientID, userID, projectID string) *AuthorizeResponse {
	t.Helper()
	resp, err := svc.Authorize(context.Background(), userID, AuthorizeRequest{
		ClientID:            clientID,
		RedirectURI:         "https://app.feedback.example/callback?existing=1",
		ProjectID:           projectID,
		Scopes:              []string{"graph:read", "projects:read"},
		State:               "state-123",
		CodeChallenge:       rfcChallenge,
		CodeChallengeMethod: "S256",
	})
	require.NoError(t, err)
	return resp
}

func codeFromRedirect(t *testing.T, redirect string) string {
	t.Helper()
	parsed, err := url.Parse(redirect)
	require.NoError(t, err)
	code := parsed.Query().Get("code")
	require.NotEmpty(t, code)
	return code
}

func TestRegisterClientSuccess(t *testing.T) {
	svc, store, _, _, _ := newServiceFixture(t, config.OAuthASConfig{})
	resp := registerTestClient(t, svc)

	require.Len(t, resp.ClientID, 43, "32 random bytes base64url is 43 chars")
	require.Equal(t, "emergent.feedback", resp.ClientName)
	require.Equal(t, "none", resp.TokenEndpointAuthMethod)
	require.Equal(t, []string{"authorization_code"}, resp.GrantTypes)
	require.Equal(t, []string{"https://app.feedback.example/callback?existing=1"}, resp.RedirectURIs)
	require.Positive(t, resp.ClientIDIssuedAt)
	require.Empty(t, resp.Scope)

	stored := store.clients[resp.ClientID]
	require.NotNil(t, stored)
	require.Equal(t, "none", stored.TokenEndpointAuthMethod)
}

func TestRegisterClientValidation(t *testing.T) {
	manyURIs := make([]string, 6)
	for i := range manyURIs {
		manyURIs[i] = "https://app.feedback.example/cb" + string(rune('a'+i))
	}

	cases := []struct {
		name       string
		cfg        config.OAuthASConfig
		req        ClientRegistrationRequest
		wantCode   string
		wantStatus int
	}{
		{
			name:       "missing client name",
			req:        ClientRegistrationRequest{RedirectURIs: []string{"https://app.example/cb"}},
			wantCode:   "invalid_client_metadata",
			wantStatus: 400,
		},
		{
			name:       "missing redirect uris",
			req:        ClientRegistrationRequest{ClientName: "app"},
			wantCode:   "invalid_redirect_uri",
			wantStatus: 400,
		},
		{
			name:       "too many redirect uris",
			req:        ClientRegistrationRequest{ClientName: "app", RedirectURIs: manyURIs},
			wantCode:   "invalid_redirect_uri",
			wantStatus: 400,
		},
		{
			name:       "relative redirect uri",
			req:        ClientRegistrationRequest{ClientName: "app", RedirectURIs: []string{"/callback"}},
			wantCode:   "invalid_redirect_uri",
			wantStatus: 400,
		},
		{
			name:       "non http scheme",
			req:        ClientRegistrationRequest{ClientName: "app", RedirectURIs: []string{"javascript:alert(1)"}},
			wantCode:   "invalid_redirect_uri",
			wantStatus: 400,
		},
		{
			name:       "fragment in redirect uri",
			req:        ClientRegistrationRequest{ClientName: "app", RedirectURIs: []string{"https://app.example/cb#x"}},
			wantCode:   "invalid_redirect_uri",
			wantStatus: 400,
		},
		{
			name: "host not in allowlist",
			cfg:  config.OAuthASConfig{AllowedRedirectHosts: "allowed.example"},
			req: ClientRegistrationRequest{
				ClientName:   "app",
				RedirectURIs: []string{"https://evil.example/cb"},
			},
			wantCode:   "invalid_redirect_uri",
			wantStatus: 400,
		},
		{
			name: "confidential client rejected",
			req: ClientRegistrationRequest{
				ClientName:              "app",
				RedirectURIs:            []string{"https://app.example/cb"},
				TokenEndpointAuthMethod: "client_secret_post",
			},
			wantCode:   "invalid_client_metadata",
			wantStatus: 400,
		},
		{
			name: "unsupported grant type",
			req: ClientRegistrationRequest{
				ClientName:   "app",
				RedirectURIs: []string{"https://app.example/cb"},
				GrantTypes:   []string{"client_credentials"},
			},
			wantCode:   "invalid_client_metadata",
			wantStatus: 400,
		},
		{
			name: "invalid scope",
			req: ClientRegistrationRequest{
				ClientName:   "app",
				RedirectURIs: []string{"https://app.example/cb"},
				Scope:        "graph:read admin:all",
			},
			wantCode:   "invalid_client_metadata",
			wantStatus: 400,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, _, _, _ := newServiceFixture(t, tc.cfg)
			_, err := svc.RegisterClient(context.Background(), tc.req)
			require.Error(t, err)
			var oe *OAuthError
			require.True(t, errors.As(err, &oe))
			require.Equal(t, tc.wantCode, oe.Code)
			require.Equal(t, tc.wantStatus, oe.Status)
		})
	}
}

func TestRegisterClientAllowedHost(t *testing.T) {
	svc, _, _, _, _ := newServiceFixture(t, config.OAuthASConfig{
		AllowedRedirectHosts: "app.feedback.example, other.example",
	})
	resp, err := svc.RegisterClient(context.Background(), ClientRegistrationRequest{
		ClientName:   "app",
		RedirectURIs: []string{"https://APP.feedback.example:8443/cb"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"https://APP.feedback.example:8443/cb"}, resp.RedirectURIs)
}

func TestAuthorizeAndExchangeSuccess(t *testing.T) {
	svc, store, minter, _, _ := newServiceFixture(t, config.OAuthASConfig{})
	client := registerTestClient(t, svc)
	userID := uuid.NewString()
	projectID := uuid.NewString()
	store.projects[projectID] = "Demo Project"
	store.writable[projectID+"|"+userID] = true

	authz := authorizeTestCode(t, svc, client.ClientID, userID, projectID)

	parsed, err := url.Parse(authz.RedirectURL)
	require.NoError(t, err)
	require.Equal(t, "/callback", parsed.Path)
	require.Equal(t, "state-123", parsed.Query().Get("state"))
	require.Equal(t, "1", parsed.Query().Get("existing"), "existing query params are preserved")
	code := parsed.Query().Get("code")
	require.NotEmpty(t, code)

	require.Len(t, store.codes, 1, "only the hash is stored at rest")
	for hash := range store.codes {
		require.NotEqual(t, code, hash)
		require.Len(t, hash, 64)
	}

	resp, err := svc.ExchangeCode(context.Background(), TokenRequest{
		GrantType:    "authorization_code",
		Code:         code,
		RedirectURI:  "https://app.feedback.example/callback?existing=1",
		ClientID:     client.ClientID,
		CodeVerifier: rfcVerifier,
	})
	require.NoError(t, err)
	require.Equal(t, "emt_test_access_token", resp.AccessToken)
	require.Equal(t, "Bearer", resp.TokenType)
	require.Equal(t, "graph:read projects:read", resp.Scope)
	require.Equal(t, projectID, resp.ProjectID)
	require.Equal(t, "Demo Project", resp.ProjectName)

	require.Len(t, minter.created, 1)
	require.Equal(t, "oauth: emergent.feedback", minter.created[0].name)
	require.Equal(t, projectID, minter.created[0].projectID)
	require.Equal(t, userID, minter.created[0].userID)

	require.Len(t, store.grants, 1)
	for _, grant := range store.grants {
		require.Equal(t, client.ClientID, grant.ClientID)
		require.Equal(t, projectID, grant.ProjectID)
		require.Equal(t, []string{"graph:read", "projects:read"}, grant.Scopes)
		require.NotEmpty(t, grant.TokenID)
	}
}

func TestExchangeCodeExpired(t *testing.T) {
	svc, store, minter, _, now := newServiceFixture(t, config.OAuthASConfig{})
	client := registerTestClient(t, svc)
	userID, projectID := uuid.NewString(), uuid.NewString()
	store.projects[projectID] = "Demo"
	store.writable[projectID+"|"+userID] = true
	authz := authorizeTestCode(t, svc, client.ClientID, userID, projectID)
	code := codeFromRedirect(t, authz.RedirectURL)

	*now = now.Add(authorizationCodeTTL + time.Second)
	_, err := svc.ExchangeCode(context.Background(), TokenRequest{
		GrantType: "authorization_code", Code: code,
		RedirectURI: "https://app.feedback.example/callback?existing=1",
		ClientID:    client.ClientID, CodeVerifier: rfcVerifier,
	})
	requireOAuthError(t, err, "invalid_grant", 400)
	require.Empty(t, minter.created)
}

func TestExchangeCodeReplayRejected(t *testing.T) {
	svc, store, minter, _, _ := newServiceFixture(t, config.OAuthASConfig{})
	client := registerTestClient(t, svc)
	userID, projectID := uuid.NewString(), uuid.NewString()
	store.projects[projectID] = "Demo"
	store.writable[projectID+"|"+userID] = true
	code := codeFromRedirect(t, authorizeTestCode(t, svc, client.ClientID, userID, projectID).RedirectURL)

	req := TokenRequest{
		GrantType: "authorization_code", Code: code,
		RedirectURI: "https://app.feedback.example/callback?existing=1",
		ClientID:    client.ClientID, CodeVerifier: rfcVerifier,
	}
	_, err := svc.ExchangeCode(context.Background(), req)
	require.NoError(t, err)

	_, err = svc.ExchangeCode(context.Background(), req)
	requireOAuthError(t, err, "invalid_grant", 400)
	require.Len(t, minter.created, 1, "replay must not mint a second token")
}

func TestExchangeCodeRedirectMismatch(t *testing.T) {
	svc, store, minter, _, _ := newServiceFixture(t, config.OAuthASConfig{})
	client := registerTestClient(t, svc)
	userID, projectID := uuid.NewString(), uuid.NewString()
	store.projects[projectID] = "Demo"
	store.writable[projectID+"|"+userID] = true
	code := codeFromRedirect(t, authorizeTestCode(t, svc, client.ClientID, userID, projectID).RedirectURL)

	_, err := svc.ExchangeCode(context.Background(), TokenRequest{
		GrantType: "authorization_code", Code: code,
		RedirectURI: "https://app.feedback.example/other",
		ClientID:    client.ClientID, CodeVerifier: rfcVerifier,
	})
	requireOAuthError(t, err, "invalid_grant", 400)
	require.Empty(t, minter.created)
	require.Nil(t, store.codes[hashCode(code)].ConsumedAt, "a mismatched attempt must not burn the code")

	// The legitimate exchange still works.
	_, err = svc.ExchangeCode(context.Background(), TokenRequest{
		GrantType: "authorization_code", Code: code,
		RedirectURI: "https://app.feedback.example/callback?existing=1",
		ClientID:    client.ClientID, CodeVerifier: rfcVerifier,
	})
	require.NoError(t, err)
}

func TestExchangeCodePKCE(t *testing.T) {
	svc, store, minter, _, _ := newServiceFixture(t, config.OAuthASConfig{})
	client := registerTestClient(t, svc)
	userID, projectID := uuid.NewString(), uuid.NewString()
	store.projects[projectID] = "Demo"
	store.writable[projectID+"|"+userID] = true
	code := codeFromRedirect(t, authorizeTestCode(t, svc, client.ClientID, userID, projectID).RedirectURL)

	wrong := strings.Repeat("x", 43)
	_, err := svc.ExchangeCode(context.Background(), TokenRequest{
		GrantType: "authorization_code", Code: code,
		RedirectURI: "https://app.feedback.example/callback?existing=1",
		ClientID:    client.ClientID, CodeVerifier: wrong,
	})
	requireOAuthError(t, err, "invalid_grant", 400)
	require.Empty(t, minter.created)
	require.Nil(t, store.codes[hashCode(code)].ConsumedAt, "failed PKCE must not burn the code")

	if !verifyPKCE(rfcVerifier, rfcChallenge) {
		t.Fatal("RFC 7636 test vector must verify")
	}
	if verifyPKCE(wrong, rfcChallenge) {
		t.Fatal("wrong verifier must not verify")
	}
}

func TestExchangeCodeUnknownOrRevokedClient(t *testing.T) {
	svc, store, minter, _, _ := newServiceFixture(t, config.OAuthASConfig{})
	client := registerTestClient(t, svc)
	userID, projectID := uuid.NewString(), uuid.NewString()
	store.projects[projectID] = "Demo"
	store.writable[projectID+"|"+userID] = true
	code := codeFromRedirect(t, authorizeTestCode(t, svc, client.ClientID, userID, projectID).RedirectURL)

	revokedAt := time.Now()
	store.clients[client.ClientID].RevokedAt = &revokedAt

	_, err := svc.ExchangeCode(context.Background(), TokenRequest{
		GrantType: "authorization_code", Code: code,
		RedirectURI: "https://app.feedback.example/callback?existing=1",
		ClientID:    client.ClientID, CodeVerifier: rfcVerifier,
	})
	requireOAuthError(t, err, "invalid_client", 401)

	_, err = svc.ExchangeCode(context.Background(), TokenRequest{
		GrantType: "authorization_code", Code: code,
		RedirectURI: "https://app.feedback.example/callback?existing=1",
		ClientID:    "does-not-exist", CodeVerifier: rfcVerifier,
	})
	requireOAuthError(t, err, "invalid_client", 401)
	require.Empty(t, minter.created)
}

func TestAuthorizeRejectsInvalidInput(t *testing.T) {
	userID := uuid.NewString()

	cases := []struct {
		name     string
		mutate   func(*AuthorizeRequest)
		wantCode string
		wantHTTP int
	}{
		{"missing challenge", func(r *AuthorizeRequest) { r.CodeChallenge = "" }, "invalid_request", 400},
		{"unsupported challenge method", func(r *AuthorizeRequest) { r.CodeChallengeMethod = "plain" }, "invalid_request", 400},
		{"invalid project id", func(r *AuthorizeRequest) { r.ProjectID = "not-a-uuid" }, "invalid_request", 400},
		{"empty scopes", func(r *AuthorizeRequest) { r.Scopes = nil }, "invalid_scope", 400},
		{"scope outside allowlist", func(r *AuthorizeRequest) { r.Scopes = []string{"admin:all"} }, "invalid_scope", 400},
		{"unregistered redirect", func(r *AuthorizeRequest) { r.RedirectURI = "https://evil.example/cb" }, "invalid_request", 400},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, store, _, _, _ := newServiceFixture(t, config.OAuthASConfig{})
			client := registerTestClient(t, svc)
			projectID := uuid.NewString()
			store.projects[projectID] = "Demo"
			store.writable[projectID+"|"+userID] = true

			req := AuthorizeRequest{
				ClientID:            client.ClientID,
				RedirectURI:         "https://app.feedback.example/callback?existing=1",
				ProjectID:           projectID,
				Scopes:              []string{"graph:read"},
				CodeChallenge:       rfcChallenge,
				CodeChallengeMethod: "S256",
			}
			tc.mutate(&req)
			_, err := svc.Authorize(context.Background(), userID, req)
			requireOAuthError(t, err, tc.wantCode, tc.wantHTTP)
		})
	}
}

func TestAuthorizeRequiresWriteAccess(t *testing.T) {
	svc, store, _, _, _ := newServiceFixture(t, config.OAuthASConfig{})
	client := registerTestClient(t, svc)
	userID, projectID := uuid.NewString(), uuid.NewString()
	store.projects[projectID] = "Read only"
	// writable is false: viewer or no membership.

	_, err := svc.Authorize(context.Background(), userID, AuthorizeRequest{
		ClientID:            client.ClientID,
		RedirectURI:         "https://app.feedback.example/callback?existing=1",
		ProjectID:           projectID,
		Scopes:              []string{"graph:read"},
		CodeChallenge:       rfcChallenge,
		CodeChallengeMethod: "S256",
	})
	requireOAuthError(t, err, "access_denied", 403)
}

func TestAuthorizeCannotWidenRegisteredClientScope(t *testing.T) {
	svc, store, _, _, _ := newServiceFixture(t, config.OAuthASConfig{})
	client, err := svc.RegisterClient(context.Background(), ClientRegistrationRequest{
		ClientName:   "narrow app",
		RedirectURIs: []string{"https://app.feedback.example/callback"},
		Scope:        "graph:read",
	})
	require.NoError(t, err)
	userID, projectID := uuid.NewString(), uuid.NewString()
	store.projects[projectID] = "Demo"
	store.writable[projectID+"|"+userID] = true

	_, err = svc.Authorize(context.Background(), userID, AuthorizeRequest{
		ClientID:            client.ClientID,
		RedirectURI:         "https://app.feedback.example/callback",
		ProjectID:           projectID,
		Scopes:              []string{"graph:write"},
		CodeChallenge:       rfcChallenge,
		CodeChallengeMethod: "S256",
	})
	requireOAuthError(t, err, "invalid_scope", 400)

	_, err = svc.Authorize(context.Background(), userID, AuthorizeRequest{
		ClientID:            client.ClientID,
		RedirectURI:         "https://app.feedback.example/callback",
		ProjectID:           projectID,
		Scopes:              []string{"graph:read"},
		CodeChallenge:       rfcChallenge,
		CodeChallengeMethod: "S256",
	})
	require.NoError(t, err)
}

func TestAuthorizeContext(t *testing.T) {
	svc, store, _, _, _ := newServiceFixture(t, config.OAuthASConfig{})
	client := registerTestClient(t, svc)
	userID := uuid.NewString()
	writableID, viewerID := uuid.NewString(), uuid.NewString()
	store.projects[writableID] = "My Project!"
	store.projects[viewerID] = "Viewer Only"
	store.writable[writableID+"|"+userID] = true

	resp, err := svc.AuthorizeContext(context.Background(), userID, client.ClientID,
		"https://app.feedback.example/callback?existing=1")
	require.NoError(t, err)
	require.Equal(t, client.ClientID, resp.ClientID)
	require.Equal(t, "emergent.feedback", resp.ClientName)
	require.Equal(t, SupportedScopes, resp.ScopesSupported)
	require.Len(t, resp.Projects, 1)
	require.Equal(t, writableID, resp.Projects[0].ID)
	require.Equal(t, "my-project", resp.Projects[0].Slug)

	revokedAt := time.Now()
	store.clients[client.ClientID].RevokedAt = &revokedAt
	_, err = svc.AuthorizeContext(context.Background(), userID, client.ClientID,
		"https://app.feedback.example/callback?existing=1")
	requireOAuthError(t, err, "invalid_client", 401)
}

func TestExchangeCodeFallsBackOnTokenNameCollision(t *testing.T) {
	svc, store, minter, _, _ := newServiceFixture(t, config.OAuthASConfig{})
	client := registerTestClient(t, svc)
	userID, projectID := uuid.NewString(), uuid.NewString()
	store.projects[projectID] = "Demo"
	store.writable[projectID+"|"+userID] = true
	code := codeFromRedirect(t, authorizeTestCode(t, svc, client.ClientID, userID, projectID).RedirectURL)

	minter.err = apperror.New(409, "token_name_exists", "duplicate")
	_, err := svc.ExchangeCode(context.Background(), TokenRequest{
		GrantType: "authorization_code", Code: code,
		RedirectURI: "https://app.feedback.example/callback?existing=1",
		ClientID:    client.ClientID, CodeVerifier: rfcVerifier,
	})
	require.NoError(t, err)
	require.Len(t, minter.created, 1)
	require.Contains(t, minter.created[0].name, "oauth: emergent.feedback (")
}

func TestExchangeCodeMissingParameters(t *testing.T) {
	svc, _, _, _, _ := newServiceFixture(t, config.OAuthASConfig{})
	_, err := svc.ExchangeCode(context.Background(), TokenRequest{GrantType: "authorization_code"})
	requireOAuthError(t, err, "invalid_request", 400)
}

func TestListGrantsAndRevokeGrant(t *testing.T) {
	svc, store, minter, _, _ := newServiceFixture(t, config.OAuthASConfig{})
	client := registerTestClient(t, svc)
	userID, projectID := uuid.NewString(), uuid.NewString()
	store.projects[projectID] = "Demo Project"
	store.writable[projectID+"|"+userID] = true
	code := codeFromRedirect(t, authorizeTestCode(t, svc, client.ClientID, userID, projectID).RedirectURL)

	resp, err := svc.ExchangeCode(context.Background(), TokenRequest{
		GrantType: "authorization_code", Code: code,
		RedirectURI: "https://app.feedback.example/callback?existing=1",
		ClientID:    client.ClientID, CodeVerifier: rfcVerifier,
	})
	require.NoError(t, err)

	grants, err := svc.ListGrants(context.Background(), userID)
	require.NoError(t, err)
	require.Len(t, grants, 1)
	require.Equal(t, "emergent.feedback", grants[0].ClientName)
	require.Equal(t, "Demo Project", grants[0].ProjectName)
	require.Equal(t, []string{"graph:read", "projects:read"}, grants[0].Scopes)
	require.Nil(t, grants[0].RevokedAt)

	// Another user cannot revoke it.
	err = svc.RevokeGrant(context.Background(), uuid.NewString(), grants[0].ID)
	requireOAuthError(t, err, "not_found", 404)

	require.NoError(t, svc.RevokeGrant(context.Background(), userID, grants[0].ID))
	require.Len(t, minter.revoked, 1)
	require.NotNil(t, store.grants[grants[0].ID].RevokedAt)
	require.Equal(t, resp.AccessToken, "emt_test_access_token")
}

func TestRevokeGrantInvalidID(t *testing.T) {
	svc, _, _, _, _ := newServiceFixture(t, config.OAuthASConfig{})
	err := svc.RevokeGrant(context.Background(), uuid.NewString(), "not-a-uuid")
	requireOAuthError(t, err, "not_found", 404)
}

func TestRevokeTokenRFC7009(t *testing.T) {
	svc, store, minter, lookup, _ := newServiceFixture(t, config.OAuthASConfig{})
	client := registerTestClient(t, svc)
	userID, projectID := uuid.NewString(), uuid.NewString()
	store.projects[projectID] = "Demo"
	store.writable[projectID+"|"+userID] = true
	code := codeFromRedirect(t, authorizeTestCode(t, svc, client.ClientID, userID, projectID).RedirectURL)

	_, err := svc.ExchangeCode(context.Background(), TokenRequest{
		GrantType: "authorization_code", Code: code,
		RedirectURI: "https://app.feedback.example/callback?existing=1",
		ClientID:    client.ClientID, CodeVerifier: rfcVerifier,
	})
	require.NoError(t, err)
	require.Len(t, store.grants, 1)
	var tokenID string
	for _, g := range store.grants {
		tokenID = g.TokenID
	}
	lookup.tokens[hashCode("emt_test_access_token")] = &apitoken.ApiToken{
		ID: tokenID, ProjectID: &projectID, UserID: &userID,
	}

	require.NoError(t, svc.RevokeToken(context.Background(), client.ClientID, "emt_test_access_token"))
	require.Len(t, minter.revoked, 1)
	for _, g := range store.grants {
		require.NotNil(t, g.RevokedAt)
	}

	// Unknown token is accepted silently (not a token oracle).
	require.NoError(t, svc.RevokeToken(context.Background(), client.ClientID, "emt_unknown"))
	// Unknown client is rejected.
	err = svc.RevokeToken(context.Background(), "nope", "emt_test_access_token")
	requireOAuthError(t, err, "invalid_client", 401)
	// Missing fields.
	err = svc.RevokeToken(context.Background(), client.ClientID, "")
	requireOAuthError(t, err, "invalid_request", 400)
}

func TestValidateScopeList(t *testing.T) {
	got, oerr := validateScopeList([]string{"graph:read", "graph:read", "projects:read"})
	require.Nil(t, oerr)
	require.Equal(t, []string{"graph:read", "projects:read"}, got)

	_, oerr = validateScopeList([]string{"graph:read", "admin"})
	require.NotNil(t, oerr)
	require.Equal(t, "invalid_scope", oerr.Code)
}

func TestSlugify(t *testing.T) {
	require.Equal(t, "my-project", slugify("My Project!"))
	require.Equal(t, "a-b-2", slugify("  A / B -- 2  "))
	require.Equal(t, "project", slugify("!!!"))
}

func requireOAuthError(t *testing.T, err error, code string, status int) {
	t.Helper()
	require.Error(t, err)
	var oe *OAuthError
	require.True(t, errors.As(err, &oe), "expected OAuthError, got %T: %v", err, err)
	require.Equal(t, code, oe.Code)
	require.Equal(t, status, oe.Status)
}
