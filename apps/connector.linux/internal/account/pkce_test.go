package account

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdkauth "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/auth"
)

func pkceDeps(t *testing.T, now func() time.Time, exchange func(context.Context, *sdkauth.OIDCConfig, string, string, string, string) (*sdkauth.Credentials, error)) Deps {
	t.Helper()
	return Deps{
		DiscoverIssuer: func(context.Context, string) (string, error) { return "https://issuer.test", nil },
		DiscoverOIDC: func(issuer string) (*sdkauth.OIDCConfig, error) {
			return &sdkauth.OIDCConfig{
				Issuer:                issuer,
				AuthorizationEndpoint: "https://issuer.test/authorize",
				TokenEndpoint:         "https://issuer.test/token",
				UserinfoEndpoint:      "https://issuer.test/userinfo",
			}, nil
		},
		FetchIdentity: func(_ context.Context, serverURL, _ string) (Identity, error) {
			return Identity{UserID: "user-1", Email: "user@" + hostOf(t, serverURL)}, nil
		},
		ExchangeCode: exchange,
		Now:          now,
	}
}

func loadPendingRecord(t *testing.T, dir, loginID string) pendingLogin {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "pending", loginID+".json"))
	if err != nil {
		t.Fatalf("read pending record: %v", err)
	}
	var rec pendingLogin
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatalf("parse pending record: %v", err)
	}
	return rec
}

func TestStartPKCEBuildsAuthorizeURLAndPersistsVerifier(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	m := NewManagerWithDeps(dir, pkceDeps(t, func() time.Time { return now }, func(context.Context, *sdkauth.OIDCConfig, string, string, string, string) (*sdkauth.Credentials, error) {
		return nil, errors.New("exchange must not be called by StartPKCE")
	}))

	pl, err := m.StartPKCE(context.Background(), "https://memory.example.test", "myapp://callback", PKCEOptions{})
	if err != nil {
		t.Fatalf("StartPKCE: %v", err)
	}
	if pl.LoginID == "" || pl.State == "" {
		t.Fatalf("pending = %+v, want login id and state", pl)
	}
	if !pl.ExpiresAt.Equal(now.Add(PendingLoginTTL)) {
		t.Errorf("ExpiresAt = %v, want %v", pl.ExpiresAt, now.Add(PendingLoginTTL))
	}

	u, err := url.Parse(pl.AuthorizeURL)
	if err != nil {
		t.Fatalf("parse authorize URL: %v", err)
	}
	q := u.Query()
	if q.Get("response_type") != "code" {
		t.Errorf("response_type = %q, want code", q.Get("response_type"))
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method = %q, want S256", q.Get("code_challenge_method"))
	}
	if q.Get("code_challenge") == "" {
		t.Error("code_challenge must not be empty")
	}
	if q.Get("state") != pl.State {
		t.Errorf("state = %q, want %q", q.Get("state"), pl.State)
	}
	if q.Get("client_id") != ClientID {
		t.Errorf("client_id = %q, want %q", q.Get("client_id"), ClientID)
	}
	if q.Get("redirect_uri") != "myapp://callback" {
		t.Errorf("redirect_uri = %q", q.Get("redirect_uri"))
	}
	for _, scope := range Scopes {
		if !strings.Contains(q.Get("scope"), scope) {
			t.Errorf("scope = %q, missing %q", q.Get("scope"), scope)
		}
	}

	// Pending record persisted 0600, with a verifier and matching challenge.
	path := filepath.Join(dir, "pending", pl.LoginID+".json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat pending: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("pending mode = %o, want 600", perm)
	}
	rec := loadPendingRecord(t, dir, pl.LoginID)
	if len(rec.CodeVerifier) < 43 || len(rec.CodeVerifier) > 128 {
		t.Errorf("verifier length = %d, want 43..128", len(rec.CodeVerifier))
	}
	if strings.ContainsAny(rec.CodeVerifier, "+/=") {
		t.Errorf("verifier %q must use unreserved characters", rec.CodeVerifier)
	}
	if pkceChallenge(rec.CodeVerifier) != q.Get("code_challenge") {
		t.Error("code_challenge is not S256(verifier)")
	}
	if rec.State != pl.State || rec.RedirectURI != "myapp://callback" || rec.Issuer != "https://issuer.test" || rec.ClientID != ClientID {
		t.Errorf("pending record = %+v", rec)
	}

	// The verifier must never leak through the returned struct.
	encoded, err := json.Marshal(pl)
	if err != nil {
		t.Fatalf("marshal PendingLogin: %v", err)
	}
	if strings.Contains(string(encoded), rec.CodeVerifier) {
		t.Errorf("returned PendingLogin leaked the code verifier: %s", encoded)
	}
}

func TestCompletePKCEHappyPath(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	serverURL := "https://memory.example.test"

	var gotClient, gotCode, gotVerifier, gotRedirect string
	var calls int
	deps := pkceDeps(t, func() time.Time { return now }, func(_ context.Context, _ *sdkauth.OIDCConfig, clientID, code, verifier, redirect string) (*sdkauth.Credentials, error) {
		calls++
		gotClient, gotCode, gotVerifier, gotRedirect = clientID, code, verifier, redirect
		return &sdkauth.Credentials{AccessToken: "access-1", RefreshToken: "refresh-1", ExpiresAt: now.Add(time.Hour)}, nil
	})
	m := NewManagerWithDeps(dir, deps)

	pl, err := m.StartPKCE(context.Background(), serverURL, "myapp://callback", PKCEOptions{})
	if err != nil {
		t.Fatalf("StartPKCE: %v", err)
	}
	wantVerifier := loadPendingRecord(t, dir, pl.LoginID).CodeVerifier

	sess, err := m.CompletePKCE(context.Background(), serverURL, pl.LoginID, "auth-code-1", pl.State)
	if err != nil {
		t.Fatalf("CompletePKCE: %v", err)
	}
	if gotClient != ClientID || gotCode != "auth-code-1" || gotVerifier != wantVerifier || gotRedirect != "myapp://callback" {
		t.Errorf("exchange args = (%q, %q, %q, %q)", gotClient, gotCode, gotVerifier, gotRedirect)
	}
	if calls != 1 {
		t.Errorf("exchange calls = %d, want 1", calls)
	}
	if sess.AccessToken != "access-1" || sess.RefreshToken != "refresh-1" || sess.UserEmail != "user@memory.example.test" {
		t.Errorf("session = %+v", sess)
	}
	stored, err := m.SessionFor(serverURL)
	if err != nil || stored == nil || stored.AccessToken != "access-1" {
		t.Fatalf("SessionFor after complete = (%v, %v)", stored, err)
	}
	if active, ok := m.Active(); !ok || active != serverURL {
		t.Errorf("active = (%q, %v), want %q", active, ok, serverURL)
	}

	// One-shot: record removed and replay fails.
	if _, err := os.Stat(filepath.Join(dir, "pending", pl.LoginID+".json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("pending record still present after complete (err=%v)", err)
	}
	if _, err := m.CompletePKCE(context.Background(), serverURL, pl.LoginID, "auth-code-1", pl.State); !errors.Is(err, ErrUnknownLogin) {
		t.Errorf("second CompletePKCE error = %v, want ErrUnknownLogin", err)
	}
}

func TestCompletePKCEStateMismatch(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	serverURL := "https://memory.example.test"
	var exchangeCalled bool
	deps := pkceDeps(t, func() time.Time { return now }, func(context.Context, *sdkauth.OIDCConfig, string, string, string, string) (*sdkauth.Credentials, error) {
		exchangeCalled = true
		return &sdkauth.Credentials{AccessToken: "access-1"}, nil
	})
	m := NewManagerWithDeps(dir, deps)

	pl, err := m.StartPKCE(context.Background(), serverURL, "myapp://callback", PKCEOptions{})
	if err != nil {
		t.Fatalf("StartPKCE: %v", err)
	}
	if _, err := m.CompletePKCE(context.Background(), serverURL, pl.LoginID, "code", "wrong-state"); !errors.Is(err, ErrStateMismatch) {
		t.Fatalf("CompletePKCE error = %v, want ErrStateMismatch", err)
	}
	if exchangeCalled {
		t.Error("token exchange must not run on a state mismatch")
	}
	if sess, err := m.SessionFor(serverURL); err != nil || sess != nil {
		t.Errorf("session persisted despite mismatch = (%v, %v)", sess, err)
	}
	// The mismatched pending record is invalidated.
	if _, err := m.CompletePKCE(context.Background(), serverURL, pl.LoginID, "code", pl.State); !errors.Is(err, ErrUnknownLogin) {
		t.Errorf("CompletePKCE after mismatch = %v, want ErrUnknownLogin", err)
	}
}

func TestCompletePKCEUnknownLogin(t *testing.T) {
	m := NewManagerWithDeps(t.TempDir(), pkceDeps(t, time.Now, func(context.Context, *sdkauth.OIDCConfig, string, string, string, string) (*sdkauth.Credentials, error) {
		return nil, errors.New("unused")
	}))
	for _, loginID := range []string{"does-not-exist", "../../etc/passwd", ""} {
		if _, err := m.CompletePKCE(context.Background(), "https://memory.example.test", loginID, "code", "state"); !errors.Is(err, ErrUnknownLogin) {
			t.Errorf("CompletePKCE(%q) error = %v, want ErrUnknownLogin", loginID, err)
		}
	}
}

func TestCompletePKCEExpired(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	serverURL := "https://memory.example.test"
	m := NewManagerWithDeps(dir, pkceDeps(t, func() time.Time { return now }, func(context.Context, *sdkauth.OIDCConfig, string, string, string, string) (*sdkauth.Credentials, error) {
		return &sdkauth.Credentials{AccessToken: "access-1"}, nil
	}))

	pl, err := m.StartPKCE(context.Background(), serverURL, "myapp://callback", PKCEOptions{})
	if err != nil {
		t.Fatalf("StartPKCE: %v", err)
	}
	now = now.Add(PendingLoginTTL + time.Second)

	if _, err := m.CompletePKCE(context.Background(), serverURL, pl.LoginID, "code", pl.State); !errors.Is(err, ErrExpiredLogin) {
		t.Fatalf("CompletePKCE error = %v, want ErrExpiredLogin", err)
	}
	if sess, err := m.SessionFor(serverURL); err != nil || sess != nil {
		t.Errorf("session persisted despite expiry = (%v, %v)", sess, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "pending", pl.LoginID+".json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expired pending record should be deleted (err=%v)", err)
	}
}

func TestStartPKCEPrunesExpiredRecords(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	serverURL := "https://memory.example.test"
	m := NewManagerWithDeps(dir, pkceDeps(t, func() time.Time { return now }, func(context.Context, *sdkauth.OIDCConfig, string, string, string, string) (*sdkauth.Credentials, error) {
		return nil, errors.New("unused")
	}))

	first, err := m.StartPKCE(context.Background(), serverURL, "myapp://callback", PKCEOptions{})
	if err != nil {
		t.Fatalf("StartPKCE first: %v", err)
	}
	now = now.Add(PendingLoginTTL + time.Second)
	if _, err := m.StartPKCE(context.Background(), serverURL, "myapp://callback", PKCEOptions{}); err != nil {
		t.Fatalf("StartPKCE second: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "pending", first.LoginID+".json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expired record should be pruned on the next start (err=%v)", err)
	}
}

func TestPKCEConcurrentLoginsIsolated(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	serverA := "https://a.example.test"
	serverB := "https://b.example.test"

	deps := pkceDeps(t, func() time.Time { return now }, func(_ context.Context, _ *sdkauth.OIDCConfig, _, _ string, verifier, _ string) (*sdkauth.Credentials, error) {
		return &sdkauth.Credentials{AccessToken: "access-" + verifier, ExpiresAt: now.Add(time.Hour)}, nil
	})
	m := NewManagerWithDeps(dir, deps)

	a, err := m.StartPKCE(context.Background(), serverA, "myapp://a", PKCEOptions{})
	if err != nil {
		t.Fatalf("StartPKCE A: %v", err)
	}
	b, err := m.StartPKCE(context.Background(), serverB, "myapp://b", PKCEOptions{})
	if err != nil {
		t.Fatalf("StartPKCE B: %v", err)
	}
	recA := loadPendingRecord(t, dir, a.LoginID)
	recB := loadPendingRecord(t, dir, b.LoginID)
	if recA.CodeVerifier == recB.CodeVerifier || recA.State == recB.State {
		t.Fatal("concurrent logins must have distinct verifiers and states")
	}

	// Complete the second first, then the first.
	sessB, err := m.CompletePKCE(context.Background(), serverB, b.LoginID, "code-b", b.State)
	if err != nil {
		t.Fatalf("CompletePKCE B: %v", err)
	}
	sessA, err := m.CompletePKCE(context.Background(), serverA, a.LoginID, "code-a", a.State)
	if err != nil {
		t.Fatalf("CompletePKCE A: %v", err)
	}
	if sessB.AccessToken != "access-"+recB.CodeVerifier {
		t.Errorf("B token = %q, want its own verifier", sessB.AccessToken)
	}
	if sessA.AccessToken != "access-"+recA.CodeVerifier {
		t.Errorf("A token = %q, want its own verifier", sessA.AccessToken)
	}
	if sessA.ServerURL != serverA || sessB.ServerURL != serverB {
		t.Errorf("server urls = %q / %q", sessA.ServerURL, sessB.ServerURL)
	}
}

func TestCancelPKCE(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	serverURL := "https://memory.example.test"
	m := NewManagerWithDeps(dir, pkceDeps(t, func() time.Time { return now }, func(context.Context, *sdkauth.OIDCConfig, string, string, string, string) (*sdkauth.Credentials, error) {
		return &sdkauth.Credentials{AccessToken: "access-1"}, nil
	}))

	pl, err := m.StartPKCE(context.Background(), serverURL, "myapp://callback", PKCEOptions{})
	if err != nil {
		t.Fatalf("StartPKCE: %v", err)
	}
	if err := m.CancelPKCE(pl.LoginID); err != nil {
		t.Fatalf("CancelPKCE: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "pending", pl.LoginID+".json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("pending record should be gone after cancel (err=%v)", err)
	}
	if _, err := m.CompletePKCE(context.Background(), serverURL, pl.LoginID, "code", pl.State); !errors.Is(err, ErrUnknownLogin) {
		t.Errorf("CompletePKCE after cancel = %v, want ErrUnknownLogin", err)
	}
	// Best-effort: cancelling again is not an error.
	if err := m.CancelPKCE(pl.LoginID); err != nil {
		t.Errorf("second CancelPKCE = %v, want nil", err)
	}
}

func TestStartPKCEValidation(t *testing.T) {
	m := NewManagerWithDeps(t.TempDir(), pkceDeps(t, time.Now, func(context.Context, *sdkauth.OIDCConfig, string, string, string, string) (*sdkauth.Credentials, error) {
		return nil, errors.New("unused")
	}))
	if _, err := m.StartPKCE(context.Background(), "", "myapp://callback", PKCEOptions{}); err == nil {
		t.Error("StartPKCE with empty server URL: expected error")
	}
	if _, err := m.StartPKCE(context.Background(), "https://memory.example.test", "  ", PKCEOptions{}); err == nil {
		t.Error("StartPKCE with empty redirect URI: expected error")
	}
}

func TestDefaultExchangeCode(t *testing.T) {
	var form url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		form = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at","refresh_token":"rt","expires_in":3600,"token_type":"Bearer"}`))
	}))
	defer srv.Close()

	oidc := &sdkauth.OIDCConfig{Issuer: "https://issuer.test", TokenEndpoint: srv.URL}
	creds, err := DefaultDeps().ExchangeCode(context.Background(), oidc, ClientID, "code-1", "verifier-1", "myapp://callback")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if form.Get("grant_type") != "authorization_code" {
		t.Errorf("grant_type = %q, want authorization_code", form.Get("grant_type"))
	}
	if form.Get("code") != "code-1" || form.Get("code_verifier") != "verifier-1" || form.Get("client_id") != ClientID || form.Get("redirect_uri") != "myapp://callback" {
		t.Errorf("form = %v", form)
	}
	if creds.AccessToken != "at" || creds.RefreshToken != "rt" || creds.IssuerURL != "https://issuer.test" {
		t.Errorf("creds = %+v", creds)
	}
	if creds.ExpiresAt.IsZero() {
		t.Error("ExpiresAt should be set from expires_in")
	}
}

func unusedExchange(context.Context, *sdkauth.OIDCConfig, string, string, string, string) (*sdkauth.Credentials, error) {
	return nil, errors.New("exchange must not be called")
}

func TestStartPKCEExplicitClientID(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	m := NewManagerWithDeps(dir, pkceDeps(t, func() time.Time { return now }, unusedExchange))

	const nativeClientID = "390138006318678019"
	pl, err := m.StartPKCE(context.Background(), "https://memory.example.test", "com.emergent.memory.connector://callback", PKCEOptions{ClientID: nativeClientID})
	if err != nil {
		t.Fatalf("StartPKCE: %v", err)
	}
	u, err := url.Parse(pl.AuthorizeURL)
	if err != nil {
		t.Fatalf("parse authorize URL: %v", err)
	}
	if got := u.Query().Get("client_id"); got != nativeClientID {
		t.Errorf("authorize client_id = %q, want %q", got, nativeClientID)
	}

	rec := loadPendingRecord(t, dir, pl.LoginID)
	if rec.ClientID != nativeClientID {
		t.Errorf("pending client_id = %q, want %q", rec.ClientID, nativeClientID)
	}
}

func TestStartPKCEDefaultClientID(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	m := NewManagerWithDeps(dir, pkceDeps(t, func() time.Time { return now }, unusedExchange))

	pl, err := m.StartPKCE(context.Background(), "https://memory.example.test", "myapp://callback", PKCEOptions{})
	if err != nil {
		t.Fatalf("StartPKCE: %v", err)
	}
	u, _ := url.Parse(pl.AuthorizeURL)
	if got := u.Query().Get("client_id"); got != ClientID {
		t.Errorf("authorize client_id = %q, want default %q", got, ClientID)
	}
	if rec := loadPendingRecord(t, dir, pl.LoginID); rec.ClientID != ClientID {
		t.Errorf("pending client_id = %q, want default %q", rec.ClientID, ClientID)
	}

	// A whitespace-only override also falls back to the package default.
	pl2, err := m.StartPKCE(context.Background(), "https://memory.example.test", "myapp://callback", PKCEOptions{ClientID: "   "})
	if err != nil {
		t.Fatalf("StartPKCE with blank client id: %v", err)
	}
	if rec := loadPendingRecord(t, dir, pl2.LoginID); rec.ClientID != ClientID {
		t.Errorf("pending client_id = %q, want default %q", rec.ClientID, ClientID)
	}
}

func TestStartPKCEIssuerSkipsDiscovery(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)

	discoverCalls := 0
	deps := pkceDeps(t, func() time.Time { return now }, unusedExchange)
	deps.DiscoverIssuer = func(context.Context, string) (string, error) {
		discoverCalls++
		return "https://from-server.test", nil
	}
	m := NewManagerWithDeps(dir, deps)

	const issuer = "https://issuer.direct.test"
	pl, err := m.StartPKCE(context.Background(), "", "myapp://callback", PKCEOptions{Issuer: issuer})
	if err != nil {
		t.Fatalf("StartPKCE (issuer only, no server): %v", err)
	}
	if discoverCalls != 0 {
		t.Errorf("DiscoverIssuer called %d times, want 0 when opts.Issuer is set", discoverCalls)
	}
	if rec := loadPendingRecord(t, dir, pl.LoginID); rec.Issuer != issuer {
		t.Errorf("pending issuer = %q, want %q", rec.Issuer, issuer)
	}
}

func TestStartPKCEClientIDRequired(t *testing.T) {
	m := NewManagerWithDeps(t.TempDir(), pkceDeps(t, time.Now, unusedExchange))
	m.clientID = "" // simulate a manager with no default client
	if _, err := m.StartPKCE(context.Background(), "https://memory.example.test", "myapp://callback", PKCEOptions{}); err == nil {
		t.Fatal("StartPKCE with no effective client id: expected error, got nil")
	}
}
