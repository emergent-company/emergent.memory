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
}

func TestListProviders_AdvertisesAuthMethods(t *testing.T) {
	svc := newTestService(nil)
	providers := svc.ListProviders(context.Background())
	p, ok := findProvider(providers, ProviderTypeGoogleDrive)
	if !ok {
		t.Fatalf("ListProviders() = %v, want a %q entry", providers, ProviderTypeGoogleDrive)
	}
	got := p.AuthMethods
	want := []string{string(AuthMethodOAuth), string(AuthMethodCredentials)}
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
