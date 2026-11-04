package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestInitials covers the avatar-initials helper: two words, one word, empty,
// and a multi-byte first rune.
func TestInitials(t *testing.T) {
	for _, c := range []struct {
		name string
		want string
	}{
		{"Ada Lovelace", "AL"},
		{"ada", "A"},
		{"", ""},
		{"   ", ""},
		{"Grace Hopper", "GH"},
		{"Émile", "É"},
	} {
		if got := initials(c.name); got != c.want {
			t.Errorf("initials(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestAccountMenuRender asserts the populated account menu: active account
// marked, a switch row for the other account, and the profile/add/logout
// affordances.
func TestAccountMenuRender(t *testing.T) {
	user := &currentUser{Name: "Ada Lovelace", Email: "ada@x.io", Picture: "", Sub: "sub-a"}
	accounts := []currentUser{{Name: "Bob", Email: "bob@x.io", Sub: "sub-b"}}
	html := renderHTML(t, userAccountMenu(user, accounts))

	for _, want := range []string{
		`data-testid="account-menu"`,
		`data-testid="account-menu-trigger"`,
		"Ada Lovelace",
		"ada@x.io",
		"AL",
		"Bob",
		"bob@x.io",
		`value="sub-b"`,
		`action="/auth/switch"`,
		`href="/auth/add"`,
		`href="/profile"`,
		`action="/auth/logout"`,
		"Add another account",
		"My Profile",
		"Log out",
		"Signed in as",
		"Switch account",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("account menu missing %q", want)
		}
	}
	if strings.Contains(html, "<img") {
		t.Error("menu must render an initials avatar (no <img>) when Picture is empty")
	}
}

// TestAccountMenuPicture asserts the menu swaps to an <img> avatar when a
// picture URL is provided.
func TestAccountMenuPicture(t *testing.T) {
	user := &currentUser{Name: "Ada Lovelace", Email: "ada@x.io", Picture: "http://x/a.png", Sub: "sub-a"}
	html := renderHTML(t, userAccountMenu(user, nil))

	if !strings.Contains(html, `<img src="http://x/a.png"`) {
		t.Errorf("picture avatar missing <img>, got %q", html)
	}
	if strings.Contains(html, ">AL<") {
		t.Error("initials must not render when a picture avatar is shown")
	}
}

// TestAccountMenuNil asserts a nil user renders nothing (dev / API-key mode).
func TestAccountMenuNil(t *testing.T) {
	html := renderHTML(t, userAccountMenu(nil, nil))
	if strings.TrimSpace(html) != "" {
		t.Errorf("nil user must render nothing, got %q", html)
	}
}

// TestAccountMenuSingleAccountNoSwitch asserts the "Switch account" section is
// omitted when there is only the active account.
func TestAccountMenuSingleAccountNoSwitch(t *testing.T) {
	user := &currentUser{Name: "Ada Lovelace", Email: "ada@x.io", Sub: "sub-a"}
	html := renderHTML(t, userAccountMenu(user, nil))
	if strings.Contains(html, "Switch account") {
		t.Error("single account must not render the Switch account section")
	}
	if !strings.Contains(html, "Signed in as") {
		t.Error("active account section missing")
	}
}

// TestAppShellAccountMenuAbsentDevMode asserts the shell renders WITHOUT the
// account menu in dev mode (no session identity → user nil).
func TestAppShellAccountMenuAbsentDevMode(t *testing.T) {
	s := &Server{cfg: Config{MemoryProjectID: "memory"}, memory: &fakeMemory{}}
	e := sessionServer(s)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/agents", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("dev /agents = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, `data-testid="account-menu"`) {
		t.Error("account menu must not render without a session identity (dev mode)")
	}
	if strings.Contains(body, `data-testid="user-profile-footer"`) {
		t.Error("legacy sidebar user footer must not render")
	}
	// the sidebar itself still renders
	for _, want := range []string{`id="_layout-sidebar"`, "Agents", "Schedules"} {
		if !strings.Contains(body, want) {
			t.Errorf("sidebar missing %q", want)
		}
	}
}

// TestAppShellAccountMenuSessionMode asserts the shell renders the account
// menu with the active identity in session mode.
func TestAppShellAccountMenuSessionMode(t *testing.T) {
	s := &Server{cfg: Config{AuthMode: "session", SessionSecret: "test-secret"}, memory: &fakeMemory{}}
	e := sessionServer(s)

	req := httptest.NewRequest(http.MethodGet, "/agents", nil)
	addSessionCookie(t, req, "test-secret", sessionClaims{
		AccessToken:     "at",
		ActiveProjectID: "proj-1",
		Name:            "Ada Lovelace",
		Email:           "ada@x.io",
		Sub:             "sub-a",
		ExpiresAt:       time.Now().Add(time.Hour).Unix(),
	})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("session /agents = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-testid="account-menu"`,
		"Ada Lovelace",
		"ada@x.io",
		"AL",
		`action="/auth/logout"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("session-mode shell missing %q in the account menu", want)
		}
	}
	if strings.Contains(body, `data-testid="user-profile-footer"`) {
		t.Error("legacy sidebar user footer must not render in session mode")
	}
}

// TestAppShellAccountMenuWithoutName asserts the account menu renders for a
// session that has a sub but no name/email (Zitadel may omit them), and that
// the display name AND email are lazily filled from the Memory profile.
func TestAppShellAccountMenuWithoutName(t *testing.T) {
	s := &Server{
		cfg: Config{AuthMode: "session", SessionSecret: "test-secret"},
		memory: &fakeMemory{profile: &UserProfileDto{
			DisplayName: "Maciej Kucharz", FirstName: "Maciej", LastName: "Kucharz",
			Email: "maciej@example.com",
		}},
	}
	e := sessionServer(s)

	req := httptest.NewRequest(http.MethodGet, "/agents", nil)
	addSessionCookie(t, req, "test-secret", sessionClaims{
		AccessToken:     "at",
		ActiveProjectID: "proj-1",
		Sub:             "389329982813372426",
		ExpiresAt:       time.Now().Add(time.Hour).Unix(),
		// intentionally no Name/Email
	})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("session /agents = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-testid="account-menu"`,
		"Maciej Kucharz",     // lazy-filled from the profile
		"maciej@example.com", // email lazy-filled from the profile
		`action="/auth/logout"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("session-mode shell missing %q (sub present, no name/email)", want)
		}
	}
}
