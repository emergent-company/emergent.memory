package sources

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// fakeOrgOAuthStore is an in-memory OrgOAuthClientStore for unit tests.
type fakeOrgOAuthStore struct {
	admin      bool
	adminErr   error
	resolveErr error
	orgID      uuid.UUID
	clients    map[string]*OAuthClientConfig
}

func orgClientKey(orgID uuid.UUID, provider string) string { return orgID.String() + "|" + provider }

func (f *fakeOrgOAuthStore) IsOrgAdmin(ctx context.Context, orgID, userID string) (bool, error) {
	return f.admin, f.adminErr
}

func (f *fakeOrgOAuthStore) ResolveOrgID(ctx context.Context, projectID uuid.UUID) (uuid.UUID, error) {
	if f.resolveErr != nil {
		return uuid.Nil, f.resolveErr
	}
	return f.orgID, nil
}

func (f *fakeOrgOAuthStore) GetOAuthClientConfig(ctx context.Context, orgID uuid.UUID, provider string) (*OAuthClientConfig, error) {
	c, ok := f.clients[orgClientKey(orgID, provider)]
	if !ok {
		return nil, apperror.ErrNotFound
	}
	return c, nil
}

func (f *fakeOrgOAuthStore) ListOAuthClientConfigs(ctx context.Context, orgID uuid.UUID) ([]*OAuthClientConfig, error) {
	out := []*OAuthClientConfig{}
	for k, c := range f.clients {
		if strings.HasPrefix(k, orgID.String()+"|") {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeOrgOAuthStore) UpsertOAuthClientConfig(ctx context.Context, cfg *OAuthClientConfig) (*OAuthClientConfig, error) {
	if f.clients == nil {
		f.clients = map[string]*OAuthClientConfig{}
	}
	now := time.Now().UTC()
	cfg.CreatedAt, cfg.UpdatedAt = now, now
	f.clients[orgClientKey(cfg.OrganizationID, cfg.Provider)] = cfg
	return cfg, nil
}

func (f *fakeOrgOAuthStore) DeleteOAuthClientConfig(ctx context.Context, orgID uuid.UUID, provider string) error {
	k := orgClientKey(orgID, provider)
	if _, ok := f.clients[k]; !ok {
		return apperror.ErrNotFound
	}
	delete(f.clients, k)
	return nil
}

func testEncryptionKey() []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	return key
}

func newOrgClientService(store OrgOAuthClientStore, oauthCfg config.SourceOAuthConfig) *Service {
	cfg := &config.Config{SourceOAuth: oauthCfg}
	cfg.LLMProvider.EncryptionKey = hex.EncodeToString(testEncryptionKey())
	return NewService(nil, cfg, discardLog(), NewDefaultRegistry(), WithOrgOAuthClientStore(store))
}

// mustEncryptOrgClient builds an encrypted org client row for the fake store.
func mustEncryptOrgClient(t *testing.T, svc *Service, orgID uuid.UUID, provider string, creds Credentials) *OAuthClientConfig {
	t.Helper()
	blob, err := svc.encryptCredentials(creds)
	if err != nil {
		t.Fatalf("encryptCredentials() error = %v", err)
	}
	return &OAuthClientConfig{OrganizationID: orgID, Provider: provider, EncryptedClient: blob}
}

func TestResolveOAuthClient_OrgOverridesDeploymentDefault(t *testing.T) {
	orgID := uuid.New()
	store := &fakeOrgOAuthStore{orgID: orgID, clients: map[string]*OAuthClientConfig{}}
	svc := newOrgClientService(store, config.SourceOAuthConfig{GoogleClientID: "deploy", GoogleClientSecret: "deploy-secret"})
	store.clients[orgClientKey(orgID, ProviderTypeGoogleDrive)] = mustEncryptOrgClient(t, svc, orgID, ProviderTypeGoogleDrive, Credentials{
		oauthClientIDKey: "org-id", oauthClientSecretKey: "org-secret",
	})

	got, err := svc.resolveOAuthClient(context.Background(), uuid.New(), ProviderTypeGoogleDrive)
	if err != nil {
		t.Fatalf("resolveOAuthClient() error = %v", err)
	}
	if got.ClientID != "org-id" || got.ClientSecret != "org-secret" {
		t.Fatalf("resolveOAuthClient() = %+v, want the org client", got)
	}
}

func TestResolveOAuthClient_FallsBackToDeploymentDefault(t *testing.T) {
	store := &fakeOrgOAuthStore{orgID: uuid.New(), clients: map[string]*OAuthClientConfig{}}
	svc := newOrgClientService(store, config.SourceOAuthConfig{GoogleClientID: "deploy", GoogleClientSecret: "deploy-secret"})

	got, err := svc.resolveOAuthClient(context.Background(), uuid.New(), ProviderTypeGoogleDrive)
	if err != nil {
		t.Fatalf("resolveOAuthClient() error = %v", err)
	}
	if got.ClientID != "deploy" || got.ClientSecret != "deploy-secret" {
		t.Fatalf("resolveOAuthClient() = %+v, want the deployment default", got)
	}
}

func TestResolveOAuthClient_NoClientIsActionableError(t *testing.T) {
	store := &fakeOrgOAuthStore{orgID: uuid.New(), clients: map[string]*OAuthClientConfig{}}
	svc := newOrgClientService(store, config.SourceOAuthConfig{})

	_, err := svc.resolveOAuthClient(context.Background(), uuid.New(), ProviderTypeGoogleDrive)
	if err == nil {
		t.Fatal("resolveOAuthClient() with no org and no deployment default = nil, want error")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("resolveOAuthClient() error type = %T, want *apperror.Error", err)
	}
	if appErr.Code != "validation_error" {
		t.Fatalf("resolveOAuthClient() error code = %q, want validation_error", appErr.Code)
	}
	// google_drive has a deployment default, so its error names the env vars.
	if !strings.Contains(appErr.Message, ProviderTypeGoogleDrive) {
		t.Errorf("error message %q must name the provider", appErr.Message)
	}
	if !strings.Contains(appErr.Message, "Organization settings") {
		t.Errorf("error message %q must direct to the org setting", appErr.Message)
	}
	if !strings.Contains(appErr.Message, "SOURCES_OAUTH_GOOGLE_CLIENT_ID/SECRET") {
		t.Errorf("error message %q must carry the env-var hint for a deployment-default provider", appErr.Message)
	}
}

func TestResolveOAuthClient_NoClient_NoDeploymentDefaultOmitsEnvVar(t *testing.T) {
	store := &fakeOrgOAuthStore{orgID: uuid.New(), clients: map[string]*OAuthClientConfig{}}
	svc := newOrgClientService(store, config.SourceOAuthConfig{})

	_, err := svc.resolveOAuthClient(context.Background(), uuid.New(), ProviderTypeGitHub)
	if err == nil {
		t.Fatal("resolveOAuthClient(github) with no org and no deployment default = nil, want error")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("resolveOAuthClient() error type = %T, want *apperror.Error", err)
	}
	if !strings.Contains(appErr.Message, ProviderTypeGitHub) {
		t.Errorf("error message %q must name the provider", appErr.Message)
	}
	if !strings.Contains(appErr.Message, "Organization settings") {
		t.Errorf("error message %q must direct to the org setting", appErr.Message)
	}
	// github has no deployment default: the Google env-var hint must be absent.
	if strings.Contains(appErr.Message, "SOURCES_OAUTH_GOOGLE_CLIENT_ID/SECRET") {
		t.Errorf("error message %q must NOT mention the Google env vars for a provider with no deployment default", appErr.Message)
	}
}

func TestListProviders_AdvertisesAuthMethods(t *testing.T) {
	svc := newTestService(nil)
	providers := svc.ListProviders(context.Background())
	p, ok := findProvider(providers, ProviderTypeGoogleDrive)
	if !ok {
		t.Fatalf("ListProviders() = %v, want a %q entry", providers, ProviderTypeGoogleDrive)
	}
	got := p.AuthMethods
	want := []string{string(AuthMethodOAuth)}
	if len(got) != len(want) {
		t.Fatalf("AuthMethods = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("AuthMethods[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestListOrgOAuthClients_RequiresOrgAdmin(t *testing.T) {
	svc := newOrgClientService(&fakeOrgOAuthStore{admin: false}, config.SourceOAuthConfig{})
	_, err := svc.ListOrgOAuthClients(context.Background(), uuid.New(), "user-1")
	if !errors.Is(err, apperror.ErrForbidden) {
		t.Fatalf("non-admin ListOrgOAuthClients() error = %v, want forbidden", err)
	}
}

func TestListOrgOAuthClients_RequiresUser(t *testing.T) {
	svc := newOrgClientService(&fakeOrgOAuthStore{admin: true}, config.SourceOAuthConfig{})
	_, err := svc.ListOrgOAuthClients(context.Background(), uuid.New(), "")
	if !errors.Is(err, apperror.ErrUnauthorized) {
		t.Fatalf("empty-user ListOrgOAuthClients() error = %v, want unauthorized", err)
	}
}

func TestUpsertOrgOAuthClient_EncryptsSecretAndRedacts(t *testing.T) {
	orgID := uuid.New()
	store := &fakeOrgOAuthStore{admin: true, clients: map[string]*OAuthClientConfig{}}
	svc := newOrgClientService(store, config.SourceOAuthConfig{})

	dto, err := svc.UpsertOrgOAuthClient(context.Background(), orgID, "user-1", ProviderTypeGoogleDrive, &SetOAuthClientRequest{
		ClientID:     "cid",
		ClientSecret: "top-secret",
		Scopes:       []string{"https://www.googleapis.com/auth/drive.file"},
		Strategy:     "drive.file",
	})
	if err != nil {
		t.Fatalf("UpsertOrgOAuthClient() error = %v", err)
	}
	if !dto.Configured || dto.ClientID != "cid" {
		t.Fatalf("DTO = %+v, want configured with client id cid", dto)
	}
	if dto.Strategy != "drive.file" {
		t.Errorf("DTO.Strategy = %q, want drive.file", dto.Strategy)
	}

	// The stored blob must decrypt to the secret.
	stored := store.clients[orgClientKey(orgID, ProviderTypeGoogleDrive)]
	creds, err := svc.decryptCredentials(stored.EncryptedClient)
	if err != nil {
		t.Fatalf("decryptCredentials(stored) error = %v", err)
	}
	if creds[oauthClientSecretKey] != "top-secret" {
		t.Errorf("stored secret = %v, want top-secret", creds[oauthClientSecretKey])
	}

	// The DTO and the store model must never serialize the secret.
	b, _ := json.Marshal(dto)
	if strings.Contains(string(b), "top-secret") || strings.Contains(string(b), "clientSecret") {
		t.Errorf("DTO JSON leaks secret material: %s", b)
	}
	rowB, _ := json.Marshal(stored)
	if strings.Contains(string(rowB), "encryptedClient") || strings.Contains(string(rowB), "top-secret") {
		t.Errorf("store row JSON leaks secret material: %s", rowB)
	}
}

func TestUpsertOrgOAuthClient_PreservesSecretOnUpdate(t *testing.T) {
	orgID := uuid.New()
	store := &fakeOrgOAuthStore{admin: true, clients: map[string]*OAuthClientConfig{}}
	svc := newOrgClientService(store, config.SourceOAuthConfig{})
	store.clients[orgClientKey(orgID, ProviderTypeGoogleDrive)] = mustEncryptOrgClient(t, svc, orgID, ProviderTypeGoogleDrive, Credentials{
		oauthClientIDKey: "cid", oauthClientSecretKey: "kept-secret",
	})

	if _, err := svc.UpsertOrgOAuthClient(context.Background(), orgID, "user-1", ProviderTypeGoogleDrive, &SetOAuthClientRequest{
		ClientID: "cid-2",
	}); err != nil {
		t.Fatalf("UpsertOrgOAuthClient() error = %v", err)
	}
	creds, err := svc.decryptCredentials(store.clients[orgClientKey(orgID, ProviderTypeGoogleDrive)].EncryptedClient)
	if err != nil {
		t.Fatalf("decryptCredentials() error = %v", err)
	}
	if creds[oauthClientIDKey] != "cid-2" {
		t.Errorf("client id after update = %v, want cid-2", creds[oauthClientIDKey])
	}
	if creds[oauthClientSecretKey] != "kept-secret" {
		t.Errorf("secret after update = %v, want kept-secret (write-only preserved)", creds[oauthClientSecretKey])
	}
}

func TestUpsertOrgOAuthClient_Validations(t *testing.T) {
	orgID := uuid.New()
	svc := newOrgClientService(&fakeOrgOAuthStore{admin: true, clients: map[string]*OAuthClientConfig{}}, config.SourceOAuthConfig{})

	if _, err := svc.UpsertOrgOAuthClient(context.Background(), orgID, "user-1", ProviderTypeGoogleDrive, &SetOAuthClientRequest{ClientSecret: "s"}); err == nil {
		t.Error("missing clientId = nil error, want validation")
	}
	if _, err := svc.UpsertOrgOAuthClient(context.Background(), orgID, "user-1", ProviderTypeGoogleDrive, &SetOAuthClientRequest{ClientID: "c"}); err == nil {
		t.Error("missing clientSecret on create = nil error, want validation")
	}
	if _, err := svc.UpsertOrgOAuthClient(context.Background(), orgID, "user-1", "imap", &SetOAuthClientRequest{ClientID: "c", ClientSecret: "s"}); err == nil {
		t.Error("unknown provider = nil error, want validation")
	}
}

func TestGetOrgOAuthClient_UnconfiguredReturnsNotConfigured(t *testing.T) {
	svc := newOrgClientService(&fakeOrgOAuthStore{admin: true, clients: map[string]*OAuthClientConfig{}}, config.SourceOAuthConfig{})
	dto, err := svc.GetOrgOAuthClient(context.Background(), uuid.New(), "user-1", ProviderTypeGoogleDrive)
	if err != nil {
		t.Fatalf("GetOrgOAuthClient() unconfigured error = %v", err)
	}
	if dto.Configured {
		t.Fatalf("unconfigured GetOrgOAuthClient() = %+v, want Configured=false", dto)
	}
}

func TestDeleteOrgOAuthClient(t *testing.T) {
	orgID := uuid.New()
	store := &fakeOrgOAuthStore{admin: true, clients: map[string]*OAuthClientConfig{}}
	svc := newOrgClientService(store, config.SourceOAuthConfig{})
	store.clients[orgClientKey(orgID, ProviderTypeGoogleDrive)] = mustEncryptOrgClient(t, svc, orgID, ProviderTypeGoogleDrive, Credentials{
		oauthClientIDKey: "cid", oauthClientSecretKey: "s",
	})

	if err := svc.DeleteOrgOAuthClient(context.Background(), orgID, "user-1", ProviderTypeGoogleDrive); err != nil {
		t.Fatalf("DeleteOrgOAuthClient() error = %v", err)
	}
	if err := svc.DeleteOrgOAuthClient(context.Background(), orgID, "user-1", ProviderTypeGoogleDrive); !errors.Is(err, apperror.ErrNotFound) {
		t.Fatalf("second DeleteOrgOAuthClient() error = %v, want not-found", err)
	}
}

// --- §6.1 scope strategy ---

func TestGoogleScopesForStrategy(t *testing.T) {
	s := newGoogleOAuthStrategy()
	cases := []struct {
		strategy string
		want     string
		ok       bool
	}{
		{"", googleDriveFile, true},
		{"drive.file", googleDriveFile, true},
		{"drive.readonly", googleDriveReadonly, true},
		{"bogus", "", false},
	}
	for _, tc := range cases {
		scopes, ok := s.ScopesForStrategy(tc.strategy)
		if ok != tc.ok {
			t.Errorf("ScopesForStrategy(%q) ok = %v, want %v", tc.strategy, ok, tc.ok)
			continue
		}
		if ok && (len(scopes) != 1 || scopes[0] != tc.want) {
			t.Errorf("ScopesForStrategy(%q) = %v, want [%s]", tc.strategy, scopes, tc.want)
		}
	}
}

func TestResolveOAuthClient_AppliesOrgStrategy(t *testing.T) {
	orgID := uuid.New()
	store := &fakeOrgOAuthStore{orgID: orgID, clients: map[string]*OAuthClientConfig{}}
	svc := newOrgClientService(store, config.SourceOAuthConfig{})
	store.clients[orgClientKey(orgID, ProviderTypeGoogleDrive)] = mustEncryptOrgClient(t, svc, orgID, ProviderTypeGoogleDrive, Credentials{
		oauthClientIDKey: "cid", oauthClientSecretKey: "s",
	})
	store.clients[orgClientKey(orgID, ProviderTypeGoogleDrive)].Strategy = string(ScopeStrategyDriveReadonly)

	got, err := svc.resolveOAuthClient(context.Background(), uuid.New(), ProviderTypeGoogleDrive)
	if err != nil {
		t.Fatalf("resolveOAuthClient() error = %v", err)
	}
	if len(got.Scopes) != 1 || got.Scopes[0] != googleDriveReadonly {
		t.Fatalf("resolveOAuthClient() scopes = %v, want [%s]", got.Scopes, googleDriveReadonly)
	}

	// An explicit scope set wins over the strategy.
	row := store.clients[orgClientKey(orgID, ProviderTypeGoogleDrive)]
	row.Scopes = []string{"explicit-scope"}
	got, err = svc.resolveOAuthClient(context.Background(), uuid.New(), ProviderTypeGoogleDrive)
	if err != nil {
		t.Fatalf("resolveOAuthClient() error = %v", err)
	}
	if len(got.Scopes) != 1 || got.Scopes[0] != "explicit-scope" {
		t.Fatalf("explicit scopes = %v, want [explicit-scope]", got.Scopes)
	}
}

func TestListProviders_AdvertisesStrategies(t *testing.T) {
	svc := newTestService(nil)
	providers := svc.ListProviders(context.Background())
	p, ok := findProvider(providers, ProviderTypeGoogleDrive)
	if !ok {
		t.Fatalf("ListProviders() = %v, want a %q entry", providers, ProviderTypeGoogleDrive)
	}
	got := p.Strategies
	want := []string{string(ScopeStrategyDriveFile), string(ScopeStrategyDriveReadonly)}
	if len(got) != len(want) {
		t.Fatalf("Strategies = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Strategies[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestUpsertOrgOAuthClient_PersistsConfig(t *testing.T) {
	orgID := uuid.New()
	store := &fakeOrgOAuthStore{admin: true, clients: map[string]*OAuthClientConfig{}}
	svc := newOrgClientService(store, config.SourceOAuthConfig{})

	dto, err := svc.UpsertOrgOAuthClient(context.Background(), orgID, "user-1", ProviderTypeGoogleDrive, &SetOAuthClientRequest{
		ClientID:     "cid",
		ClientSecret: "s",
		Strategy:     string(ScopeStrategyDriveFile),
		Config:       map[string]any{"pickerDeveloperKey": "dev-key", "pickerAppId": "123"},
	})
	if err != nil {
		t.Fatalf("UpsertOrgOAuthClient() error = %v", err)
	}
	if dto.Config["pickerDeveloperKey"] != "dev-key" || dto.Config["pickerAppId"] != "123" {
		t.Fatalf("DTO.Config = %+v, want the picker extras", dto.Config)
	}

	// Round-trips through the store (a fresh read returns the same config).
	got, err := svc.GetOrgOAuthClient(context.Background(), orgID, "user-1", ProviderTypeGoogleDrive)
	if err != nil {
		t.Fatalf("GetOrgOAuthClient() error = %v", err)
	}
	if got.Config["pickerDeveloperKey"] != "dev-key" {
		t.Fatalf("persisted config = %+v, want the picker developer key", got.Config)
	}
}

// --- project-scoped client configs ---

// fakeProjectOAuthStore is an in-memory ProjectOAuthClientStore for unit tests.
type fakeProjectOAuthStore struct {
	admin      bool
	adminErr   error
	resolveErr error
	orgID      uuid.UUID
	clients    map[string]*OAuthClientConfig
}

func projectClientKey(projectID uuid.UUID, provider string) string {
	return projectID.String() + "|" + provider
}

func (f *fakeProjectOAuthStore) IsProjectAdmin(ctx context.Context, projectID, userID string) (bool, error) {
	return f.admin, f.adminErr
}

func (f *fakeProjectOAuthStore) ResolveOrgID(ctx context.Context, projectID uuid.UUID) (uuid.UUID, error) {
	if f.resolveErr != nil {
		return uuid.Nil, f.resolveErr
	}
	return f.orgID, nil
}

func (f *fakeProjectOAuthStore) GetProjectOAuthClientConfig(ctx context.Context, projectID uuid.UUID, provider string) (*OAuthClientConfig, error) {
	c, ok := f.clients[projectClientKey(projectID, provider)]
	if !ok {
		return nil, apperror.ErrNotFound
	}
	return c, nil
}

func (f *fakeProjectOAuthStore) ListProjectOAuthClientConfigs(ctx context.Context, projectID uuid.UUID) ([]*OAuthClientConfig, error) {
	out := []*OAuthClientConfig{}
	for k, c := range f.clients {
		if strings.HasPrefix(k, projectID.String()+"|") {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeProjectOAuthStore) UpsertProjectOAuthClientConfig(ctx context.Context, cfg *OAuthClientConfig) (*OAuthClientConfig, error) {
	if f.clients == nil {
		f.clients = map[string]*OAuthClientConfig{}
	}
	if cfg.ProjectID == nil {
		return nil, apperror.NewInternal("missing project_id", nil)
	}
	now := time.Now().UTC()
	cfg.CreatedAt, cfg.UpdatedAt = now, now
	f.clients[projectClientKey(*cfg.ProjectID, cfg.Provider)] = cfg
	return cfg, nil
}

func (f *fakeProjectOAuthStore) DeleteProjectOAuthClientConfig(ctx context.Context, projectID uuid.UUID, provider string) error {
	k := projectClientKey(projectID, provider)
	if _, ok := f.clients[k]; !ok {
		return apperror.ErrNotFound
	}
	delete(f.clients, k)
	return nil
}

func newProjectClientService(store ProjectOAuthClientStore, oauthCfg config.SourceOAuthConfig) *Service {
	cfg := &config.Config{SourceOAuth: oauthCfg}
	cfg.LLMProvider.EncryptionKey = hex.EncodeToString(testEncryptionKey())
	return NewService(nil, cfg, discardLog(), NewDefaultRegistry(), WithProjectOAuthClientStore(store))
}

func newOrgAndProjectClientService(orgStore OrgOAuthClientStore, projectStore ProjectOAuthClientStore, oauthCfg config.SourceOAuthConfig) *Service {
	cfg := &config.Config{SourceOAuth: oauthCfg}
	cfg.LLMProvider.EncryptionKey = hex.EncodeToString(testEncryptionKey())
	return NewService(nil, cfg, discardLog(), NewDefaultRegistry(),
		WithOrgOAuthClientStore(orgStore),
		WithProjectOAuthClientStore(projectStore))
}

func mustEncryptProjectClient(t *testing.T, svc *Service, projectID uuid.UUID, provider string, creds Credentials) *OAuthClientConfig {
	t.Helper()
	blob, err := svc.encryptCredentials(creds)
	if err != nil {
		t.Fatalf("encryptCredentials() error = %v", err)
	}
	return &OAuthClientConfig{ProjectID: &projectID, Provider: provider, EncryptedClient: blob}
}

func TestResolveOAuthClient_ProjectOverridesOrg(t *testing.T) {
	projectID := uuid.New()
	orgID := uuid.New()
	orgStore := &fakeOrgOAuthStore{orgID: orgID, clients: map[string]*OAuthClientConfig{}}
	projStore := &fakeProjectOAuthStore{orgID: orgID, clients: map[string]*OAuthClientConfig{}}
	svc := newOrgAndProjectClientService(orgStore, projStore, config.SourceOAuthConfig{GoogleClientID: "deploy", GoogleClientSecret: "deploy-secret"})

	orgStore.clients[orgClientKey(orgID, ProviderTypeGoogleDrive)] = mustEncryptOrgClient(t, svc, orgID, ProviderTypeGoogleDrive, Credentials{
		oauthClientIDKey: "org-id", oauthClientSecretKey: "org-secret",
	})
	projStore.clients[projectClientKey(projectID, ProviderTypeGoogleDrive)] = mustEncryptProjectClient(t, svc, projectID, ProviderTypeGoogleDrive, Credentials{
		oauthClientIDKey: "proj-id", oauthClientSecretKey: "proj-secret",
	})

	got, err := svc.resolveOAuthClient(context.Background(), projectID, ProviderTypeGoogleDrive)
	if err != nil {
		t.Fatalf("resolveOAuthClient() error = %v", err)
	}
	if got.ClientID != "proj-id" || got.ClientSecret != "proj-secret" {
		t.Fatalf("resolveOAuthClient() = %+v, want the project client", got)
	}
}

func TestResolveOAuthClient_ProjectMissingFallsBackToOrg(t *testing.T) {
	projectID := uuid.New()
	orgID := uuid.New()
	orgStore := &fakeOrgOAuthStore{orgID: orgID, clients: map[string]*OAuthClientConfig{}}
	projStore := &fakeProjectOAuthStore{orgID: orgID, clients: map[string]*OAuthClientConfig{}}
	svc := newOrgAndProjectClientService(orgStore, projStore, config.SourceOAuthConfig{GoogleClientID: "deploy", GoogleClientSecret: "deploy-secret"})

	orgStore.clients[orgClientKey(orgID, ProviderTypeGoogleDrive)] = mustEncryptOrgClient(t, svc, orgID, ProviderTypeGoogleDrive, Credentials{
		oauthClientIDKey: "org-id", oauthClientSecretKey: "org-secret",
	})

	got, err := svc.resolveOAuthClient(context.Background(), projectID, ProviderTypeGoogleDrive)
	if err != nil {
		t.Fatalf("resolveOAuthClient() error = %v", err)
	}
	if got.ClientID != "org-id" || got.ClientSecret != "org-secret" {
		t.Fatalf("resolveOAuthClient() = %+v, want the org client", got)
	}
}

func TestResolveOAuthClient_ProjectMissingAndOrgMissingFallsBackToEnv(t *testing.T) {
	projectID := uuid.New()
	orgStore := &fakeOrgOAuthStore{orgID: uuid.New(), clients: map[string]*OAuthClientConfig{}}
	projStore := &fakeProjectOAuthStore{orgID: uuid.New(), clients: map[string]*OAuthClientConfig{}}
	svc := newOrgAndProjectClientService(orgStore, projStore, config.SourceOAuthConfig{GoogleClientID: "deploy", GoogleClientSecret: "deploy-secret"})

	got, err := svc.resolveOAuthClient(context.Background(), projectID, ProviderTypeGoogleDrive)
	if err != nil {
		t.Fatalf("resolveOAuthClient() error = %v", err)
	}
	if got.ClientID != "deploy" || got.ClientSecret != "deploy-secret" {
		t.Fatalf("resolveOAuthClient() = %+v, want the deployment default", got)
	}
}

func TestResolveOAuthClient_ProjectStoreErrorPropagates(t *testing.T) {
	projectID := uuid.New()
	orgStore := &fakeOrgOAuthStore{orgID: uuid.New(), clients: map[string]*OAuthClientConfig{}}
	projStore := &fakeProjectOAuthStore{orgID: uuid.New(), clients: map[string]*OAuthClientConfig{}}
	svc := newOrgAndProjectClientService(orgStore, projStore, config.SourceOAuthConfig{})

	projStore.clients[projectClientKey(projectID, ProviderTypeGoogleDrive)] = &OAuthClientConfig{
		ProjectID:       &projectID,
		Provider:        ProviderTypeGoogleDrive,
		EncryptedClient: []byte("not-a-valid-blob"),
	}

	_, err := svc.resolveOAuthClient(context.Background(), projectID, ProviderTypeGoogleDrive)
	if err == nil {
		t.Fatal("resolveOAuthClient() with undecryptable project row = nil, want error")
	}
}

func TestListProjectOAuthClients_RequiresProjectAdmin(t *testing.T) {
	svc := newProjectClientService(&fakeProjectOAuthStore{admin: false}, config.SourceOAuthConfig{})
	_, err := svc.ListProjectOAuthClients(context.Background(), uuid.New(), "user-1")
	if !errors.Is(err, apperror.ErrForbidden) {
		t.Fatalf("non-admin ListProjectOAuthClients() error = %v, want forbidden", err)
	}
}

func TestListProjectOAuthClients_RequiresUser(t *testing.T) {
	svc := newProjectClientService(&fakeProjectOAuthStore{admin: true}, config.SourceOAuthConfig{})
	_, err := svc.ListProjectOAuthClients(context.Background(), uuid.New(), "")
	if !errors.Is(err, apperror.ErrUnauthorized) {
		t.Fatalf("empty-user ListProjectOAuthClients() error = %v, want unauthorized", err)
	}
}

func TestUpsertProjectOAuthClient_EncryptsSecretAndRedacts(t *testing.T) {
	projectID := uuid.New()
	orgID := uuid.New()
	store := &fakeProjectOAuthStore{admin: true, orgID: orgID, clients: map[string]*OAuthClientConfig{}}
	svc := newProjectClientService(store, config.SourceOAuthConfig{
		RedirectBaseURL: "https://api.example",
		LandingURL:      "https://ui.example/data-sources",
	})

	dto, err := svc.UpsertProjectOAuthClient(context.Background(), projectID, "user-1", ProviderTypeGoogleDrive, &SetOAuthClientRequest{
		ClientID:     "cid",
		ClientSecret: "top-secret",
		Scopes:       []string{"https://www.googleapis.com/auth/drive.file"},
		Strategy:     "drive.file",
	})
	if err != nil {
		t.Fatalf("UpsertProjectOAuthClient() error = %v", err)
	}
	if !dto.Configured || dto.ClientID != "cid" {
		t.Fatalf("DTO = %+v, want configured with client id cid", dto)
	}
	if dto.RedirectURI != "https://api.example/api/data-source-integrations/oauth/google_drive/callback" {
		t.Errorf("DTO.RedirectURI = %q", dto.RedirectURI)
	}
	if dto.JavascriptOrigin != "https://ui.example" {
		t.Errorf("DTO.JavascriptOrigin = %q, want https://ui.example", dto.JavascriptOrigin)
	}

	// The stored row carries the owning org and the project.
	stored := store.clients[projectClientKey(projectID, ProviderTypeGoogleDrive)]
	if stored.ProjectID == nil || *stored.ProjectID != projectID {
		t.Fatalf("stored ProjectID = %v, want %v", stored.ProjectID, projectID)
	}
	if stored.OrganizationID != orgID {
		t.Fatalf("stored OrganizationID = %v, want %v", stored.OrganizationID, orgID)
	}

	// The stored blob must decrypt to the secret.
	creds, err := svc.decryptCredentials(stored.EncryptedClient)
	if err != nil {
		t.Fatalf("decryptCredentials(stored) error = %v", err)
	}
	if creds[oauthClientSecretKey] != "top-secret" {
		t.Errorf("stored secret = %v, want top-secret", creds[oauthClientSecretKey])
	}

	// The DTO must never serialize the secret.
	b, _ := json.Marshal(dto)
	if strings.Contains(string(b), "top-secret") || strings.Contains(string(b), "clientSecret") {
		t.Errorf("DTO JSON leaks secret material: %s", b)
	}
}

func TestDeleteProjectOAuthClient(t *testing.T) {
	projectID := uuid.New()
	orgID := uuid.New()
	store := &fakeProjectOAuthStore{admin: true, orgID: orgID, clients: map[string]*OAuthClientConfig{}}
	svc := newProjectClientService(store, config.SourceOAuthConfig{})
	store.clients[projectClientKey(projectID, ProviderTypeGoogleDrive)] = mustEncryptProjectClient(t, svc, projectID, ProviderTypeGoogleDrive, Credentials{
		oauthClientIDKey: "cid", oauthClientSecretKey: "s",
	})

	if err := svc.DeleteProjectOAuthClient(context.Background(), projectID, "user-1", ProviderTypeGoogleDrive); err != nil {
		t.Fatalf("DeleteProjectOAuthClient() error = %v", err)
	}
	if err := svc.DeleteProjectOAuthClient(context.Background(), projectID, "user-1", ProviderTypeGoogleDrive); !errors.Is(err, apperror.ErrNotFound) {
		t.Fatalf("second DeleteProjectOAuthClient() error = %v, want not-found", err)
	}
}

func TestGetProjectOAuthClient_UnconfiguredReturnsNotConfigured(t *testing.T) {
	svc := newProjectClientService(&fakeProjectOAuthStore{admin: true, clients: map[string]*OAuthClientConfig{}}, config.SourceOAuthConfig{})
	dto, err := svc.GetProjectOAuthClient(context.Background(), uuid.New(), "user-1", ProviderTypeGoogleDrive)
	if err != nil {
		t.Fatalf("GetProjectOAuthClient() unconfigured error = %v", err)
	}
	if dto.Configured {
		t.Fatalf("unconfigured GetProjectOAuthClient() = %+v, want Configured=false", dto)
	}
}

func TestGoogleDriveProvider_OAuthOnly(t *testing.T) {
	p := newGoogleDriveProvider()
	if methods := p.AuthMethods(); len(methods) != 1 || methods[0] != AuthMethodOAuth {
		t.Fatalf("AuthMethods() = %v, want [oauth]", methods)
	}
	if fields := p.CredentialFields(); len(fields) != 0 {
		t.Fatalf("CredentialFields() = %v, want empty (OAuth-only)", fields)
	}
}
