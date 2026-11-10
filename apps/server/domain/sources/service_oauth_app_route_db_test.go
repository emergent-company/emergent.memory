package sources

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// oauthAppRouteFixture is a DB-backed service wired with the real named-app
// store, a real org/project pair, and a deployment Google client that must never
// win over the named app under test.
type oauthAppRouteFixture struct {
	svc       *Service
	repo      *Repository
	orgID     uuid.UUID
	projectID uuid.UUID
}

func newOAuthAppRouteFixture(t *testing.T, suffix, tokenURL string, httpClient *http.Client) *oauthAppRouteFixture {
	t.Helper()
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, suffix)
	t.Cleanup(tdb.Close)

	orgID, projectID := uuid.New(), uuid.New()
	// Inlined instead of testutil.CreateTestProject: testutil imports this
	// package for its server harness, which would make this in-package test an
	// import cycle. The rows are the same two inserts.
	if _, err := tdb.DB.NewRaw(`
		INSERT INTO kb.orgs (id, name, created_at, updated_at)
		VALUES (?, 'Test Organization', NOW(), NOW())
	`, orgID).Exec(ctx); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	if _, err := tdb.DB.NewRaw(`
		INSERT INTO kb.projects (id, name, organization_id, project_info, created_at, updated_at)
		VALUES (?, 'Named OAuth app routing', ?, NULL, NOW(), NOW())
	`, projectID, orgID).Exec(ctx); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 3)
	}
	cfg := &config.Config{
		LLMProvider: config.LLMProviderConfig{EncryptionKey: hex.EncodeToString(key)},
		SourceOAuth: config.SourceOAuthConfig{RedirectBaseURL: "https://api.example"},
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := NewRepository(tdb.DB, log)
	registry := NewDefaultRegistry()
	// Named apps carry only client id/secret; the token endpoint comes from the
	// provider strategy. Override the Drive strategy's endpoint so the exchange
	// hits the test server instead of Google, while the provider keeps its real
	// catalog (type + vendor) so named-app resolution is unchanged.
	drive, ok := registry.Get(ProviderTypeGoogleDrive)
	if !ok {
		t.Fatal("default registry has no Google Drive provider")
	}
	registry.Register(tokenURLProvider{SourceProvider: drive, tokenURL: tokenURL})
	svc := NewService(repo, cfg, log, registry,
		WithOAuthClients(map[string]OAuthClient{
			ProviderTypeGoogleDrive: {ClientID: "deploy-cid", ClientSecret: "deploy-cs", TokenURL: tokenURL},
		}),
		WithOAuthHTTPClient(httpClient),
		WithOrgOAuthClientStore(repo),
		WithProjectOAuthClientStore(repo),
	)
	return &oauthAppRouteFixture{svc: svc, repo: repo, orgID: orgID, projectID: projectID}
}

// tokenURLProvider delegates every provider call to the real provider but points
// its OAuth strategy's exchange at the test token server.
type tokenURLProvider struct {
	SourceProvider
	tokenURL string
}

func (p tokenURLProvider) OAuth() (OAuthStrategy, bool) {
	base, ok := p.SourceProvider.OAuth()
	if !ok {
		return nil, false
	}
	return tokenURLStrategy{OAuthStrategy: base, tokenURL: p.tokenURL}, true
}

// tokenURLStrategy overrides only the exchange endpoint; scopes and the
// authorize URL stay the provider's own.
type tokenURLStrategy struct {
	OAuthStrategy
	tokenURL string
}

func (s tokenURLStrategy) Exchange(ctx context.Context, httpClient *http.Client, client OAuthClient, code, codeVerifier, redirectURI string) (Credentials, error) {
	client.TokenURL = s.tokenURL
	return s.OAuthStrategy.Exchange(ctx, httpClient, client, code, codeVerifier, redirectURI)
}

// createOrgApp inserts a named, vendor-level app encrypted with the fixture's
// key, exactly as the create path would store it.
func (f *oauthAppRouteFixture) createOrgApp(t *testing.T, name, clientID, clientSecret string) *OAuthClientConfig {
	t.Helper()
	blob, err := f.svc.encryptCredentials(Credentials{
		oauthClientIDKey:     clientID,
		oauthClientSecretKey: clientSecret,
	})
	if err != nil {
		t.Fatalf("encryptCredentials() error = %v", err)
	}
	app, err := f.repo.CreateOAuthClientConfig(context.Background(), &OAuthClientConfig{
		OrganizationID:  f.orgID,
		Vendor:          "google",
		Name:            name,
		Provider:        "google",
		EncryptedClient: blob,
		ProviderConfigs: map[string]OAuthProviderConfig{},
	})
	if err != nil {
		t.Fatalf("CreateOAuthClientConfig() error = %v", err)
	}
	return app
}

// tokenServer records the client credentials the exchange actually used, so a
// test can prove the named app was selected rather than the deployment default.
type tokenServer struct {
	*httptest.Server
	mu           sync.Mutex
	clientIDs    []string
	clientSecret []string
}

func newTokenServer(t *testing.T) *tokenServer {
	t.Helper()
	ts := &tokenServer{}
	ts.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		ts.mu.Lock()
		ts.clientIDs = append(ts.clientIDs, r.Form.Get("client_id"))
		ts.clientSecret = append(ts.clientSecret, r.Form.Get("client_secret"))
		ts.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"at","refresh_token":"rt","expires_in":3599,"scope":"https://www.googleapis.com/auth/drive.readonly","token_type":"Bearer"}`)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func (ts *tokenServer) lastClient(t *testing.T) (string, string) {
	t.Helper()
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if len(ts.clientIDs) == 0 {
		t.Fatal("token endpoint was never called")
	}
	return ts.clientIDs[len(ts.clientIDs)-1], ts.clientSecret[len(ts.clientSecret)-1]
}

// TestCompleteOAuth_RoutesPendingStateToNamedApp asserts the callback routing
// contract for a named app: StartOAuth pins the selected app in the persisted
// pending state, CompleteOAuth consumes that state exactly once, exchanges with
// the app's client rather than the deployment default, and creates a source
// bound to the app without leaking the reserved flow key into its config.
func TestCompleteOAuth_RoutesPendingStateToNamedApp(t *testing.T) {
	tokens := newTokenServer(t)
	fx := newOAuthAppRouteFixture(t, "oauthapproute", tokens.URL, tokens.Client())
	ctx := context.Background()
	app := fx.createOrgApp(t, "Acme Google", "app-cid", "app-secret")

	start, err := fx.svc.StartOAuth(ctx, fx.projectID, ProviderTypeGoogleDrive, StartOAuthRequest{
		Config:     map[string]any{"name": "Team Drive"},
		OAuthAppID: stringPtr(app.ID.String()),
	})
	if err != nil {
		t.Fatalf("StartOAuth() error = %v", err)
	}
	state := authorizeState(t, start.AuthorizeURL)

	var pending OAuthPendingState
	if err := fx.repo.db.NewSelect().Model(&pending).Where("state = ?", state).Scan(ctx); err != nil {
		t.Fatalf("load pending state: %v", err)
	}
	if got := pending.Config[oauthPendingAppKey]; got != app.ID.String() {
		t.Fatalf("pending config[%s] = %v, want %s", oauthPendingAppKey, got, app.ID)
	}

	completion, err := fx.svc.CompleteOAuth(ctx, ProviderTypeGoogleDrive, state, "code-1")
	if err != nil {
		t.Fatalf("CompleteOAuth() error = %v", err)
	}
	if completion.Reconnected {
		t.Error("a fresh connect must not report itself as a reconnect")
	}
	if completion.Integration.OAuthAppID == nil || *completion.Integration.OAuthAppID != app.ID.String() {
		t.Errorf("created source oauthAppId = %v, want %s", completion.Integration.OAuthAppID, app.ID)
	}
	if completion.Integration.Name != "Team Drive" {
		t.Errorf("created source name = %q, want the pending config's name", completion.Integration.Name)
	}

	if gotID, gotSecret := tokens.lastClient(t); gotID != "app-cid" || gotSecret != "app-secret" {
		t.Errorf("token exchange used %q/%q, want the named app client", gotID, gotSecret)
	}

	src := mustGetSource(t, ctx, fx.repo, fx.projectID, uuid.MustParse(completion.Integration.ID))
	if src.OAuthAppID == nil || *src.OAuthAppID != app.ID {
		t.Errorf("source oauth_app_id = %v, want %s", src.OAuthAppID, app.ID)
	}
	if _, leaked := src.Config[oauthPendingAppKey]; leaked {
		t.Errorf("reserved flow key %q leaked into the source config", oauthPendingAppKey)
	}
	if src.Config["name"] != nil {
		t.Errorf("source config kept the name key: %v", src.Config["name"])
	}

	// The pending state is single-use: replaying the callback cannot mint a
	// second source with the same grant.
	if _, err := fx.svc.CompleteOAuth(ctx, ProviderTypeGoogleDrive, state, "code-1"); !errors.Is(err, apperror.ErrNotFound) {
		t.Errorf("replayed CompleteOAuth() error = %v, want not found", err)
	}
}

// TestCreateSource_BindsNamedVendorApp asserts a source created with an explicit
// app id is bound to that vendor-level app on the row and in the read DTO, so a
// later edit or reconnect resolves the same client.
func TestCreateSource_BindsNamedVendorApp(t *testing.T) {
	tokens := newTokenServer(t)
	fx := newOAuthAppRouteFixture(t, "oauthappbind", tokens.URL, tokens.Client())
	ctx := context.Background()
	app := fx.createOrgApp(t, "Acme Google", "app-cid", "app-secret")

	created, err := fx.svc.Create(ctx, fx.projectID, &CreateIntegrationRequest{
		Name:         "Docs",
		ProviderType: ProviderTypeGoogleDrive,
		Config: map[string]any{
			"folder_id":     "folder-a",
			"refresh_token": "rt",
			"client_id":     "app-cid",
			"client_secret": "app-secret",
		},
		OAuthAppID: stringPtr(app.ID.String()),
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.OAuthAppID == nil || *created.OAuthAppID != app.ID.String() {
		t.Fatalf("Create() oauthAppId = %v, want %s", created.OAuthAppID, app.ID)
	}

	src := mustGetSource(t, ctx, fx.repo, fx.projectID, uuid.MustParse(created.ID))
	if src.OAuthAppID == nil || *src.OAuthAppID != app.ID {
		t.Errorf("stored oauth_app_id = %v, want %s", src.OAuthAppID, app.ID)
	}

	read, err := fx.svc.Get(ctx, fx.projectID, uuid.MustParse(created.ID))
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if read.OAuthAppName != "Acme Google" {
		t.Errorf("read oauthAppName = %q, want the bound app's name", read.OAuthAppName)
	}
	client, err := fx.svc.resolveOAuthClient(ctx, fx.projectID, ProviderTypeGoogleDrive, src.OAuthAppID)
	if err != nil {
		t.Fatalf("resolveOAuthClient() error = %v", err)
	}
	if client.ClientID != "app-cid" {
		t.Errorf("resolved client id = %q, want the bound app's client", client.ClientID)
	}
}

// TestOAuthAppBoundSourceParksAndReconnects covers the park on the named-app
// path: a source bound to an app is parked as needs_reconnect, keeps the binding
// while parked, and a reconnect reuses the same app (through the pending state)
// to clear the park without silently switching clients.
func TestOAuthAppBoundSourceParksAndReconnects(t *testing.T) {
	tokens := newTokenServer(t)
	fx := newOAuthAppRouteFixture(t, "oauthapppark", tokens.URL, tokens.Client())
	ctx := context.Background()
	app := fx.createOrgApp(t, "Acme Google", "app-cid", "app-secret")

	created, err := fx.svc.Create(ctx, fx.projectID, &CreateIntegrationRequest{
		Name:         "Docs",
		ProviderType: ProviderTypeGoogleDrive,
		Config: map[string]any{
			"folder_id":     "folder-a",
			"refresh_token": "rt-old",
			"client_id":     "app-cid",
			"client_secret": "app-secret",
		},
		OAuthAppID: stringPtr(app.ID.String()),
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	srcID := uuid.MustParse(created.ID)

	if _, err := fx.repo.MarkSourceNeedsReconnect(ctx, fx.projectID, srcID, driveReconnectRequired, nil); err != nil {
		t.Fatalf("MarkSourceNeedsReconnect() error = %v", err)
	}
	parked := mustGetSource(t, ctx, fx.repo, fx.projectID, srcID)
	if parked.Status != SourceStatusNeedsReconnect {
		t.Fatalf("parked status = %q, want %q", parked.Status, SourceStatusNeedsReconnect)
	}
	if parked.OAuthAppID == nil || *parked.OAuthAppID != app.ID {
		t.Fatalf("park cleared the app binding: oauth_app_id = %v, want %s", parked.OAuthAppID, app.ID)
	}

	start, err := fx.svc.StartOAuth(ctx, fx.projectID, ProviderTypeGoogleDrive, StartOAuthRequest{
		ReconnectSourceID: &srcID,
	})
	if err != nil {
		t.Fatalf("StartOAuth(reconnect) error = %v", err)
	}
	state := authorizeState(t, start.AuthorizeURL)

	var pending OAuthPendingState
	if err := fx.repo.db.NewSelect().Model(&pending).Where("state = ?", state).Scan(ctx); err != nil {
		t.Fatalf("load pending state: %v", err)
	}
	if got := pending.Config[oauthPendingAppKey]; got != app.ID.String() {
		t.Errorf("reconnect pending app = %v, want the source's app %s", got, app.ID)
	}

	completion, err := fx.svc.CompleteOAuth(ctx, ProviderTypeGoogleDrive, state, "code-1")
	if err != nil {
		t.Fatalf("CompleteOAuth(reconnect) error = %v", err)
	}
	if !completion.Reconnected {
		t.Error("reconnect must report itself as a reconnect")
	}
	if gotID, _ := tokens.lastClient(t); gotID != "app-cid" {
		t.Errorf("reconnect exchanged with %q, want the bound app client", gotID)
	}

	repaired := mustGetSource(t, ctx, fx.repo, fx.projectID, srcID)
	if repaired.Status != SourceStatusActive {
		t.Errorf("status after reconnect = %q, want active", repaired.Status)
	}
	if repaired.LastError != nil {
		t.Errorf("last error after reconnect = %q, want cleared", *repaired.LastError)
	}
	if repaired.OAuthAppID == nil || *repaired.OAuthAppID != app.ID {
		t.Errorf("oauth_app_id after reconnect = %v, want %s", repaired.OAuthAppID, app.ID)
	}
	if _, leaked := repaired.Config[oauthPendingAppKey]; leaked {
		t.Errorf("reserved flow key %q leaked into the source config", oauthPendingAppKey)
	}
}
