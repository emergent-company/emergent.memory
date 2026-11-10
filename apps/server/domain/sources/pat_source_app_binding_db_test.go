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

// patTestProvider stands in for a credentials-only (PAT/API-key) provider. It
// shares the "github" vendor with the real OAuth GitHub provider, so a sole
// github app is visible to implicit resolution — exactly the shape #554 is
// about — but it advertises no OAuth auth method.
type patTestProvider struct{}

const patTestProviderType = "github_pat_test"

func (p *patTestProvider) Catalog() Provider {
	return Provider{
		Type:        patTestProviderType,
		Name:        "GitHub (PAT test)",
		Description: "Credentials-only provider used by the #554 regression test.",
		SourceType:  SourceTypeRepo,
		Vendor:      "github",
		VendorName:  "GitHub",
		Available:   true,
	}
}

func (p *patTestProvider) ConfigSchema() ProviderSchema {
	return ProviderSchema{Type: patTestProviderType}
}

func (p *patTestProvider) CredentialFields() []ProviderField {
	return []ProviderField{{Key: "access_token", Label: "Personal access token", Type: "string", WriteOnly: true, Required: true}}
}

func (p *patTestProvider) AuthMethods() []AuthMethod { return []AuthMethod{AuthMethodCredentials} }

func (p *patTestProvider) OAuth() (OAuthStrategy, bool) { return nil, false }

func (p *patTestProvider) ValidateConfig(map[string]any) error { return nil }

func (p *patTestProvider) ExtractCredentials(cfg map[string]any) (Credentials, error) {
	token, _ := cfg["access_token"].(string)
	return Credentials{"access_token": token}, nil
}

func (p *patTestProvider) CredentialKeys() []string { return []string{"access_token"} }

func (p *patTestProvider) ListResources(context.Context, Credentials, map[string]any, string, ConnectorDeps) ([]Resource, bool, error) {
	return nil, false, nil
}

func (p *patTestProvider) NewConnector(context.Context, Credentials, ConnectorDeps) (Connector, error) {
	return nil, errors.New("not implemented in test provider")
}

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

// TestCreateSource_NonOAuthProviderNotBoundToVendorApp is the regression guard
// for #554: a credentials-only source created while its vendor has exactly one
// app must not be persisted with oauth_app_id, and the app must stay deletable.
func TestCreateSource_NonOAuthProviderNotBoundToVendorApp(t *testing.T) {
	fx := newOAuthAppRouteFixture(t, "nonoauthbind", "", nil)
	ctx := context.Background()
	app := createVendorApp(t, fx, "github", "Acme GitHub", "gh-cid", "gh-secret")
	fx.svc.providers.Register(&patTestProvider{})

	// The sole github app is visible to the old implicit path: the real github
	// provider (which offers OAuth) resolves it. The NULL asserted below comes
	// from the credentials-only provider's auth methods, not a failed lookup.
	if got := fx.svc.implicitSourceAppID(ctx, fx.projectID, ProviderTypeGitHub); got == nil || *got != app.ID {
		t.Fatalf("implicitSourceAppID(github) = %v, want the sole app %s; the fixture is not exercising the bug", got, app.ID)
	}

	created, err := fx.svc.Create(ctx, fx.projectID, &CreateIntegrationRequest{
		Name:         "Docs via PAT",
		ProviderType: patTestProviderType,
		Config: map[string]any{
			"repository":   "acme/docs",
			"access_token": "ghp_test",
		},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.OAuthAppID != nil {
		t.Fatalf("Create() oauthAppId = %v, want nil for a credentials-only source", *created.OAuthAppID)
	}

	src := mustGetSource(t, ctx, fx.repo, fx.projectID, uuid.MustParse(created.ID))
	if src.OAuthAppID != nil {
		t.Fatalf("stored oauth_app_id = %v, want NULL for a credentials-only source", *src.OAuthAppID)
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

// TestCreateSource_OAuthProviderStillImplicitlyBound pins the load-bearing half
// of implicitSourceAppID: an OAuth source created without an explicit app is
// still bound to the vendor's sole app, and deleting that app is still refused
// with a message that names the source.
func TestCreateSource_OAuthProviderStillImplicitlyBound(t *testing.T) {
	fx := newOAuthAppRouteFixture(t, "oauthimplicit", "", nil)
	ctx := context.Background()
	app := createVendorApp(t, fx, "google", "Acme Google", "app-cid", "app-secret")

	created, err := fx.svc.Create(ctx, fx.projectID, &CreateIntegrationRequest{
		Name:         "Team drive",
		ProviderType: ProviderTypeGoogleDrive,
		Config: map[string]any{
			"folder_id":     "folder-a",
			"refresh_token": "rt",
			"client_id":     "app-cid",
			"client_secret": "app-secret",
		},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.OAuthAppID == nil || *created.OAuthAppID != app.ID.String() {
		t.Fatalf("Create() oauthAppId = %v, want the sole google app %s", created.OAuthAppID, app.ID)
	}

	src := mustGetSource(t, ctx, fx.repo, fx.projectID, uuid.MustParse(created.ID))
	if src.OAuthAppID == nil || *src.OAuthAppID != app.ID {
		t.Fatalf("stored oauth_app_id = %v, want %s", src.OAuthAppID, app.ID)
	}

	admin := uuid.New()
	grantOrgAdmin(t, fx, admin)
	err = fx.svc.DeleteOrgOAuthApp(ctx, fx.orgID, admin.String(), app.ID.String())
	if err == nil {
		t.Fatal("DeleteOrgOAuthApp() succeeded while an OAuth source uses the app")
	}
	if !strings.Contains(err.Error(), "Team drive") || !strings.Contains(err.Error(), created.ID) {
		t.Fatalf("delete refusal = %q, want it to name the source (name and id)", err.Error())
	}
	if _, gerr := fx.repo.GetOAuthClientConfigByID(ctx, fx.orgID, app.ID); gerr != nil {
		t.Fatalf("app missing after a refused delete: %v", gerr)
	}
}

// TestMigration00290_UnbindsNonOAuthSources covers the backfill: a pre-fix row
// that points a non-OAuth source at an app is detached, a genuine OAuth binding
// survives, and the detached app becomes deletable.
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
	// The test database is migrated to head; roll the backfill far enough back
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
	// One app referenced only by non-OAuth rows (the reported bug) and one app
	// that still backs a genuine OAuth source.
	patApp := insertApp("PAT-bound app")
	oauthApp := insertApp("OAuth-backed app")

	insertSource := func(provider, name string, appID uuid.UUID) uuid.UUID {
		t.Helper()
		id := uuid.New()
		if _, err := db.NewRaw(`
			INSERT INTO kb.sources (id, project_id, provider, display_name, oauth_app_id)
			VALUES (?, ?, ?, ?, ?)
		`, id, projectID, provider, name, appID).Exec(ctx); err != nil {
			t.Fatalf("insert %s source: %v", provider, err)
		}
		return id
	}
	webSource := insertSource(ProviderTypeWeb, "Docs site", patApp)
	appSource := insertSource("github_app", "GitHub App bot", patApp)
	driveSource := insertSource(ProviderTypeGoogleDrive, "Team drive", oauthApp)

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

	// Before the backfill the pre-fix rows pin the app, so the delete is refused
	// and the message names a source that never reads an OAuth client.
	beforeErr := svc.DeleteOrgOAuthApp(ctx, orgID, admin.String(), patApp.String())
	if beforeErr == nil {
		t.Fatal("DeleteOrgOAuthApp() succeeded before the backfill; the seeded rows do not reproduce #554")
	}
	if !strings.Contains(beforeErr.Error(), "Docs site") {
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

	if got := appIDOf(webSource); got != nil {
		t.Fatalf("web source oauth_app_id = %v, want NULL after the backfill", *got)
	}
	if got := appIDOf(appSource); got != nil {
		t.Fatalf("github_app source oauth_app_id = %v, want NULL after the backfill", *got)
	}
	if got := appIDOf(driveSource); got == nil || *got != oauthApp {
		t.Fatalf("google_drive source oauth_app_id = %v, want the genuine binding %s preserved", got, oauthApp)
	}

	// The previously PAT-bound app is now deletable, and the old refusal is
	// gone because no source references it...
	if err := svc.DeleteOrgOAuthApp(ctx, orgID, admin.String(), patApp.String()); err != nil {
		t.Fatalf("DeleteOrgOAuthApp(PAT-bound app) error = %v, want success", err)
	}
	// ...while the app a genuine OAuth source still uses is not.
	err := svc.DeleteOrgOAuthApp(ctx, orgID, admin.String(), oauthApp.String())
	if err == nil {
		t.Fatal("DeleteOrgOAuthApp(OAuth-backed app) succeeded, want a refusal")
	}
	if !strings.Contains(err.Error(), "Team drive") || !strings.Contains(err.Error(), driveSource.String()) {
		t.Fatalf("delete refusal = %q, want it to name the OAuth source (name and id)", err.Error())
	}
}
