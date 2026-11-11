package sources

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/migrate"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// createVendorApp inserts a named, vendor-level app encrypted with the
// fixture's key, exactly as the create path would store it.
func createVendorApp(t *testing.T, fx *oauthAppRouteFixture, vendor, name, clientID, clientSecret string) *OAuthClientConfig {
	t.Helper()
	blob, err := fx.svc.encryptCredentials(Credentials{
		oauthClientIDKey:     clientID,
		oauthClientSecretKey: clientSecret,
	})
	if err != nil {
		t.Fatalf("encryptCredentials() error = %v", err)
	}
	app, err := fx.repo.CreateOAuthClientConfig(context.Background(), &OAuthClientConfig{
		OrganizationID:  fx.orgID,
		Vendor:          vendor,
		Name:            name,
		Provider:        vendor,
		EncryptedClient: blob,
		ProviderConfigs: map[string]OAuthProviderConfig{},
	})
	if err != nil {
		t.Fatalf("CreateOAuthClientConfig() error = %v", err)
	}
	return app
}

// grantOrgAdmin gives userID org_admin authority so a delete reaches the guard
// under test rather than the authorization refusal.
func grantOrgAdmin(t *testing.T, fx *oauthAppRouteFixture, userID uuid.UUID) {
	t.Helper()
	if _, err := fx.repo.db.NewRaw(`
		INSERT INTO core.user_profiles (id, zitadel_user_id, display_name)
		VALUES (?, ?, 'OAuth app admin')
	`, userID, userID.String()).Exec(context.Background()); err != nil {
		t.Fatalf("insert user profile: %v", err)
	}
	if _, err := fx.repo.db.NewRaw(`
		INSERT INTO kb.organization_memberships (organization_id, user_id, role, created_at)
		VALUES (?, ?, 'org_admin', NOW())
	`, fx.orgID, userID).Exec(context.Background()); err != nil {
		t.Fatalf("insert org_admin membership: %v", err)
	}
}

// TestCreateSource_PATSourceNotBoundToVendorApp is the #554 regression guard:
// a GitHub personal-access-token source created while its vendor has exactly
// one app must not be persisted with oauth_app_id, and the app must stay
// deletable. GitHub advertises both OAuth and credentials, so this is the
// per-source gate, not a provider one.
func TestCreateSource_PATSourceNotBoundToVendorApp(t *testing.T) {
	fx := newOAuthAppRouteFixture(t, "patbind", "", nil)
	ctx := context.Background()
	app := createVendorApp(t, fx, "github", "Acme GitHub", "gh-cid", "gh-secret")

	created, err := fx.svc.Create(ctx, fx.projectID, &CreateIntegrationRequest{
		Name:         "Docs via PAT",
		ProviderType: ProviderTypeGitHub,
		Config: map[string]any{
			"repo":         "acme/docs",
			"access_token": "ghp_test",
		},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.OAuthAppID != nil {
		t.Fatalf("Create() oauthAppId = %v, want nil for a PAT source", *created.OAuthAppID)
	}

	src := mustGetSource(t, ctx, fx.repo, fx.projectID, uuid.MustParse(created.ID))
	if src.OAuthAppID != nil {
		t.Fatalf("stored oauth_app_id = %v, want NULL for a PAT source", *src.OAuthAppID)
	}

	// The whole point: the app the old implicit path would have attached is now
	// deletable, because no row references it.
	admin := uuid.New()
	grantOrgAdmin(t, fx, admin)
	if err := fx.svc.DeleteOrgOAuthApp(ctx, fx.orgID, admin.String(), app.ID.String()); err != nil {
		t.Fatalf("DeleteOrgOAuthApp() error = %v, want the app to be deletable", err)
	}
	if _, err := fx.repo.GetOAuthClientConfigByID(ctx, fx.orgID, app.ID); !errors.Is(err, apperror.ErrNotFound) {
		t.Fatalf("app still present after delete: err = %v", err)
	}
}

// TestCompleteOAuth_NoChosenAppStillImplicitlyBound pins the load-bearing half
// of implicitSourceAppID: an OAuth completion that did not choose an app is
// still bound to the vendor's sole app, and deleting that app is still refused
// with a message that names the source.
func TestCompleteOAuth_NoChosenAppStillImplicitlyBound(t *testing.T) {
	tokens := newTokenServer(t)
	fx := newOAuthAppRouteFixture(t, "oauthimplicit", tokens.URL, tokens.Client())
	ctx := context.Background()
	app := createVendorApp(t, fx, "google", "Acme Google", "app-cid", "app-secret")

	start, err := fx.svc.StartOAuth(ctx, fx.projectID, ProviderTypeGoogleDrive, StartOAuthRequest{
		Config: map[string]any{"name": "Team drive"},
	})
	if err != nil {
		t.Fatalf("StartOAuth() error = %v", err)
	}
	completion, err := fx.svc.CompleteOAuth(ctx, ProviderTypeGoogleDrive, authorizeState(t, start.AuthorizeURL), "code-1")
	if err != nil {
		t.Fatalf("CompleteOAuth() error = %v", err)
	}
	if completion.Reconnected {
		t.Error("a fresh connect must not report itself as a reconnect")
	}
	if completion.Integration.OAuthAppID == nil || *completion.Integration.OAuthAppID != app.ID.String() {
		t.Fatalf("created source oauthAppId = %v, want the sole google app %s", completion.Integration.OAuthAppID, app.ID)
	}
	if gotID, _ := tokens.lastClient(t); gotID != "app-cid" {
		t.Errorf("token exchange used %q, want the sole app's client", gotID)
	}

	src := mustGetSource(t, ctx, fx.repo, fx.projectID, uuid.MustParse(completion.Integration.ID))
	if src.OAuthAppID == nil || *src.OAuthAppID != app.ID {
		t.Fatalf("stored oauth_app_id = %v, want %s", src.OAuthAppID, app.ID)
	}

	admin := uuid.New()
	grantOrgAdmin(t, fx, admin)
	err = fx.svc.DeleteOrgOAuthApp(ctx, fx.orgID, admin.String(), app.ID.String())
	if err == nil {
		t.Fatal("DeleteOrgOAuthApp() succeeded while an OAuth source uses the app")
	}
	if !strings.Contains(err.Error(), "Team drive") || !strings.Contains(err.Error(), completion.Integration.ID) {
		t.Fatalf("delete refusal = %q, want it to name the source (name and id)", err.Error())
	}
	if _, gerr := fx.repo.GetOAuthClientConfigByID(ctx, fx.orgID, app.ID); gerr != nil {
		t.Fatalf("app missing after a refused delete: %v", gerr)
	}
}

// TestMigration00290_UnbindsNonOAuthSources covers the backfill: a pre-fix
// GitHub PAT row that points at an app is detached, genuine OAuth rows keep
// their bindings, and the detached app becomes deletable.
func TestMigration00290_UnbindsNonOAuthSources(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "unbind_non_oauth")
	t.Cleanup(tdb.Close)
	db := tdb.DB

	orgID, projectID := uuid.New(), uuid.New()
	if _, err := db.NewRaw(`
		INSERT INTO kb.orgs (id, name, created_at, updated_at)
		VALUES (?, 'Unbind Organization', NOW(), NOW())
	`, orgID).Exec(ctx); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	if _, err := db.NewRaw(`
		INSERT INTO kb.projects (id, name, organization_id, project_info, created_at, updated_at)
		VALUES (?, 'Unbind project', ?, NULL, NOW(), NOW())
	`, projectID, orgID).Exec(ctx); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	mig := migrate.NewMigrator(db, zap.NewNop())
	// The test database is migrated to head; roll the backfill back far enough
	// that the seeded pre-fix rows are the ones it sees.
	if err := mig.DownTo(ctx, 289); err != nil {
		t.Fatalf("DownTo(289): %v", err)
	}

	insertApp := func(name string) uuid.UUID {
		t.Helper()
		id := uuid.New()
		if _, err := db.NewRaw(`
			INSERT INTO kb.oauth_client_configs
				(id, organization_id, project_id, vendor, name, provider, encrypted_client, scopes, strategy, provider_configs)
			VALUES (?, ?, NULL, 'google', ?, 'google', '\x00'::bytea, '[]'::jsonb, '', '{}'::jsonb)
		`, id, orgID, name).Exec(ctx); err != nil {
			t.Fatalf("insert app %s: %v", name, err)
		}
		return id
	}
	// One app referenced only by the pre-fix PAT row and one app still backing
	// genuine OAuth sources.
	patApp := insertApp("PAT-bound app")
	oauthApp := insertApp("OAuth-backed app")

	insertSource := func(provider, name, configJSON string, appID uuid.UUID) uuid.UUID {
		t.Helper()
		id := uuid.New()
		if _, err := db.NewRaw(`
			INSERT INTO kb.sources (id, project_id, provider, display_name, config, oauth_app_id)
			VALUES (?, ?, ?, ?, ?::jsonb, ?)
		`, id, projectID, provider, name, configJSON, appID).Exec(ctx); err != nil {
			t.Fatalf("insert %s source: %v", provider, err)
		}
		return id
	}
	// The reported case: a real GitHub PAT source carries no OAuth grant
	// metadata, so the old code bound it to the sole github app.
	patSource := insertSource(ProviderTypeGitHub, "Docs via PAT", `{"repo":"acme/docs"}`, patApp)
	// A GitHub OAuth completion records the granted scope; it must survive.
	githubOAuth := insertSource(ProviderTypeGitHub, "GitHub repo", `{"repo":"acme/docs","oauth_granted_scopes":["repo"]}`, oauthApp)
	// A Drive OAuth source records scope + strategy; it must survive too.
	driveSource := insertSource(ProviderTypeGoogleDrive, "Team drive",
		`{"folder_id":"folder-a","oauth_granted_scopes":["https://www.googleapis.com/auth/drive.readonly"],"oauth_granted_strategy":"drive.file"}`,
		oauthApp)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := NewRepository(db, log)
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 7)
	}
	svc := NewService(repo, &config.Config{
		LLMProvider: config.LLMProviderConfig{EncryptionKey: hex.EncodeToString(key)},
	}, log, NewDefaultRegistry(),
		WithOrgOAuthClientStore(repo), WithProjectOAuthClientStore(repo))

	admin := uuid.New()
	if _, err := db.NewRaw(`
		INSERT INTO core.user_profiles (id, zitadel_user_id, display_name)
		VALUES (?, ?, 'Unbind admin')
	`, admin, admin.String()).Exec(ctx); err != nil {
		t.Fatalf("insert user profile: %v", err)
	}
	if _, err := db.NewRaw(`
		INSERT INTO kb.organization_memberships (organization_id, user_id, role, created_at)
		VALUES (?, ?, 'org_admin', NOW())
	`, orgID, admin).Exec(ctx); err != nil {
		t.Fatalf("insert org_admin membership: %v", err)
	}

	// Before the backfill the PAT row pins the app, so the delete is refused and
	// the message names a source that never reads an OAuth client.
	beforeErr := svc.DeleteOrgOAuthApp(ctx, orgID, admin.String(), patApp.String())
	if beforeErr == nil {
		t.Fatal("DeleteOrgOAuthApp() succeeded before the backfill; the seeded rows do not reproduce #554")
	}
	if !strings.Contains(beforeErr.Error(), "Docs via PAT") {
		t.Fatalf("pre-backfill refusal = %q, want it to name the PAT source", beforeErr.Error())
	}

	if err := mig.UpTo(ctx, 290); err != nil {
		t.Fatalf("UpTo(290): %v", err)
	}

	appIDOf := func(id uuid.UUID) *uuid.UUID {
		t.Helper()
		var row struct {
			OAuthAppID *uuid.UUID `bun:"oauth_app_id"`
		}
		if err := db.NewRaw(`SELECT oauth_app_id FROM kb.sources WHERE id = ?`, id).Scan(ctx, &row); err != nil {
			t.Fatalf("read oauth_app_id: %v", err)
		}
		return row.OAuthAppID
	}

	if got := appIDOf(patSource); got != nil {
		t.Fatalf("PAT source oauth_app_id = %v, want NULL after the backfill", *got)
	}
	if got := appIDOf(githubOAuth); got == nil || *got != oauthApp {
		t.Fatalf("GitHub OAuth source oauth_app_id = %v, want its binding %s preserved", got, oauthApp)
	}
	if got := appIDOf(driveSource); got == nil || *got != oauthApp {
		t.Fatalf("google_drive source oauth_app_id = %v, want its binding %s preserved", got, oauthApp)
	}

	// The previously PAT-bound app is now deletable...
	if err := svc.DeleteOrgOAuthApp(ctx, orgID, admin.String(), patApp.String()); err != nil {
		t.Fatalf("DeleteOrgOAuthApp(PAT-bound app) error = %v, want success", err)
	}
	// ...while the app the OAuth sources still use is not.
	err := svc.DeleteOrgOAuthApp(ctx, orgID, admin.String(), oauthApp.String())
	if err == nil {
		t.Fatal("DeleteOrgOAuthApp(OAuth-backed app) succeeded, want a refusal")
	}
	for _, want := range []string{"GitHub repo", "Team drive", githubOAuth.String(), driveSource.String()} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("delete refusal = %q, want it to name %q", err.Error(), want)
		}
	}
}
