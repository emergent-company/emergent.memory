package main

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// --- helpers for the recent-projects / org-activation tests ---

// recentCtx builds an echo context carrying the recent-projects cookie value
// (empty cookieValue = no cookie, as on first use).
func recentCtx(secret, cookieValue string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	if cookieValue != "" {
		req.Header.Set("Cookie", cookieHeader(recentProjectsCookieName, cookieValue))
	}
	rec := httptest.NewRecorder()
	return e.NewContext(req, rec), rec
}

// recentCookieValue extracts the just-issued recent-projects cookie value from
// a recorder, verifying it exists and its signature.
func recentCookieValue(t *testing.T, secret string, rec *httptest.ResponseRecorder) string {
	t.Helper()
	ck := findCookie(rec, recentProjectsCookieName)
	if ck == nil {
		t.Fatal("recent-projects cookie not set")
	}
	return ck.Value
}

// recentList re-verifies a cookie value and returns the account's project list.
func recentList(t *testing.T, secret, value, sub string) []string {
	t.Helper()
	claims, err := verifyRecentProjects(secret, value)
	if err != nil {
		t.Fatal(err)
	}
	return claims.Recent[sub]
}

// record runs recordRecentProject and returns the recorder + re-verified list.
func record(t *testing.T, s *Server, c echo.Context, rec *httptest.ResponseRecorder, sub, projectID string) []string {
	t.Helper()
	s.recordRecentProject(c, sub, projectID)
	return recentList(t, s.cfg.SessionSecret, recentCookieValue(t, s.cfg.SessionSecret, rec), sub)
}

// --- recent-projects cookie: sign/verify + ordering/dedupe/cap ---

func TestRecentProjectsIssueVerifyRoundTrip(t *testing.T) {
	claims := recentProjectsClaims{Recent: map[string][]string{
		"sub-1": {"proj-3", "proj-1"},
		"sub-2": {"proj-9"},
	}}
	val, err := issueRecentProjects("test-secret", claims)
	if err != nil {
		t.Fatal(err)
	}
	got, err := verifyRecentProjects("test-secret", val)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, claims) {
		t.Errorf("claims = %+v, want %+v", got, claims)
	}
	// Wrong secret is rejected.
	if _, err := verifyRecentProjects("other-secret", val); err == nil {
		t.Error("wrong key: want error, got nil")
	}
	// Tampered value is rejected.
	if _, err := verifyRecentProjects("test-secret", mutateB64(val, 0)); err == nil {
		t.Error("tampered payload: want error, got nil")
	}
}

func TestRecordRecentProjectPrependsAndDedupes(t *testing.T) {
	s := &Server{cfg: sessionCfg(), memory: &fakeMemory{}}

	c, rec := recentCtx(s.cfg.SessionSecret, "")
	if got := record(t, s, c, rec, "sub-1", "proj-1"); !reflect.DeepEqual(got, []string{"proj-1"}) {
		t.Errorf("after proj-1: %v", got)
	}

	// Second record must carry the cookie forward and prepend.
	c2, rec2 := recentCtx(s.cfg.SessionSecret, recentCookieValue(t, s.cfg.SessionSecret, rec))
	if got := record(t, s, c2, rec2, "sub-1", "proj-2"); !reflect.DeepEqual(got, []string{"proj-2", "proj-1"}) {
		t.Errorf("after proj-2: %v", got)
	}

	// Re-activating proj-1 moves it to the front (no duplicate).
	c3, rec3 := recentCtx(s.cfg.SessionSecret, recentCookieValue(t, s.cfg.SessionSecret, rec2))
	if got := record(t, s, c3, rec3, "sub-1", "proj-1"); !reflect.DeepEqual(got, []string{"proj-1", "proj-2"}) {
		t.Errorf("after re-activating proj-1: %v", got)
	}

	// Accounts are keyed independently.
	claims, err := verifyRecentProjects(s.cfg.SessionSecret, recentCookieValue(t, s.cfg.SessionSecret, rec3))
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := claims.Recent["sub-2"]; exists {
		t.Errorf("sub-2 should not have recents: %+v", claims.Recent)
	}
}

func TestRecordRecentProjectCapsAtEight(t *testing.T) {
	s := &Server{cfg: sessionCfg(), memory: &fakeMemory{}}
	c, rec := recentCtx(s.cfg.SessionSecret, "")
	var cookieVal string
	for i := range 10 {
		id := "proj-" + string(rune('a'+i))
		s.recordRecentProject(c, "sub-1", id)
		cookieVal = recentCookieValue(t, s.cfg.SessionSecret, rec)
		c, rec = recentCtx(s.cfg.SessionSecret, cookieVal) // carry forward
	}
	got := recentList(t, s.cfg.SessionSecret, cookieVal, "sub-1")
	if len(got) != 8 {
		t.Fatalf("len = %d, want 8 (%v)", len(got), got)
	}
	// Most recent first; the two oldest (proj-a, proj-b) fell off.
	want := []string{"proj-j", "proj-i", "proj-h", "proj-g", "proj-f", "proj-e", "proj-d", "proj-c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("list = %v, want %v", got, want)
	}
}

func TestRecordRecentProjectNoopWithoutSubOrID(t *testing.T) {
	s := &Server{cfg: sessionCfg(), memory: &fakeMemory{}}

	for _, tc := range []struct{ sub, id string }{
		{"", "proj-1"}, {"sub-1", ""}, {"", ""},
	} {
		c, rec := recentCtx(s.cfg.SessionSecret, "")
		s.recordRecentProject(c, tc.sub, tc.id)
		if ck := findCookie(rec, recentProjectsCookieName); ck != nil {
			t.Errorf("record(sub=%q, id=%q): cookie should not be set, got %+v", tc.sub, tc.id, ck)
		}
	}
}

// --- activateProjectInSession records into the recent cookie ---

func TestActivateProjectInSessionRecordsRecentProject(t *testing.T) {
	s := &Server{cfg: sessionCfg(), memory: &fakeMemory{}}
	e := echo.New()

	claims := testSessionClaims()
	claims.Sub = "sub-1"

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	addSessionCookie(t, req, "test-secret", claims)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	s.activateProjectInSession(c, "proj-9", "org-9")

	ck := findCookie(rec, recentProjectsCookieName)
	if ck == nil {
		t.Fatal("recent-projects cookie not issued")
	}
	got := recentList(t, "test-secret", ck.Value, "sub-1")
	if !reflect.DeepEqual(got, []string{"proj-9"}) {
		t.Errorf("recents = %v, want [proj-9]", got)
	}
	if ck.MaxAge != 31536000 {
		t.Errorf("recent cookie MaxAge = %d, want 1 year (durable)", ck.MaxAge)
	}

	// Switch project again: both recent and session cookies update.
	req2 := httptest.NewRequest(http.MethodPost, "/", nil)
	addSessionCookie(t, req2, "test-secret", claims)
	req2.Header.Add("Cookie", ck.String()) // carry the recent cookie forward alongside the session
	rec2 := httptest.NewRecorder()
	s.activateProjectInSession(e.NewContext(req2, rec2), "proj-2", "org-9")
	got = recentList(t, "test-secret", recentCookieValue(t, "test-secret", rec2), "sub-1")
	if !reflect.DeepEqual(got, []string{"proj-2", "proj-9"}) {
		t.Errorf("recents = %v, want [proj-2 proj-9]", got)
	}
}

// A session caller without a Sub (dev) records nothing but still switches.
func TestActivateProjectInSessionNoSubSkipsRecent(t *testing.T) {
	s := &Server{cfg: sessionCfg(), memory: &fakeMemory{}}
	e := echo.New()

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	addSessionCookie(t, req, "test-secret", testSessionClaims()) // no Sub
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	s.activateProjectInSession(c, "proj-1", "org-1")

	if ck := findCookie(rec, recentProjectsCookieName); ck != nil {
		t.Errorf("no Sub: recent cookie should not be set, got %+v", ck)
	}
	ck := findCookie(rec, sessionCookieName)
	if ck == nil {
		t.Fatal("session cookie should still be re-issued")
	}
	claims, err := verifySession("test-secret", ck.Value, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if claims.ActiveProjectID != "proj-1" || claims.OrgID != "org-1" {
		t.Errorf("claims = %+v", claims)
	}
}

// --- activateOrg ---

func TestActivateOrgSetsOrgAndClearsProject(t *testing.T) {
	s := &Server{cfg: sessionCfg(), memory: &fakeMemory{}}
	e := echo.New()

	claims := testSessionClaims() // ActiveProjectID proj-sess, OrgID org-sess
	claims.Sub = "sub-1"
	claims.Name = "Ada Lovelace"
	claims.Email = "ada@example.com"
	claims.Picture = "https://example.com/ada.png"
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	addSessionCookie(t, req, "test-secret", claims)
	rec := httptest.NewRecorder()
	s.activateOrg(e.NewContext(req, rec), "org-new")

	ck := findCookie(rec, sessionCookieName)
	if ck == nil {
		t.Fatal("session cookie not re-issued")
	}
	got, err := verifySession("test-secret", ck.Value, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got.OrgID != "org-new" {
		t.Errorf("OrgID = %q, want org-new", got.OrgID)
	}
	if got.ActiveProjectID != "" {
		t.Errorf("ActiveProjectID = %q, want empty", got.ActiveProjectID)
	}
	// Tokens + identity + expiry preserved.
	if got.AccessToken != claims.AccessToken || got.RefreshToken != claims.RefreshToken {
		t.Errorf("tokens not preserved: %+v", got)
	}
	if got.Name != claims.Name || got.Email != claims.Email || got.Picture != claims.Picture || got.Sub != claims.Sub {
		t.Errorf("identity not preserved: %+v", got)
	}
	if got.ExpiresAt != claims.ExpiresAt {
		t.Errorf("ExpiresAt = %d, want %d", got.ExpiresAt, claims.ExpiresAt)
	}
}

func TestActivateOrgNoSessionNoop(t *testing.T) {
	s := &Server{cfg: sessionCfg(), memory: &fakeMemory{}}
	e := echo.New()
	c, rec := recentCtx("", "") // no session cookie at all
	s.activateOrg(c, "org-new")
	if ck := findCookie(rec, sessionCookieName); ck != nil {
		t.Errorf("no session: cookie should not be issued, got %+v", ck)
	}
	_ = e
}

func TestActivateOrgExpiredSessionNoop(t *testing.T) {
	s := &Server{cfg: sessionCfg(), memory: &fakeMemory{}}
	e := echo.New()
	claims := testSessionClaims()
	claims.ExpiresAt = time.Now().Add(-time.Hour).Unix() // expired
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	addSessionCookie(t, req, "test-secret", claims)
	rec := httptest.NewRecorder()
	s.activateOrg(e.NewContext(req, rec), "org-new")
	if ck := findCookie(rec, sessionCookieName); ck != nil {
		t.Errorf("expired session: cookie should not be re-issued, got %+v", ck)
	}
}

// --- clearContext ---

func TestClearContextClearsProjectAndOrg(t *testing.T) {
	s := &Server{cfg: sessionCfg(), memory: &fakeMemory{}}
	e := echo.New()

	claims := testSessionClaims()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	addSessionCookie(t, req, "test-secret", claims)
	rec := httptest.NewRecorder()
	s.clearContext(e.NewContext(req, rec))

	ck := findCookie(rec, sessionCookieName)
	if ck == nil {
		t.Fatal("session cookie not re-issued")
	}
	got, err := verifySession("test-secret", ck.Value, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got.OrgID != "" || got.ActiveProjectID != "" {
		t.Errorf("OrgID = %q, ActiveProjectID = %q, want both empty", got.OrgID, got.ActiveProjectID)
	}
	if got.AccessToken != claims.AccessToken || got.RefreshToken != claims.RefreshToken {
		t.Errorf("tokens not preserved: %+v", got)
	}
}

func TestClearContextNoSessionNoop(t *testing.T) {
	s := &Server{cfg: sessionCfg(), memory: &fakeMemory{}}
	e := echo.New()
	c, rec := recentCtx("", "")
	s.clearContext(c)
	if ck := findCookie(rec, sessionCookieName); ck != nil {
		t.Errorf("no session: cookie should not be issued, got %+v", ck)
	}
	_ = e
}
