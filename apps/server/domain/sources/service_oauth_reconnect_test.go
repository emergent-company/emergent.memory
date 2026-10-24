package sources

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// newReconnectTestService builds a DB-backed Service with deployment OAuth
// clients for Google Drive and GitHub pointing at tokenURL, and a redirect base
// so StartOAuth can mint a flow. Two providers with a client let a test ask for
// a reconnect of the wrong provider without failing on client resolution first.
func newReconnectTestService(t *testing.T, db *testdb.TestDB, tokenURL string, httpClient *http.Client) *Service {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	cfg := &config.Config{
		LLMProvider: config.LLMProviderConfig{EncryptionKey: hex.EncodeToString(key)},
		SourceOAuth: config.SourceOAuthConfig{RedirectBaseURL: "https://api.example"},
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewService(NewRepository(db.DB, log), cfg, log, NewDefaultRegistry(),
		WithOAuthClients(map[string]OAuthClient{
			ProviderTypeGoogleDrive: {ClientID: "cid", ClientSecret: "cs", TokenURL: tokenURL},
			ProviderTypeGitHub:      {ClientID: "gh-cid", ClientSecret: "gh-cs", TokenURL: tokenURL},
		}),
		WithOAuthHTTPClient(httpClient),
	)
}

// authorizeState pulls the opaque state out of a start response's authorize URL.
func authorizeState(t *testing.T, authorizeURL string) string {
	t.Helper()
	u, err := url.Parse(authorizeURL)
	if err != nil {
		t.Fatalf("parse authorize URL: %v", err)
	}
	state := u.Query().Get("state")
	if state == "" {
		t.Fatal("authorize URL carries no state")
	}
	return state
}

// TestCompleteOAuth_ReconnectReplacesGrantInPlace asserts a reconnect repairs
// the existing source instead of creating a second one (#2107): the same id
// comes back, the source's own config and history survive, the granted strategy
// and credential blob are replaced, and the recorded dead-grant error is cleared.
func TestCompleteOAuth_ReconnectReplacesGrantInPlace(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "sourcesreconnect")
	t.Cleanup(tdb.Close)

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"access_token":"at-new","refresh_token":"rt-new","expires_in":3599,"scope":%q,"token_type":"Bearer"}`, googleDriveReadonly)
	}))
	defer tokenSrv.Close()

	svc := newReconnectTestService(t, tdb, tokenSrv.URL, tokenSrv.Client())
	projectID := uuid.New()

	created, err := svc.Create(ctx, projectID, &CreateIntegrationRequest{
		Name:         "Team drive",
		ProviderType: ProviderTypeGoogleDrive,
		Config: map[string]any{
			"folder_id":     "folder-a",
			"mime_types":    []string{"application/pdf"},
			"recursive":     true,
			"refresh_token": "rt-old",
			"client_id":     "old-client",
		},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	srcID := uuid.MustParse(created.ID)
	if err := svc.repo.RecordSourceError(ctx, projectID, srcID, "the Google Drive connection for this source has expired or was revoked; reconnect the source to continue"); err != nil {
		t.Fatalf("RecordSourceError() error = %v", err)
	}

	// The reconnect start carries the source id plus an ignored config: a
	// reconnect repairs credentials, it must never re-enter the source's name or
	// sync scope from whatever the browser happens to post.
	start, err := svc.StartOAuth(ctx, projectID, ProviderTypeGoogleDrive, StartOAuthRequest{
		ReconnectSourceID: &srcID,
		Config:            map[string]any{"name": "Hijacked", "folder_id": "folder-b"},
	})
	if err != nil {
		t.Fatalf("StartOAuth(reconnect) error = %v", err)
	}
	state := authorizeState(t, start.AuthorizeURL)

	got, err := svc.CompleteOAuth(ctx, ProviderTypeGoogleDrive, state, "code-1")
	if err != nil {
		t.Fatalf("CompleteOAuth(reconnect) error = %v", err)
	}

	if got.ID != created.ID {
		t.Errorf("reconnect returned source %q, want the existing %q", got.ID, created.ID)
	}
	all, err := svc.List(ctx, projectID)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("reconnect left %d sources, want 1 (no duplicate)", len(all))
	}

	if got.Name != "Team drive" {
		t.Errorf("name = %q, want the existing %q (reconnect does not rename)", got.Name, "Team drive")
	}
	// The source's own config survives untouched. Create normalizes the legacy
	// folder_id shape into `folders`, so compare against what the source actually
	// stored; JSON gives one canonical shape for []string vs the []any a database
	// read returns.
	gotCfg := make(map[string]any, len(got.Config))
	for k, v := range got.Config {
		if k == oauthGrantedStrategyKey {
			continue // recorded by the new grant
		}
		gotCfg[k] = v
	}
	gotJSON, _ := json.Marshal(gotCfg)
	wantJSON, _ := json.Marshal(created.Config)
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("config after reconnect = %s, want the existing %s", gotJSON, wantJSON)
	}
	if got.Config[oauthGrantedStrategyKey] != string(ScopeStrategyDriveReadonly) {
		t.Errorf("config[%q] = %v, want %q from the new token response", oauthGrantedStrategyKey, got.Config[oauthGrantedStrategyKey], ScopeStrategyDriveReadonly)
	}
	if _, present := got.Config[oauthReconnectKey]; present {
		t.Error("reconnect id leaked into the source config")
	}

	row, err := svc.repo.GetByIDWithCredentials(ctx, srcID, projectID)
	if err != nil {
		t.Fatalf("GetByIDWithCredentials() error = %v", err)
	}
	if row.LastError != nil {
		t.Errorf("last_error = %q, want cleared after a successful reconnect", *row.LastError)
	}
	if row.ErrorCount != 0 {
		t.Errorf("error_count = %d, want 0 after a successful reconnect", row.ErrorCount)
	}
	creds, err := svc.decryptCredentials(row.EncryptedCredentials)
	if err != nil {
		t.Fatalf("decryptCredentials() error = %v", err)
	}
	if creds["refresh_token"] != "rt-new" {
		t.Errorf("stored refresh_token = %q, want rt-new", creds["refresh_token"])
	}
	if creds["access_token"] != "at-new" {
		t.Errorf("stored access_token = %q, want at-new", creds["access_token"])
	}
	if creds["client_id"] != "cid" {
		t.Errorf("stored client_id = %q, want the deployed client cid", creds["client_id"])
	}
	if creds["client_secret"] != "cs" {
		t.Errorf("stored client_secret = %q, want the deployed secret", creds["client_secret"])
	}
}

// TestStartOAuth_ReconnectValidatesTheTarget pins the two validations that keep a
// browser-supplied id from starting a flow: the source must be in the caller's
// project, and it must be a source of the provider being reconnected.
func TestStartOAuth_ReconnectValidatesTheTarget(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "sourcesreconnectvalidate")
	t.Cleanup(tdb.Close)

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"at","refresh_token":"rt","expires_in":3599,"token_type":"Bearer"}`)
	}))
	defer tokenSrv.Close()

	svc := newReconnectTestService(t, tdb, tokenSrv.URL, tokenSrv.Client())
	ownerProject := uuid.New()
	otherProject := uuid.New()

	created, err := svc.Create(ctx, ownerProject, &CreateIntegrationRequest{
		Name:         "Team drive",
		ProviderType: ProviderTypeGoogleDrive,
		Config:       map[string]any{"folder_id": "folder-a", "refresh_token": "rt"},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	srcID := uuid.MustParse(created.ID)

	t.Run("cross-project id is not found", func(t *testing.T) {
		_, err := svc.StartOAuth(ctx, otherProject, ProviderTypeGoogleDrive, StartOAuthRequest{ReconnectSourceID: &srcID})
		if !errors.Is(err, apperror.ErrNotFound) {
			t.Fatalf("StartOAuth(cross-project reconnect) error = %v, want not found", err)
		}
	})

	t.Run("provider mismatch is rejected", func(t *testing.T) {
		_, err := svc.StartOAuth(ctx, ownerProject, ProviderTypeGitHub, StartOAuthRequest{ReconnectSourceID: &srcID})
		if err == nil {
			t.Fatal("StartOAuth(github reconnect of a drive source) succeeded, want a validation error")
		}
		var appErr *apperror.Error
		if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusUnprocessableEntity {
			t.Fatalf("StartOAuth(provider mismatch) error = %v, want a validation error (422)", err)
		}
	})

	t.Run("a zero id is not a reconnect", func(t *testing.T) {
		var zero uuid.UUID
		// No source exists for the nil id, so a reconnect attempt would be not
		// found; the zero id must instead mint an ordinary connect flow.
		resp, err := svc.StartOAuth(ctx, ownerProject, ProviderTypeGoogleDrive, StartOAuthRequest{ReconnectSourceID: &zero})
		if err != nil {
			t.Fatalf("StartOAuth(zero reconnect id) error = %v", err)
		}
		if resp.AuthorizeURL == "" {
			t.Error("StartOAuth(zero reconnect id) returned no authorize URL")
		}
	})
}

// TestCompleteOAuth_ConnectIgnoresReservedFlowKeys asserts a connect request
// cannot steer the flow: a client-supplied reconnect (or dynamic-client) key
// must neither target an existing source nor be persisted into the new source's
// config. Only StartOAuth may set those keys (#2107 review).
func TestCompleteOAuth_ConnectIgnoresReservedFlowKeys(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "sourcesreservedkeys")
	t.Cleanup(tdb.Close)

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"access_token":"at","refresh_token":"rt","expires_in":3599,"scope":%q,"token_type":"Bearer"}`, googleDriveReadonly)
	}))
	defer tokenSrv.Close()

	for _, tc := range []struct {
		name   string
		inject string
	}{
		{"reconnect key naming a real source", "victim"},
		{"empty reconnect key", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := newReconnectTestService(t, tdb, tokenSrv.URL, tokenSrv.Client())
			projectID := uuid.New()

			victim, err := svc.Create(ctx, projectID, &CreateIntegrationRequest{
				Name:         "Victim",
				ProviderType: ProviderTypeGoogleDrive,
				Config:       map[string]any{"folder_id": "folder-a", "refresh_token": "rt-old"},
			})
			if err != nil {
				t.Fatalf("Create(victim) error = %v", err)
			}
			value := ""
			if tc.inject == "victim" {
				value = victim.ID
			}

			start, err := svc.StartOAuth(ctx, projectID, ProviderTypeGoogleDrive, StartOAuthRequest{
				Config: map[string]any{
					oauthReconnectKey: value,
					oauthDynamicKey:   "spoofed",
					"name":            "Fresh source",
				},
			})
			if err != nil {
				t.Fatalf("StartOAuth(connect) error = %v", err)
			}
			got, err := svc.CompleteOAuth(ctx, ProviderTypeGoogleDrive, authorizeState(t, start.AuthorizeURL), "code-1")
			if err != nil {
				t.Fatalf("CompleteOAuth(connect) error = %v", err)
			}

			if got.ID == victim.ID {
				t.Fatal("a connect request with a reconnect key replaced the existing source, want a new one")
			}
			if got.Name != "Fresh source" {
				t.Errorf("name = %q, want the connect flow's own name", got.Name)
			}
			for _, k := range []string{oauthReconnectKey, oauthDynamicKey} {
				if _, present := got.Config[k]; present {
					t.Errorf("config carries the reserved key %q: %+v", k, got.Config)
				}
			}
			all, err := svc.List(ctx, projectID)
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}
			if len(all) != 2 {
				t.Fatalf("sources = %d, want the victim plus the new source", len(all))
			}

			// The victim keeps its own grant.
			row, err := svc.repo.GetByIDWithCredentials(ctx, uuid.MustParse(victim.ID), projectID)
			if err != nil {
				t.Fatalf("GetByIDWithCredentials(victim) error = %v", err)
			}
			creds, err := svc.decryptCredentials(row.EncryptedCredentials)
			if err != nil {
				t.Fatalf("decryptCredentials() error = %v", err)
			}
			if creds["refresh_token"] != "rt-old" {
				t.Errorf("victim refresh_token = %q, want its own rt-old", creds["refresh_token"])
			}
		})
	}
}
