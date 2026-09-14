package project

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/account"
	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/memoryapi"
)

type fakeSessions struct {
	sessions map[string]*account.Session
}

func (f fakeSessions) SessionFor(serverURL string) (*account.Session, error) {
	return f.sessions[serverURL], nil
}

func signedInSessions(serverURL, token string) fakeSessions {
	return fakeSessions{sessions: map[string]*account.Session{
		serverURL: {ServerURL: serverURL, AccessToken: token},
	}}
}

// fakeDeps records calls and returns canned results.
type fakeDeps struct {
	projects    []memoryapi.Project
	createErr   error
	revokeErr   error
	createCalls int
	revokeCalls int
	lastServer  string
	lastAccess  string
	lastProject string
	lastName    string
	lastScopes  []string
	lastTokenID string
}

func (f *fakeDeps) deps() Deps {
	return Deps{
		ListProjects: func(_ context.Context, serverURL, accessToken string) ([]memoryapi.Project, error) {
			f.lastServer, f.lastAccess = serverURL, accessToken
			return f.projects, nil
		},
		CreateToken: func(_ context.Context, serverURL, accessToken, projectID, name string, scopes []string) (memoryapi.CreatedToken, error) {
			f.createCalls++
			f.lastServer, f.lastAccess = serverURL, accessToken
			f.lastProject, f.lastName, f.lastScopes = projectID, name, scopes
			if f.createErr != nil {
				return memoryapi.CreatedToken{}, f.createErr
			}
			return memoryapi.CreatedToken{ID: "tok-" + projectID, Name: name, Token: "emt_" + projectID, Scopes: scopes}, nil
		},
		RevokeToken: func(_ context.Context, serverURL, accessToken, projectID, tokenID string) error {
			f.revokeCalls++
			f.lastServer, f.lastAccess = serverURL, accessToken
			f.lastProject, f.lastTokenID = projectID, tokenID
			return f.revokeErr
		},
	}
}

func TestListNotSignedIn(t *testing.T) {
	m := NewManagerWithDeps(t.TempDir(), fakeSessions{}, (&fakeDeps{}).deps())
	_, err := m.List(context.Background(), "https://srv.test")
	if !errors.Is(err, ErrNotSignedIn) {
		t.Fatalf("List error = %v, want ErrNotSignedIn", err)
	}
}

func TestListReturnsProjectsAndUsesAccessToken(t *testing.T) {
	fd := &fakeDeps{projects: []memoryapi.Project{{ID: "p1", Name: "One"}, {ID: "p2", Name: "Two"}}}
	m := NewManagerWithDeps(t.TempDir(), signedInSessions("https://srv.test", "oauth-access"), fd.deps())

	projects, err := m.List(context.Background(), "https://srv.test")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !reflect.DeepEqual(projects, fd.projects) {
		t.Errorf("projects = %+v, want %+v", projects, fd.projects)
	}
	if fd.lastAccess != "oauth-access" || fd.lastServer != "https://srv.test" {
		t.Errorf("api call used (%q, %q), want server + oauth-access", fd.lastServer, fd.lastAccess)
	}
}

func TestActiveRoundTripAndMode(t *testing.T) {
	dir := t.TempDir()
	m := NewManagerWithDeps(dir, fakeSessions{}, (&fakeDeps{}).deps())
	serverURL := "https://srv.test"

	if _, ok := m.Active(serverURL); ok {
		t.Fatal("Active on fresh store = set, want unset")
	}
	if err := m.SetActive(serverURL, "p1", "Project One"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	got, ok := m.Active(serverURL)
	if !ok || got.ID != "p1" || got.Name != "Project One" {
		t.Errorf("Active = (%+v, %v), want p1/Project One", got, ok)
	}

	files, _ := filepath.Glob(filepath.Join(dir, "projects", "*.json"))
	if len(files) != 1 {
		t.Fatalf("project files = %v, want 1", files)
	}
	info, err := os.Stat(files[0])
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("project file mode = %o, want 600", perm)
	}
}

func TestEnsureTokenMintsThenReuses(t *testing.T) {
	dir := t.TempDir()
	serverURL := "https://srv.test"
	fd := &fakeDeps{}
	m := NewManagerWithDeps(dir, signedInSessions(serverURL, "oauth-access"), fd.deps())

	first, err := m.EnsureToken(context.Background(), serverURL, "p1")
	if err != nil {
		t.Fatalf("EnsureToken first: %v", err)
	}
	if first != "emt_p1" {
		t.Errorf("token = %q, want emt_p1", first)
	}
	if fd.createCalls != 1 {
		t.Fatalf("CreateToken calls = %d, want 1", fd.createCalls)
	}

	second, err := m.EnsureToken(context.Background(), serverURL, "p1")
	if err != nil {
		t.Fatalf("EnsureToken second: %v", err)
	}
	if second != first {
		t.Errorf("reused token = %q, want %q", second, first)
	}
	if fd.createCalls != 1 {
		t.Errorf("CreateToken calls after reuse = %d, want 1", fd.createCalls)
	}

	files, _ := filepath.Glob(filepath.Join(dir, "tokens", "*.json"))
	if len(files) != 1 {
		t.Fatalf("token files = %v, want 1", files)
	}
	info, err := os.Stat(files[0])
	if err != nil {
		t.Fatalf("stat token: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("token file mode = %o, want 600", perm)
	}
	raw, _ := os.ReadFile(files[0])
	if !strings.Contains(string(raw), "emt_p1") {
		t.Errorf("stored token file missing token:\n%s", raw)
	}
}

func TestEnsureTokenUsesLeastPrivilegeScopesAndName(t *testing.T) {
	fd := &fakeDeps{}
	m := NewManagerWithDeps(t.TempDir(), signedInSessions("https://srv.test", "at"), fd.deps())

	if _, err := m.EnsureToken(context.Background(), "https://srv.test", "p1"); err != nil {
		t.Fatalf("EnsureToken: %v", err)
	}
	if !reflect.DeepEqual(fd.lastScopes, []string{"data:read"}) {
		t.Errorf("scopes = %v, want [data:read]", fd.lastScopes)
	}
	if fd.lastName != m.TokenName() || !strings.HasPrefix(fd.lastName, "connector-") {
		t.Errorf("token name = %q, want %q", fd.lastName, m.TokenName())
	}
}

func TestEnsureTokenNotSignedIn(t *testing.T) {
	m := NewManagerWithDeps(t.TempDir(), fakeSessions{}, (&fakeDeps{}).deps())
	if _, err := m.EnsureToken(context.Background(), "https://srv.test", "p1"); !errors.Is(err, ErrNotSignedIn) {
		t.Fatalf("EnsureToken error = %v, want ErrNotSignedIn", err)
	}
}

func TestRevokeRevokesAndDeletes(t *testing.T) {
	dir := t.TempDir()
	serverURL := "https://srv.test"
	fd := &fakeDeps{}
	m := NewManagerWithDeps(dir, signedInSessions(serverURL, "at"), fd.deps())
	if _, err := m.EnsureToken(context.Background(), serverURL, "p1"); err != nil {
		t.Fatalf("EnsureToken: %v", err)
	}

	if err := m.Revoke(context.Background(), serverURL, "p1"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if fd.revokeCalls != 1 || fd.lastTokenID != "tok-p1" || fd.lastProject != "p1" {
		t.Errorf("revoke called with (%d, %q, %q), want 1/tok-p1/p1", fd.revokeCalls, fd.lastTokenID, fd.lastProject)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "tokens", "*.json"))
	if len(files) != 0 {
		t.Errorf("token files after revoke = %v, want none", files)
	}
	// Re-mint after revoke calls create again.
	if _, err := m.EnsureToken(context.Background(), serverURL, "p1"); err != nil {
		t.Fatalf("EnsureToken after revoke: %v", err)
	}
	if fd.createCalls != 2 {
		t.Errorf("CreateToken calls after revoke = %d, want 2", fd.createCalls)
	}
}

func TestRevokeBestEffortDeletesOnServerFailure(t *testing.T) {
	dir := t.TempDir()
	serverURL := "https://srv.test"
	fd := &fakeDeps{revokeErr: errors.New("gone")}
	m := NewManagerWithDeps(dir, signedInSessions(serverURL, "at"), fd.deps())
	if _, err := m.EnsureToken(context.Background(), serverURL, "p1"); err != nil {
		t.Fatalf("EnsureToken: %v", err)
	}

	if err := m.Revoke(context.Background(), serverURL, "p1"); err != nil {
		t.Fatalf("Revoke (best-effort) = %v, want nil", err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "tokens", "*.json"))
	if len(files) != 0 {
		t.Errorf("token files after best-effort revoke = %v, want none", files)
	}
}

func TestRevokeMissingIsNoop(t *testing.T) {
	m := NewManagerWithDeps(t.TempDir(), fakeSessions{}, (&fakeDeps{}).deps())
	if err := m.Revoke(context.Background(), "https://srv.test", "nope"); err != nil {
		t.Errorf("Revoke missing = %v, want nil", err)
	}
}

func TestClearAccountTokensOnlyForAccount(t *testing.T) {
	dir := t.TempDir()
	serverA := "https://a.example.test"
	serverB := "https://b.example.test"
	sessions := fakeSessions{sessions: map[string]*account.Session{
		serverA: {ServerURL: serverA, AccessToken: "a"},
		serverB: {ServerURL: serverB, AccessToken: "b"},
	}}
	fd := &fakeDeps{}
	m := NewManagerWithDeps(dir, sessions, fd.deps())
	for _, s := range []string{serverA, serverB} {
		if _, err := m.EnsureToken(context.Background(), s, "p1"); err != nil {
			t.Fatalf("EnsureToken %s: %v", s, err)
		}
	}

	if err := m.ClearAccountTokens(serverA); err != nil {
		t.Fatalf("ClearAccountTokens: %v", err)
	}
	if _, ok := m.loadToken(serverA, "p1"); ok {
		t.Errorf("account A token still present after clear")
	}
	if _, ok := m.loadToken(serverB, "p1"); !ok {
		t.Errorf("account B token must survive clearing account A")
	}
}

func TestTokenName(t *testing.T) {
	m := NewManagerWithDeps(t.TempDir(), fakeSessions{}, (&fakeDeps{}).deps())
	if name := m.TokenName(); !strings.HasPrefix(name, "connector-") || len(name) <= len("connector-") {
		t.Errorf("TokenName() = %q, want connector-<hostname>", name)
	}
}
