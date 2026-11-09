package sources

import (
	"context"
	"encoding/hex"
	"testing"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// namedAppStore is an in-memory store implementing both the legacy and named-app
// surfaces, so resolution exercises the vendor-level path.
type namedAppStore struct {
	orgID       uuid.UUID
	orgApps     []*OAuthClientConfig
	projectApps map[uuid.UUID][]*OAuthClientConfig
}

func (f *namedAppStore) IsOrgAdmin(context.Context, string, string) (bool, error) { return true, nil }
func (f *namedAppStore) IsProjectAdmin(context.Context, string, string) (bool, error) {
	return true, nil
}
func (f *namedAppStore) ResolveOrgID(context.Context, uuid.UUID) (uuid.UUID, error) {
	return f.orgID, nil
}

func (f *namedAppStore) GetOAuthClientConfig(context.Context, uuid.UUID, string) (*OAuthClientConfig, error) {
	return nil, apperror.ErrNotFound
}
func (f *namedAppStore) ListOAuthClientConfigs(context.Context, uuid.UUID) ([]*OAuthClientConfig, error) {
	return f.orgApps, nil
}
func (f *namedAppStore) UpsertOAuthClientConfig(_ context.Context, cfg *OAuthClientConfig) (*OAuthClientConfig, error) {
	f.orgApps = append(f.orgApps, cfg)
	return cfg, nil
}
func (f *namedAppStore) DeleteOAuthClientConfig(context.Context, uuid.UUID, string) error {
	return apperror.ErrNotFound
}
func (f *namedAppStore) GetProjectOAuthClientConfig(context.Context, uuid.UUID, string) (*OAuthClientConfig, error) {
	return nil, apperror.ErrNotFound
}
func (f *namedAppStore) ListProjectOAuthClientConfigs(_ context.Context, projectID uuid.UUID) ([]*OAuthClientConfig, error) {
	return f.projectApps[projectID], nil
}
func (f *namedAppStore) UpsertProjectOAuthClientConfig(_ context.Context, cfg *OAuthClientConfig) (*OAuthClientConfig, error) {
	return cfg, nil
}
func (f *namedAppStore) DeleteProjectOAuthClientConfig(context.Context, uuid.UUID, string) error {
	return apperror.ErrNotFound
}

func (f *namedAppStore) GetOAuthClientConfigByID(_ context.Context, orgID, id uuid.UUID) (*OAuthClientConfig, error) {
	for _, app := range f.orgApps {
		if app.ID == id && app.OrganizationID == orgID {
			return app, nil
		}
	}
	return nil, apperror.ErrNotFound
}
func (f *namedAppStore) ListOAuthClientConfigsByVendor(_ context.Context, orgID uuid.UUID, vendor string) ([]*OAuthClientConfig, error) {
	out := []*OAuthClientConfig{}
	for _, app := range f.orgApps {
		if app.OrganizationID == orgID && app.Vendor == vendor {
			out = append(out, app)
		}
	}
	return out, nil
}
func (f *namedAppStore) CreateOAuthClientConfig(_ context.Context, cfg *OAuthClientConfig) (*OAuthClientConfig, error) {
	if cfg.ID == uuid.Nil {
		cfg.ID = uuid.New()
	}
	f.orgApps = append(f.orgApps, cfg)
	return cfg, nil
}
func (f *namedAppStore) UpdateOAuthClientConfig(_ context.Context, cfg *OAuthClientConfig) (*OAuthClientConfig, error) {
	return cfg, nil
}
func (f *namedAppStore) DeleteOAuthClientConfigByID(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}
func (f *namedAppStore) CountSourcesUsingOAuthConfig(context.Context, uuid.UUID) (int, error) {
	return 0, nil
}

func (f *namedAppStore) GetProjectOAuthClientConfigByID(_ context.Context, projectID, id uuid.UUID) (*OAuthClientConfig, error) {
	for _, app := range f.projectApps[projectID] {
		if app.ID == id {
			return app, nil
		}
	}
	return nil, apperror.ErrNotFound
}
func (f *namedAppStore) ListProjectOAuthClientConfigsByVendor(_ context.Context, projectID uuid.UUID, vendor string) ([]*OAuthClientConfig, error) {
	out := []*OAuthClientConfig{}
	for _, app := range f.projectApps[projectID] {
		if app.Vendor == vendor {
			out = append(out, app)
		}
	}
	return out, nil
}
func (f *namedAppStore) CreateProjectOAuthClientConfig(_ context.Context, cfg *OAuthClientConfig) (*OAuthClientConfig, error) {
	if cfg.ID == uuid.Nil {
		cfg.ID = uuid.New()
	}
	if cfg.ProjectID != nil {
		f.projectApps[*cfg.ProjectID] = append(f.projectApps[*cfg.ProjectID], cfg)
	}
	return cfg, nil
}
func (f *namedAppStore) UpdateProjectOAuthClientConfig(_ context.Context, cfg *OAuthClientConfig) (*OAuthClientConfig, error) {
	return cfg, nil
}
func (f *namedAppStore) DeleteProjectOAuthClientConfigByID(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

func newNamedAppService(t *testing.T, store *namedAppStore) *Service {
	t.Helper()
	cfg := &config.Config{SourceOAuth: config.SourceOAuthConfig{
		GoogleClientID:     "deploy-id",
		GoogleClientSecret: "deploy-secret",
	}}
	cfg.LLMProvider.EncryptionKey = hex.EncodeToString(testEncryptionKey())
	return NewService(nil, cfg, discardLog(), NewDefaultRegistry(),
		WithOrgOAuthClientStore(store), WithProjectOAuthClientStore(store))
}

func namedApp(t *testing.T, svc *Service, orgID uuid.UUID, projectID *uuid.UUID, vendor, name, clientID string) *OAuthClientConfig {
	t.Helper()
	blob, err := svc.encryptCredentials(Credentials{oauthClientIDKey: clientID, oauthClientSecretKey: "secret-" + clientID})
	if err != nil {
		t.Fatalf("encryptCredentials: %v", err)
	}
	app := &OAuthClientConfig{
		ID:              uuid.New(),
		OrganizationID:  orgID,
		ProjectID:       projectID,
		Vendor:          vendor,
		Name:            name,
		Provider:        vendor,
		EncryptedClient: blob,
		ProviderConfigs: map[string]OAuthProviderConfig{},
	}
	return app
}

func TestResolveOAuthClient_NamedApps(t *testing.T) {
	orgID, projectID := uuid.New(), uuid.New()
	store := &namedAppStore{orgID: orgID, projectApps: map[uuid.UUID][]*OAuthClientConfig{}}
	svc := newNamedAppService(t, store)

	prod := namedApp(t, svc, orgID, &projectID, "google", "Production", "prod-id")
	store.projectApps[projectID] = []*OAuthClientConfig{prod}

	got, err := svc.resolveOAuthClient(context.Background(), projectID, ProviderTypeGoogleDrive, nil)
	if err != nil {
		t.Fatalf("sole project app: %v", err)
	}
	if got.ClientID != "prod-id" {
		t.Fatalf("ClientID = %q, want prod-id", got.ClientID)
	}

	// Explicit selection wins even when the app is org-scoped.
	orgApp := namedApp(t, svc, orgID, nil, "google", "Org", "org-id")
	store.orgApps = []*OAuthClientConfig{orgApp}
	got, err = svc.resolveOAuthClient(context.Background(), projectID, ProviderTypeGmail, &orgApp.ID)
	if err != nil {
		t.Fatalf("explicit org app: %v", err)
	}
	if got.ClientID != "org-id" {
		t.Fatalf("ClientID = %q, want org-id", got.ClientID)
	}

	// Two project apps and no selection is an ambiguity.
	second := namedApp(t, svc, orgID, &projectID, "google", "Sandbox", "sandbox-id")
	store.projectApps[projectID] = []*OAuthClientConfig{prod, second}
	if _, err := svc.resolveOAuthClient(context.Background(), projectID, ProviderTypeGoogleDrive, nil); err == nil {
		t.Fatal("two project apps must require an explicit choice")
	}
}

func TestResolveOAuthClient_NamedAppVendorAndVisibility(t *testing.T) {
	orgID, projectID, otherProject := uuid.New(), uuid.New(), uuid.New()
	store := &namedAppStore{orgID: orgID, projectApps: map[uuid.UUID][]*OAuthClientConfig{}}
	svc := newNamedAppService(t, store)

	google := namedApp(t, svc, orgID, &projectID, "google", "Google", "gid")
	store.projectApps[projectID] = []*OAuthClientConfig{google}
	other := namedApp(t, svc, orgID, &otherProject, "google", "Other", "oid")
	store.projectApps[otherProject] = []*OAuthClientConfig{other}

	if _, err := svc.resolveOAuthClient(context.Background(), projectID, ProviderTypeGmail, &other.ID); err == nil {
		t.Fatal("an app owned by another project must be rejected")
	}

	github := namedApp(t, svc, orgID, &projectID, "github", "GitHub", "hid")
	store.projectApps[projectID] = []*OAuthClientConfig{google, github}
	if _, err := svc.resolveOAuthClient(context.Background(), projectID, ProviderTypeGmail, &github.ID); err == nil {
		t.Fatal("a vendor-mismatched app must be rejected")
	}
}

func TestResolveOAuthClient_DeploymentFallbackWithNamedStore(t *testing.T) {
	orgID, projectID := uuid.New(), uuid.New()
	store := &namedAppStore{orgID: orgID, projectApps: map[uuid.UUID][]*OAuthClientConfig{}}
	svc := newNamedAppService(t, store)

	got, err := svc.resolveOAuthClient(context.Background(), projectID, ProviderTypeGoogleDrive, nil)
	if err != nil {
		t.Fatalf("deployment fallback: %v", err)
	}
	if got.ClientID != "deploy-id" {
		t.Fatalf("ClientID = %q, want deploy-id", got.ClientID)
	}
}

func TestResolveOAuthClient_AppProviderStrategy(t *testing.T) {
	orgID, projectID := uuid.New(), uuid.New()
	store := &namedAppStore{orgID: orgID, projectApps: map[uuid.UUID][]*OAuthClientConfig{}}
	svc := newNamedAppService(t, store)

	app := namedApp(t, svc, orgID, &projectID, "google", "Drive file", "gid")
	app.ProviderConfigs["google_drive"] = OAuthProviderConfig{Strategy: string(ScopeStrategyDriveFile)}
	store.projectApps[projectID] = []*OAuthClientConfig{app}

	got, err := svc.resolveOAuthClient(context.Background(), projectID, ProviderTypeGoogleDrive, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(got.Scopes) != 1 || got.Scopes[0] != googleDriveFile {
		t.Fatalf("Scopes = %v, want [%s]", got.Scopes, googleDriveFile)
	}
}
