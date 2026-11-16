package account

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdkauth "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/auth"
)

func hostOf(t *testing.T, serverURL string) string {
	t.Helper()
	u, err := url.Parse(serverURL)
	if err != nil {
		t.Fatalf("parse %s: %v", serverURL, err)
	}
	return u.Host
}

// fakeDeps returns deterministic, network-free Deps. PollToken/RefreshToken
// emulate the SDK persisting credentials to credsPath so file-level assertions
// stay meaningful.
func fakeDeps(t *testing.T) Deps {
	t.Helper()
	return Deps{
		DiscoverIssuer: func(context.Context, string) (string, error) { return "https://issuer.test", nil },
		DiscoverOIDC: func(issuer string) (*sdkauth.OIDCConfig, error) {
			return &sdkauth.OIDCConfig{
				Issuer:                      issuer,
				DeviceAuthorizationEndpoint: "https://issuer.test/device",
				TokenEndpoint:               "https://issuer.test/token",
				UserinfoEndpoint:            "https://issuer.test/userinfo",
			}, nil
		},
		RequestDeviceCode: func(context.Context, *sdkauth.OIDCConfig, string, []string) (*DeviceCode, error) {
			return &DeviceCode{
				DeviceCode:              "device-1",
				UserCode:                "WXYZ-1234",
				VerificationURI:         "https://verify.test",
				VerificationURIComplete: "https://verify.test?code=WXYZ-1234",
				ExpiresIn:               300,
				Interval:                1,
			}, nil
		},
		PollToken: func(_ context.Context, _ *sdkauth.OIDCConfig, _, credsPath, _ string, _, _ int) (*sdkauth.Credentials, error) {
			creds := &sdkauth.Credentials{AccessToken: "access-1", RefreshToken: "refresh-1", ExpiresAt: time.Now().Add(time.Hour).UTC()}
			if err := sdkauth.SaveCredentials(creds, credsPath); err != nil {
				return nil, err
			}
			return creds, nil
		},
		RefreshToken: func(_ context.Context, _ *sdkauth.OIDCConfig, _, credsPath string) (*sdkauth.Credentials, error) {
			creds := &sdkauth.Credentials{AccessToken: "access-2", RefreshToken: "refresh-2", ExpiresAt: time.Now().Add(2 * time.Hour).UTC()}
			if err := sdkauth.SaveCredentials(creds, credsPath); err != nil {
				return nil, err
			}
			return creds, nil
		},
		FetchIdentity: func(_ context.Context, serverURL, _ string) (Identity, error) {
			return Identity{UserID: "user-1", Email: "user@" + hostOf(t, serverURL), Type: "session"}, nil
		},
	}
}

func TestDefaultRequestDeviceCodeIncludesOfflineAccess(t *testing.T) {
	var form url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		form = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"device_code":"d","user_code":"u","verification_uri":"https://v","expires_in":60,"interval":5}`))
	}))
	defer srv.Close()

	oidc := &sdkauth.OIDCConfig{DeviceAuthorizationEndpoint: srv.URL}
	device, err := DefaultDeps().RequestDeviceCode(context.Background(), oidc, ClientID, Scopes)
	if err != nil {
		t.Fatalf("RequestDeviceCode: %v", err)
	}
	if device.DeviceCode != "d" || device.UserCode != "u" {
		t.Errorf("device = %+v", device)
	}
	if got := form.Get("client_id"); got != ClientID {
		t.Errorf("client_id = %q, want %q", got, ClientID)
	}
	if scope := form.Get("scope"); !strings.Contains(scope, "offline_access") {
		t.Errorf("scope = %q, want it to include offline_access", scope)
	}
}

func TestDefaultDiscoverIssuerAndFetchIdentity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/issuer":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"issuer":"https://issuer.test","standalone":false}`))
		case "/api/auth/me":
			if got := r.Header.Get("Authorization"); got != "Bearer token-1" {
				t.Errorf("Authorization = %q, want Bearer token-1", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"user_id":"u1","email":"user@example.com","type":"session"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	deps := DefaultDeps()
	issuer, err := deps.DiscoverIssuer(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("DiscoverIssuer: %v", err)
	}
	if issuer != "https://issuer.test" {
		t.Errorf("issuer = %q", issuer)
	}
	id, err := deps.FetchIdentity(context.Background(), srv.URL, "token-1")
	if err != nil {
		t.Fatalf("FetchIdentity: %v", err)
	}
	if id.UserID != "u1" || id.Email != "user@example.com" {
		t.Errorf("identity = %+v", id)
	}
}

func TestDiscoverIssuerStandaloneFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"standalone":true}`))
	}))
	defer srv.Close()

	if _, err := DefaultDeps().DiscoverIssuer(context.Background(), srv.URL); err == nil {
		t.Fatal("DiscoverIssuer on standalone server: expected error, got nil")
	}
}

func TestLoginPersistsSessionAndActive(t *testing.T) {
	dir := t.TempDir()
	m := NewManagerWithDeps(dir, fakeDeps(t))
	serverURL := "https://memory.example.test"

	var out bytes.Buffer
	sess, err := m.Login(context.Background(), serverURL, &out)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if sess.RefreshToken != "refresh-1" {
		t.Errorf("RefreshToken = %q, want refresh-1", sess.RefreshToken)
	}
	if sess.UserEmail != "user@memory.example.test" {
		t.Errorf("UserEmail = %q", sess.UserEmail)
	}
	if !strings.Contains(out.String(), "https://verify.test?code=WXYZ-1234") || !strings.Contains(out.String(), "WXYZ-1234") {
		t.Errorf("login output should show the verification URL and code:\n%s", out.String())
	}

	files, err := filepath.Glob(filepath.Join(dir, "accounts", "*.json"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("account files = %v, want exactly 1", files)
	}
	info, err := os.Stat(files[0])
	if err != nil {
		t.Fatalf("stat session: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("session file mode = %o, want 600", perm)
	}
	raw, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("read session: %v", err)
	}
	if !strings.Contains(string(raw), "refresh-1") {
		t.Errorf("session file missing refresh token:\n%s", raw)
	}
	if active, ok := m.Active(); !ok || active != serverURL {
		t.Errorf("Active() = (%q, %v), want (%q, true)", active, ok, serverURL)
	}
}

func TestLoginWithOptionsCustomClientID(t *testing.T) {
	dir := t.TempDir()
	var gotClientID string
	device := DeviceCode{
		DeviceCode:              "device-1",
		UserCode:                "WXYZ-1234",
		VerificationURI:         "https://verify.test",
		VerificationURIComplete: "https://verify.test?code=WXYZ-1234",
		ExpiresIn:               300,
		Interval:                1,
	}
	deps := fakeDeps(t)
	deps.RequestDeviceCode = func(_ context.Context, _ *sdkauth.OIDCConfig, clientID string, _ []string) (*DeviceCode, error) {
		gotClientID = clientID
		return &device, nil
	}
	m := NewManagerWithDeps(dir, deps)
	serverURL := "https://memory.example.test"
	const custom = "390138928478289930"

	sess, err := m.LoginWithOptions(context.Background(), serverURL, LoginOptions{ClientID: custom}, io.Discard)
	if err != nil {
		t.Fatalf("LoginWithOptions: %v", err)
	}
	if gotClientID != custom {
		t.Errorf("device-code client_id = %q, want %q", gotClientID, custom)
	}
	if sess.ClientID != custom {
		t.Errorf("session client_id = %q, want %q", sess.ClientID, custom)
	}
	stored, err := m.SessionFor(serverURL)
	if err != nil || stored == nil {
		t.Fatalf("SessionFor = (%v, %v)", stored, err)
	}
	if stored.ClientID != custom {
		t.Errorf("persisted client_id = %q, want %q", stored.ClientID, custom)
	}
}

func TestLoginWithOptionsDefaultClientID(t *testing.T) {
	dir := t.TempDir()
	var gotClientID string
	device := DeviceCode{
		DeviceCode:      "device-1",
		UserCode:        "WXYZ-1234",
		VerificationURI: "https://verify.test",
		ExpiresIn:       300,
		Interval:        1,
	}
	deps := fakeDeps(t)
	deps.RequestDeviceCode = func(_ context.Context, _ *sdkauth.OIDCConfig, clientID string, _ []string) (*DeviceCode, error) {
		gotClientID = clientID
		return &device, nil
	}
	m := NewManagerWithDeps(dir, deps)

	sess, err := m.LoginWithOptions(context.Background(), "https://memory.example.test", LoginOptions{}, io.Discard)
	if err != nil {
		t.Fatalf("LoginWithOptions: %v", err)
	}
	if gotClientID != ClientID {
		t.Errorf("device-code client_id = %q, want default %q", gotClientID, ClientID)
	}
	if sess.ClientID != ClientID {
		t.Errorf("session client_id = %q, want default %q", sess.ClientID, ClientID)
	}
}

func TestLoginWithOptionsIssuerSkipsDiscovery(t *testing.T) {
	dir := t.TempDir()
	deps := fakeDeps(t)
	deps.DiscoverIssuer = func(context.Context, string) (string, error) {
		return "", errors.New("DiscoverIssuer must not be called when --issuer is set")
	}
	m := NewManagerWithDeps(dir, deps)
	const issuer = "https://issuer.native.test"

	sess, err := m.LoginWithOptions(context.Background(), "https://memory.example.test", LoginOptions{Issuer: issuer}, io.Discard)
	if err != nil {
		t.Fatalf("LoginWithOptions: %v", err)
	}
	if sess.IssuerURL != issuer {
		t.Errorf("session issuer = %q, want %q", sess.IssuerURL, issuer)
	}
}

func TestLoginWithOptionsClientIDRequired(t *testing.T) {
	m := NewManagerWithDeps(t.TempDir(), fakeDeps(t))
	m.clientID = "" // simulate a manager with no default client
	if _, err := m.LoginWithOptions(context.Background(), "https://memory.example.test", LoginOptions{}, io.Discard); err == nil {
		t.Fatal("LoginWithOptions with no effective client id: expected error, got nil")
	}
}

func TestRefreshUsesPersistedClientID(t *testing.T) {
	dir := t.TempDir()
	var gotClientID string
	deps := fakeDeps(t)
	deps.RefreshToken = func(_ context.Context, _ *sdkauth.OIDCConfig, clientID, credsPath string) (*sdkauth.Credentials, error) {
		gotClientID = clientID
		creds := &sdkauth.Credentials{AccessToken: "access-2", RefreshToken: "refresh-2", ExpiresAt: time.Now().Add(time.Hour).UTC()}
		if err := sdkauth.SaveCredentials(creds, credsPath); err != nil {
			return nil, err
		}
		return creds, nil
	}
	m := NewManagerWithDeps(dir, deps)
	serverURL := "https://memory.example.test"
	const custom = "390138928478289930"

	if _, err := m.LoginWithOptions(context.Background(), serverURL, LoginOptions{ClientID: custom}, io.Discard); err != nil {
		t.Fatalf("LoginWithOptions: %v", err)
	}
	if _, err := m.Refresh(context.Background(), serverURL); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if gotClientID != custom {
		t.Errorf("refresh client_id = %q, want persisted %q", gotClientID, custom)
	}
}

func TestRefreshLegacySessionFallsBackToDefaultClientID(t *testing.T) {
	dir := t.TempDir()
	var gotClientID string
	deps := fakeDeps(t)
	deps.RefreshToken = func(_ context.Context, _ *sdkauth.OIDCConfig, clientID, credsPath string) (*sdkauth.Credentials, error) {
		gotClientID = clientID
		creds := &sdkauth.Credentials{AccessToken: "access-2", RefreshToken: "refresh-2", ExpiresAt: time.Now().Add(time.Hour).UTC()}
		if err := sdkauth.SaveCredentials(creds, credsPath); err != nil {
			return nil, err
		}
		return creds, nil
	}
	m := NewManagerWithDeps(dir, deps)
	serverURL := "https://legacy.example.test"
	// Legacy session: no client_id stored.
	if err := m.Save(serverURL, &Session{
		ServerURL:    serverURL,
		IssuerURL:    "https://issuer.test",
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("Save legacy session: %v", err)
	}

	if _, err := m.Refresh(context.Background(), serverURL); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if gotClientID != ClientID {
		t.Errorf("refresh client_id = %q, want fallback default %q", gotClientID, ClientID)
	}
}

func TestPerServerIsolation(t *testing.T) {
	dir := t.TempDir()
	m := NewManagerWithDeps(dir, fakeDeps(t))
	serverA := "https://a.example.test"
	serverB := "https://b.example.test:8443"

	if _, err := m.Login(context.Background(), serverA, io.Discard); err != nil {
		t.Fatalf("Login A: %v", err)
	}
	if _, err := m.Login(context.Background(), serverB, io.Discard); err != nil {
		t.Fatalf("Login B: %v", err)
	}

	if active, _ := m.Active(); active != serverB {
		t.Errorf("active = %q, want most recent %q", active, serverB)
	}
	sessA, err := m.SessionFor(serverA)
	if err != nil || sessA == nil {
		t.Fatalf("SessionFor(A) = (%v, %v)", sessA, err)
	}
	sessB, err := m.SessionFor(serverB)
	if err != nil || sessB == nil {
		t.Fatalf("SessionFor(B) = (%v, %v)", sessB, err)
	}
	if sessA.UserEmail == sessB.UserEmail {
		t.Errorf("sessions for different servers should be distinct, both %q", sessA.UserEmail)
	}
	if sessA.ServerURL != serverA || sessB.ServerURL != serverB {
		t.Errorf("session server urls = %q / %q", sessA.ServerURL, sessB.ServerURL)
	}

	if err := m.SetActive(serverA); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	if active, _ := m.Active(); active != serverA {
		t.Errorf("active after switch = %q, want %q", active, serverA)
	}
}

func TestLogoutRemovesOnlyThatAccount(t *testing.T) {
	dir := t.TempDir()
	m := NewManagerWithDeps(dir, fakeDeps(t))
	serverA := "https://a.example.test"
	serverB := "https://b.example.test"

	if _, err := m.Login(context.Background(), serverA, io.Discard); err != nil {
		t.Fatalf("Login A: %v", err)
	}
	if _, err := m.Login(context.Background(), serverB, io.Discard); err != nil {
		t.Fatalf("Login B: %v", err)
	}
	if err := m.Logout(serverB); err != nil {
		t.Fatalf("Logout B: %v", err)
	}

	if sess, err := m.SessionFor(serverB); err != nil || sess != nil {
		t.Errorf("SessionFor(B) after logout = (%v, %v), want (nil, nil)", sess, err)
	}
	if sess, err := m.SessionFor(serverA); err != nil || sess == nil {
		t.Errorf("SessionFor(A) must survive logout of B = (%v, %v)", sess, err)
	}
	if _, ok := m.Active(); ok {
		t.Errorf("active should be cleared after logging out the active account")
	}
}

func TestLogoutAll(t *testing.T) {
	dir := t.TempDir()
	m := NewManagerWithDeps(dir, fakeDeps(t))
	for _, s := range []string{"https://a.example.test", "https://b.example.test"} {
		if _, err := m.Login(context.Background(), s, io.Discard); err != nil {
			t.Fatalf("Login %s: %v", s, err)
		}
	}
	if err := m.LogoutAll(); err != nil {
		t.Fatalf("LogoutAll: %v", err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "accounts", "*.json"))
	if len(files) != 0 {
		t.Errorf("account files after LogoutAll = %v, want none", files)
	}
	if _, ok := m.Active(); ok {
		t.Errorf("active should be cleared after LogoutAll")
	}
}

func TestRefreshRejectionKeepsSessionAndMarksNeedsReauth(t *testing.T) {
	dir := t.TempDir()
	deps := fakeDeps(t)
	deps.RefreshToken = func(context.Context, *sdkauth.OIDCConfig, string, string) (*sdkauth.Credentials, error) {
		return nil, &sdkauth.RefreshError{StatusCode: http.StatusBadRequest, Code: "invalid_grant"}
	}
	m := NewManagerWithDeps(dir, deps)
	serverURL := "https://memory.example.test"
	if _, err := m.Login(context.Background(), serverURL, io.Discard); err != nil {
		t.Fatalf("Login: %v", err)
	}

	if _, err := m.Refresh(context.Background(), serverURL); err == nil {
		t.Fatal("Refresh: expected error, got nil")
	}
	// A rejection must not delete the session: the refresh token is the only
	// thing that can recover the account without user interaction, and a
	// single-use rotation raced by two processes used to land here. Mark the
	// session instead, keeping both tokens.
	sess, err := m.SessionFor(serverURL)
	if err != nil {
		t.Fatalf("SessionFor after rejected refresh: %v", err)
	}
	if sess == nil {
		t.Fatal("session after rejected refresh = nil, want retained")
	}
	if !sess.NeedsReauth {
		t.Error("NeedsReauth = false after a rejected refresh, want true")
	}
	if sess.RefreshToken != "refresh-1" {
		t.Errorf("refresh token = %q, want refresh-1 retained", sess.RefreshToken)
	}
}

func TestRefreshTransientErrorRetainsSession(t *testing.T) {
	dir := t.TempDir()
	deps := fakeDeps(t)
	deps.RefreshToken = func(context.Context, *sdkauth.OIDCConfig, string, string) (*sdkauth.Credentials, error) {
		return nil, errors.New("token endpoint unreachable")
	}
	m := NewManagerWithDeps(dir, deps)
	serverURL := "https://memory.example.test"
	if _, err := m.Login(context.Background(), serverURL, io.Discard); err != nil {
		t.Fatalf("Login: %v", err)
	}

	if _, err := m.Refresh(context.Background(), serverURL); err == nil {
		t.Fatal("Refresh: expected error, got nil")
	}
	sess, err := m.SessionFor(serverURL)
	if err != nil || sess == nil {
		t.Fatalf("session after transient refresh failure = (%v, %v), want retained", sess, err)
	}
	if sess.RefreshToken != "refresh-1" {
		t.Errorf("refresh token = %q, want refresh-1 retained", sess.RefreshToken)
	}
}

func TestRefreshServerErrorRetainsSession(t *testing.T) {
	dir := t.TempDir()
	deps := fakeDeps(t)
	deps.RefreshToken = func(context.Context, *sdkauth.OIDCConfig, string, string) (*sdkauth.Credentials, error) {
		return nil, &sdkauth.RefreshError{StatusCode: http.StatusInternalServerError}
	}
	m := NewManagerWithDeps(dir, deps)
	serverURL := "https://memory.example.test"
	if _, err := m.Login(context.Background(), serverURL, io.Discard); err != nil {
		t.Fatalf("Login: %v", err)
	}

	if _, err := m.Refresh(context.Background(), serverURL); err == nil {
		t.Fatal("Refresh: expected error, got nil")
	}
	sess, err := m.SessionFor(serverURL)
	if err != nil || sess == nil {
		t.Fatalf("session after 5xx refresh failure = (%v, %v), want retained", sess, err)
	}
	if sess.RefreshToken != "refresh-1" {
		t.Errorf("refresh token = %q, want refresh-1 retained", sess.RefreshToken)
	}
}

func TestRefreshRejectedAfterTokenRotatedRetainsSession(t *testing.T) {
	dir := t.TempDir()
	serverURL := "https://memory.example.test"
	deps := fakeDeps(t)
	rotated := false
	deps.RefreshToken = func(_ context.Context, _ *sdkauth.OIDCConfig, _, _ string) (*sdkauth.Credentials, error) {
		// Simulate a concurrent process winning the single-use refresh race:
		// it rotates the token before this Refresh's original token is
		// rejected, so the stored session now holds a different refresh token
		// than the one this Refresh just attempted.
		winner := NewManager(dir)
		if err := winner.Save(serverURL, &Session{
			ServerURL:    serverURL,
			IssuerURL:    "https://issuer.test",
			AccessToken:  "access-winner",
			RefreshToken: "refresh-winner",
			ExpiresAt:    time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC),
		}); err != nil {
			t.Fatalf("winner save: %v", err)
		}
		rotated = true
		return nil, &sdkauth.RefreshError{StatusCode: http.StatusBadRequest, Code: "invalid_grant"}
	}
	m := NewManagerWithDeps(dir, deps)
	if _, err := m.Login(context.Background(), serverURL, io.Discard); err != nil {
		t.Fatalf("Login: %v", err)
	}

	if _, err := m.Refresh(context.Background(), serverURL); err == nil {
		t.Fatal("Refresh: expected error, got nil")
	}
	if !rotated {
		t.Fatal("RefreshToken fake did not run")
	}
	sess, err := m.SessionFor(serverURL)
	if err != nil || sess == nil {
		t.Fatalf("session after rotated-token rejection = (%v, %v), want retained", sess, err)
	}
	if sess.RefreshToken != "refresh-winner" {
		t.Errorf("refresh token = %q, want winner's refresh-winner retained", sess.RefreshToken)
	}
}

func TestRefreshSuccessUpdatesSession(t *testing.T) {
	dir := t.TempDir()
	m := NewManagerWithDeps(dir, fakeDeps(t))
	serverURL := "https://memory.example.test"
	if _, err := m.Login(context.Background(), serverURL, io.Discard); err != nil {
		t.Fatalf("Login: %v", err)
	}

	sess, err := m.Refresh(context.Background(), serverURL)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if sess.AccessToken != "access-2" || sess.RefreshToken != "refresh-2" {
		t.Errorf("refreshed session = %+v", sess)
	}
	stored, err := m.SessionFor(serverURL)
	if err != nil || stored == nil {
		t.Fatalf("SessionFor after refresh = (%v, %v)", stored, err)
	}
	if stored.AccessToken != "access-2" {
		t.Errorf("stored AccessToken = %q, want access-2", stored.AccessToken)
	}
}

func TestRefreshWithoutSession(t *testing.T) {
	m := NewManagerWithDeps(t.TempDir(), fakeDeps(t))
	if _, err := m.Refresh(context.Background(), "https://nobody.test"); !errors.Is(err, ErrNoSession) {
		t.Errorf("Refresh without session error = %v, want ErrNoSession", err)
	}
}

// TestUpdateSessionWaitsForTheRefreshLock is the reason the API exists: the
// read-modify-write must not run while a refresh holds the cross-process lock,
// or it would save a session built from the pre-rotation refresh token.
func TestUpdateSessionWaitsForTheRefreshLock(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)
	serverURL := "https://memory.example.test"
	if err := m.Save(serverURL, &Session{AccessToken: "access-1", RefreshToken: "refresh-1"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Stand in for an in-flight refresh: the winner takes the lock, reads the
	// pre-rotation token, rotates, and only then releases the lock.
	unlock, err := m.lockRefresh(serverURL)
	if err != nil {
		t.Fatalf("lockRefresh: %v", err)
	}
	mutated := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		_, err := m.UpdateSession(serverURL, func(s *Session) error {
			mutated <- s.RefreshToken
			s.AccessToken = "access-imported"
			return nil
		})
		done <- err
	}()

	select {
	case token := <-mutated:
		t.Fatalf("UpdateSession ran with refresh token %q while the lock was held", token)
	case <-time.After(150 * time.Millisecond):
	}

	// The refresh persists its rotated pair before dropping the lock.
	if err := m.Save(serverURL, &Session{AccessToken: "access-rotated", RefreshToken: "refresh-2"}); err != nil {
		t.Fatalf("save rotated session: %v", err)
	}
	unlock()

	if err := <-done; err != nil {
		t.Fatalf("UpdateSession: %v", err)
	}
	if token := <-mutated; token != "refresh-2" {
		t.Errorf("mutator saw refresh token %q, want the rotated refresh-2", token)
	}
	stored, err := m.SessionFor(serverURL)
	if err != nil || stored == nil {
		t.Fatalf("SessionFor = (%v, %v)", stored, err)
	}
	if stored.AccessToken != "access-imported" {
		t.Errorf("AccessToken = %q, want the mutation's access-imported", stored.AccessToken)
	}
	if stored.RefreshToken != "refresh-2" {
		t.Errorf("RefreshToken = %q, want refresh-2 (the rotation must survive the update)", stored.RefreshToken)
	}
}

// TestUpdateSessionMutatorErrorWritesNothing keeps the read-modify-write atomic
// on the failure path too.
func TestUpdateSessionMutatorErrorWritesNothing(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)
	serverURL := "https://memory.example.test"
	if err := m.Save(serverURL, &Session{AccessToken: "access-1", RefreshToken: "refresh-1"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	wantErr := errors.New("mutator refused")
	sess, err := m.UpdateSession(serverURL, func(s *Session) error {
		s.AccessToken = "access-should-not-persist"
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("UpdateSession error = %v, want the mutator's error", err)
	}
	if sess != nil {
		t.Errorf("UpdateSession session = %+v, want nil on a mutator error", sess)
	}
	stored, err := m.SessionFor(serverURL)
	if err != nil || stored == nil {
		t.Fatalf("SessionFor = (%v, %v)", stored, err)
	}
	if stored.AccessToken != "access-1" {
		t.Errorf("AccessToken = %q, want the unchanged access-1", stored.AccessToken)
	}
}

// TestUpdateSessionCreatesAnAbsentSession is what `auth import` relies on: an
// import into a fresh store starts from an empty document instead of failing.
func TestUpdateSessionCreatesAnAbsentSession(t *testing.T) {
	m := NewManager(t.TempDir())
	serverURL := "https://memory.example.test"

	sawEmpty := false
	sess, err := m.UpdateSession(serverURL, func(s *Session) error {
		sawEmpty = s.AccessToken == "" && s.RefreshToken == "" && s.ServerURL == serverURL
		s.AccessToken = "access-new"
		s.RefreshToken = "refresh-new"
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateSession: %v", err)
	}
	if !sawEmpty {
		t.Error("mutator did not start from an empty document with the server URL set")
	}
	if sess.AccessToken != "access-new" || sess.ServerURL != serverURL {
		t.Errorf("returned session = %+v", sess)
	}
	stored, err := m.SessionFor(serverURL)
	if err != nil || stored == nil {
		t.Fatalf("SessionFor = (%v, %v), want the created session", stored, err)
	}
	if stored.AccessToken != "access-new" || stored.RefreshToken != "refresh-new" {
		t.Errorf("stored session = %+v", stored)
	}
}

func TestImportCLISessionReadsWithoutMutating(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	body := `{"access_token":"cli-access","refresh_token":"cli-refresh","expires_at":"2030-01-02T03:04:05Z","user_email":"cli@example.com","issuer_url":"https://issuer.test"}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write CLI credentials: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read before: %v", err)
	}

	sess, err := ImportCLISession(path)
	if err != nil {
		t.Fatalf("ImportCLISession: %v", err)
	}
	if sess.AccessToken != "cli-access" || sess.RefreshToken != "cli-refresh" || sess.UserEmail != "cli@example.com" || sess.IssuerURL != "https://issuer.test" {
		t.Errorf("imported session = %+v", sess)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("CLI credentials file was mutated:\nbefore: %s\nafter:  %s", before, after)
	}
}

func TestImportCLISessionMissingFile(t *testing.T) {
	if _, err := ImportCLISession(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("ImportCLISession on missing file: expected error, got nil")
	}
}

// freshSession persists a session with an access token that expires in d.
func freshSession(t *testing.T, m *Manager, serverURL, accessToken string, d time.Duration) {
	t.Helper()
	if err := m.Save(serverURL, &Session{
		AccessToken:  accessToken,
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(d).UTC(),
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
}

func TestAccessTokenRefreshesNearExpiry(t *testing.T) {
	dir := t.TempDir()
	m := NewManagerWithDeps(dir, fakeDeps(t))
	serverURL := "https://memory.example.test"
	freshSession(t, m, serverURL, "stale", 30*time.Second)

	token, err := m.AccessToken(context.Background(), serverURL, 5*time.Minute)
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if token != "access-2" {
		t.Errorf("AccessToken = %q, want the refreshed access-2", token)
	}
	stored, err := m.SessionFor(serverURL)
	if err != nil || stored == nil {
		t.Fatalf("SessionFor = (%v, %v)", stored, err)
	}
	if stored.AccessToken != "access-2" || stored.RefreshToken != "refresh-2" {
		t.Errorf("stored session = %+v, want the refreshed token pair", stored)
	}
	if stored.NeedsReauth {
		t.Error("NeedsReauth = true after a successful refresh")
	}
}

func TestAccessTokenKeepsFreshToken(t *testing.T) {
	dir := t.TempDir()
	var refreshes int32
	deps := fakeDeps(t)
	base := deps.RefreshToken
	deps.RefreshToken = func(ctx context.Context, oidc *sdkauth.OIDCConfig, clientID, credsPath string) (*sdkauth.Credentials, error) {
		atomic.AddInt32(&refreshes, 1)
		return base(ctx, oidc, clientID, credsPath)
	}
	m := NewManagerWithDeps(dir, deps)
	serverURL := "https://memory.example.test"
	freshSession(t, m, serverURL, "still-good", 2*time.Hour)

	token, err := m.AccessToken(context.Background(), serverURL, 5*time.Minute)
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if token != "still-good" {
		t.Errorf("AccessToken = %q, want the stored token", token)
	}
	if got := atomic.LoadInt32(&refreshes); got != 0 {
		t.Errorf("refreshes = %d, want 0 for a token outside the window", got)
	}
}

func TestAccessTokenRejectionMarksNeedsReauth(t *testing.T) {
	dir := t.TempDir()
	deps := fakeDeps(t)
	deps.RefreshToken = func(context.Context, *sdkauth.OIDCConfig, string, string) (*sdkauth.Credentials, error) {
		return nil, &sdkauth.RefreshError{StatusCode: http.StatusBadRequest, Code: "invalid_grant"}
	}
	m := NewManagerWithDeps(dir, deps)
	serverURL := "https://memory.example.test"
	freshSession(t, m, serverURL, "stale", 0)

	if _, err := m.AccessToken(context.Background(), serverURL, 5*time.Minute); err == nil {
		t.Fatal("AccessToken: expected error from a rejected refresh")
	}
	stored, err := m.SessionFor(serverURL)
	if err != nil || stored == nil {
		t.Fatalf("SessionFor = (%v, %v), want the session retained", stored, err)
	}
	if !stored.NeedsReauth {
		t.Error("NeedsReauth = false after a rejected refresh, want true")
	}
	if stored.RefreshToken != "refresh-1" {
		t.Errorf("refresh token = %q, want refresh-1 retained", stored.RefreshToken)
	}
}

// TestAccessTokenSerializesRefreshesAcrossManagers is the cross-process race in
// miniature. Zitadel refresh tokens are single-use, and the macOS app spawns a
// separate `memory-connector` process per token read, so two refreshes at once
// mean one wins and the other is rejected — which the app used to surface as
// "signed out". Two Managers over one directory stand in for two processes: the
// lock is flock(2)-based, so each Manager's own file descriptor contends exactly
// as a second process would.
func TestAccessTokenSerializesRefreshesAcrossManagers(t *testing.T) {
	dir := t.TempDir()
	var refreshes int32
	deps := fakeDeps(t)
	base := deps.RefreshToken
	deps.RefreshToken = func(ctx context.Context, oidc *sdkauth.OIDCConfig, clientID, credsPath string) (*sdkauth.Credentials, error) {
		atomic.AddInt32(&refreshes, 1)
		// Hold the refresh long enough that an unserialized second caller
		// would definitely enter it too.
		time.Sleep(100 * time.Millisecond)
		return base(ctx, oidc, clientID, credsPath)
	}
	serverURL := "https://memory.example.test"
	freshSession(t, NewManagerWithDeps(dir, deps), serverURL, "stale", 10*time.Second)

	var wg sync.WaitGroup
	tokens := make([]string, 2)
	errs := make([]error, 2)
	for i := range tokens {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m := NewManagerWithDeps(dir, deps)
			tokens[i], errs[i] = m.AccessToken(context.Background(), serverURL, 5*time.Minute)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("AccessToken[%d]: %v", i, err)
		}
		if tokens[i] != "access-2" {
			t.Errorf("AccessToken[%d] = %q, want the winner's access-2", i, tokens[i])
		}
	}
	if got := atomic.LoadInt32(&refreshes); got != 1 {
		t.Errorf("refreshes = %d, want 1 — the loser must reuse the winner's token", got)
	}
}

func TestAccessTokenWithoutRefreshTokenReturnsStored(t *testing.T) {
	dir := t.TempDir()
	m := NewManagerWithDeps(dir, fakeDeps(t))
	serverURL := "https://memory.example.test"
	if err := m.Save(serverURL, &Session{AccessToken: "no-refresh", ExpiresAt: time.Now().Add(-time.Minute)}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	token, err := m.AccessToken(context.Background(), serverURL, 5*time.Minute)
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if token != "no-refresh" {
		t.Errorf("AccessToken = %q, want the stored token", token)
	}
}

func TestRefreshCodedNonGrantErrorIsNotARejection(t *testing.T) {
	dir := t.TempDir()
	deps := fakeDeps(t)
	deps.RefreshToken = func(context.Context, *sdkauth.OIDCConfig, string, string) (*sdkauth.Credentials, error) {
		// A 400 whose code is not about the grant itself: the client config is
		// wrong, so an interactive sign-in would fail identically. Marking the
		// account "needs re-auth" would loop the user through a useless login.
		return nil, &sdkauth.RefreshError{StatusCode: http.StatusBadRequest, Code: "invalid_client"}
	}
	m := NewManagerWithDeps(dir, deps)
	serverURL := "https://memory.example.test"
	if _, err := m.Login(context.Background(), serverURL, io.Discard); err != nil {
		t.Fatalf("Login: %v", err)
	}

	if _, err := m.Refresh(context.Background(), serverURL); err == nil {
		t.Fatal("Refresh: expected the configuration error to surface")
	}
	sess, err := m.SessionFor(serverURL)
	if err != nil || sess == nil {
		t.Fatalf("SessionFor = (%v, %v), want retained", sess, err)
	}
	if sess.NeedsReauth {
		t.Error("NeedsReauth = true for a non-grant error, want false")
	}
	if sess.RefreshToken != "refresh-1" {
		t.Errorf("refresh token = %q, want refresh-1 kept", sess.RefreshToken)
	}
}
