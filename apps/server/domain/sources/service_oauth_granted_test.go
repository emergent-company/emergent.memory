package sources

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// newGrantedTestService builds a DB-backed Service whose Google Drive deployment
// client points at the given token server, so CompleteOAuth exchanges against a
// controllable token response without reaching Google.
func newGrantedTestService(t *testing.T, db *testdb.TestDB, tokenURL string, httpClient *http.Client) *Service {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	cfg := &config.Config{LLMProvider: config.LLMProviderConfig{EncryptionKey: hex.EncodeToString(key)}}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := NewRepository(db.DB, log)
	return NewService(repo, cfg, log, NewDefaultRegistry(),
		WithOAuthClients(map[string]OAuthClient{
			ProviderTypeGoogleDrive: {ClientID: "cid", ClientSecret: "cs", TokenURL: tokenURL},
		}),
		WithOAuthHTTPClient(httpClient),
	)
}

// TestCompleteOAuth_PersistsGrantedStrategy asserts CompleteOAuth resolves the
// token response's granted scope to its strategy and persists it under
// oauth_granted_strategy — and persists nothing for an unrecognised scope.
func TestCompleteOAuth_PersistsGrantedStrategy(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "sourcesgranted")
	t.Cleanup(tdb.Close)

	cases := []struct {
		name  string
		scope string
		want  string
	}{
		{"drive.readonly", googleDriveReadonly, string(ScopeStrategyDriveReadonly)},
		{"drive.file", googleDriveFile, string(ScopeStrategyDriveFile)},
		{"unrecognised scope", "https://www.googleapis.com/auth/drive.metadata.readonly", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"access_token":"at","refresh_token":"rt","expires_in":3599,"scope":%q,"token_type":"Bearer"}`, tc.scope)
			}))
			defer tokenSrv.Close()

			svc := newGrantedTestService(t, tdb, tokenSrv.URL, tokenSrv.Client())

			projectID := uuid.New()
			state := uuid.NewString()
			if err := svc.repo.CreateOAuthPendingState(ctx, &OAuthPendingState{
				State:        state,
				ProjectID:    projectID,
				Provider:     ProviderTypeGoogleDrive,
				CodeVerifier: "verifier-1",
				RedirectURI:  "https://app.example/callback",
				Config:       map[string]any{},
				Scopes:       []string{googleDriveReadonly},
				ExpiresAt:    time.Now().Add(time.Hour),
			}); err != nil {
				t.Fatalf("CreateOAuthPendingState() error = %v", err)
			}

			completion, err := svc.CompleteOAuth(ctx, ProviderTypeGoogleDrive, state, "code-1")
			if err != nil {
				t.Fatalf("CompleteOAuth() error = %v", err)
			}
			got := completion.Integration
			granted, present := got.Config[oauthGrantedStrategyKey].(string)
			switch tc.want {
			case "":
				if present {
					t.Errorf("config[%q] = %q, want absent for an unrecognised scope", oauthGrantedStrategyKey, granted)
				}
			default:
				if !present || granted != tc.want {
					t.Errorf("config[%q] = %q (present=%v), want %q", oauthGrantedStrategyKey, granted, present, tc.want)
				}
			}
		})
	}
}

// TestGet_PublishesGrantedStrategy asserts the single-source read surfaces the
// persisted granted strategy, and leaves it empty for a source without the key.
func TestGet_PublishesGrantedStrategy(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "sourcesgetgranted")
	t.Cleanup(tdb.Close)

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	cfg := &config.Config{LLMProvider: config.LLMProviderConfig{EncryptionKey: hex.EncodeToString(key)}}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := NewService(NewRepository(tdb.DB, log), cfg, log, NewDefaultRegistry())

	projectID := uuid.New()

	withKey, err := svc.Create(ctx, projectID, &CreateIntegrationRequest{
		Name:         "Drive (drive.file)",
		ProviderType: ProviderTypeGoogleDrive,
		Config: map[string]any{
			"folder_id":             "folder-a",
			"refresh_token":         "rt",
			oauthGrantedStrategyKey: "drive.file",
		},
	})
	if err != nil {
		t.Fatalf("Create(with key) error = %v", err)
	}

	withoutKey, err := svc.Create(ctx, projectID, &CreateIntegrationRequest{
		Name:         "Drive (default)",
		ProviderType: ProviderTypeGoogleDrive,
		Config:       map[string]any{"folder_id": "folder-b", "refresh_token": "rt"},
	})
	if err != nil {
		t.Fatalf("Create(without key) error = %v", err)
	}

	got, err := svc.Get(ctx, projectID, uuid.MustParse(withKey.ID))
	if err != nil {
		t.Fatalf("Get(with key) error = %v", err)
	}
	if got.OAuthGrantedStrategy != "drive.file" {
		t.Errorf("Get(with key).OAuthGrantedStrategy = %q, want %q", got.OAuthGrantedStrategy, "drive.file")
	}

	got, err = svc.Get(ctx, projectID, uuid.MustParse(withoutKey.ID))
	if err != nil {
		t.Fatalf("Get(without key) error = %v", err)
	}
	if got.OAuthGrantedStrategy != "" {
		t.Errorf("Get(without key).OAuthGrantedStrategy = %q, want empty", got.OAuthGrantedStrategy)
	}
}

// TestUpdate_PreservesGrantedStrategy asserts the config-replacing Update path
// keeps the recorded granted strategy when the submitted config (the edit form)
// omits the key, and lets an explicit submitted value win.
func TestUpdate_PreservesGrantedStrategy(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "sourcesupdategranted")
	t.Cleanup(tdb.Close)

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	cfg := &config.Config{LLMProvider: config.LLMProviderConfig{EncryptionKey: hex.EncodeToString(key)}}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := NewService(NewRepository(tdb.DB, log), cfg, log, NewDefaultRegistry())

	projectID := uuid.New()
	created, err := svc.Create(ctx, projectID, &CreateIntegrationRequest{
		Name:         "Drive",
		ProviderType: ProviderTypeGoogleDrive,
		Config: map[string]any{
			"folder_id":             "folder-a",
			"refresh_token":         "rt",
			oauthGrantedStrategyKey: "drive.file",
		},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	id := uuid.MustParse(created.ID)

	// Edit that omits the key but changes a declared field: the granted
	// strategy must survive the config replace.
	if _, err := svc.Update(ctx, projectID, id, &UpdateIntegrationRequest{
		Config: map[string]any{"folder_id": "folder-a", "recursive": true},
	}); err != nil {
		t.Fatalf("Update(omit key) error = %v", err)
	}
	got, err := svc.Get(ctx, projectID, id)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.OAuthGrantedStrategy != "drive.file" {
		t.Errorf("Update(omit key).OAuthGrantedStrategy = %q, want %q", got.OAuthGrantedStrategy, "drive.file")
	}
	if g, ok := got.Config[oauthGrantedStrategyKey].(string); !ok || g != "drive.file" {
		t.Errorf("Update(omit key).config[%q] = %v, want %q preserved", oauthGrantedStrategyKey, got.Config[oauthGrantedStrategyKey], "drive.file")
	}
	if got.Config["recursive"] != true {
		t.Errorf("Update(omit key).config[recursive] = %v, want true", got.Config["recursive"])
	}

	// An explicit submitted value wins over the stored one.
	if _, err := svc.Update(ctx, projectID, id, &UpdateIntegrationRequest{
		Config: map[string]any{"folder_id": "folder-a", oauthGrantedStrategyKey: "drive.readonly"},
	}); err != nil {
		t.Fatalf("Update(explicit key) error = %v", err)
	}
	got, err = svc.Get(ctx, projectID, id)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.OAuthGrantedStrategy != "drive.readonly" {
		t.Errorf("Update(explicit key).OAuthGrantedStrategy = %q, want %q", got.OAuthGrantedStrategy, "drive.readonly")
	}
}
