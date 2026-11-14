package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// findCookies returns every Set-Cookie on the response with the given name, in
// header order. A correct session response carries exactly one: two values for
// the same name means the browser silently keeps the last one.
func findCookies(rec *httptest.ResponseRecorder, name string) []*http.Cookie {
	var out []*http.Cookie
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == name {
			out = append(out, ck)
		}
	}
	return out
}

// reissueClaims builds a session that is inside the 5-minute grace window, so
// requireSession refreshes it, and (with Rotate) whose refresh token is rotated
// by the token endpoint — the case where re-issuing pre-refresh claims would
// write an already-redeemed refresh token back into the browser (#610).
func reissueClaims(issuer string) sessionClaims {
	return sessionClaims{
		AccessToken:     "at-old",
		RefreshToken:    "rt-old",
		ActiveProjectID: "proj-old",
		OrgID:           "org-old",
		Name:            "Ada",
		Email:           "ada@example.com",
		Issuer:          issuer,
		// Inside the grace window: the middleware refreshes on this request.
		ExpiresAt: time.Now().Add(4 * time.Minute).Unix(),
	}
}

// oneSessionCookie asserts the response carries exactly one memory_session
// cookie and returns its verified claims.
func oneSessionCookie(t *testing.T, rec *httptest.ResponseRecorder, secret string) *sessionClaims {
	t.Helper()
	cookies := findCookies(rec, sessionCookieName)
	if len(cookies) != 1 {
		t.Fatalf("Set-Cookie %s count = %d, want exactly 1 (%v)", sessionCookieName, len(cookies), cookies)
	}
	claims, err := verifySession(secret, cookies[0].Value, time.Now())
	if err != nil {
		t.Fatalf("verify re-issued cookie: %v", err)
	}
	return claims
}

// TestActivateProjectAfterRefreshReissuesFromFreshClaims is the end-to-end
// regression for #610: a request that refreshes the session in the middleware
// AND switches project in the handler must produce exactly one memory_session
// Set-Cookie, carrying the POST-refresh tokens (and the switched scope).
//
// Before the fix the handler re-read the request cookie, so the response
// carried two Set-Cookie values; the browser kept the last (stale tokens with
// an already-redeemed, rotated refresh token) and the next request could not
// recover the session.
func TestActivateProjectAfterRefreshReissuesFromFreshClaims(t *testing.T) {
	seen := &refreshSeen{Rotate: true}
	z := newRefreshZitadel(t, seen)
	defer z.Close()

	f := &fakeMemory{
		orgsAndProjects: []OrgWithProjectsDto{
			{ID: "org-new", Name: "New Org", Projects: []ProjectAccessDto{{ID: "proj-new", Name: "New", OrgID: "org-new"}}},
		},
	}
	s := &Server{cfg: refreshCfg(z.URL), memory: f}
	e := sessionServer(s)

	req := httptest.NewRequest(http.MethodPost, "/api/projects/proj-new/activate", nil)
	addSessionCookie(t, req, "test-secret", reissueClaims(z.URL))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if seen.hits != 1 {
		t.Fatalf("token refresh hits = %d, want 1", seen.hits)
	}

	claims := oneSessionCookie(t, rec, "test-secret")
	if claims.AccessToken != "at-fresh" || claims.RefreshToken != "rt-rotated" {
		t.Errorf("re-issued tokens = (%q, %q), want the post-refresh pair (at-fresh, rt-rotated)",
			claims.AccessToken, claims.RefreshToken)
	}
	if claims.ActiveProjectID != "proj-new" || claims.OrgID != "org-new" {
		t.Errorf("scope = (%q, %q), want (proj-new, org-new)", claims.ActiveProjectID, claims.OrgID)
	}
	if claims.Name != "Ada" || claims.Email != "ada@example.com" {
		t.Errorf("identity not preserved: %+v", claims)
	}
}

// TestSessionReissueHelpersAfterRefreshWriteOneCookie covers every
// cookie-re-issuing helper behind requireSession: each must write exactly one
// memory_session cookie, from the post-refresh claims, whatever it does to the
// scope. It also pins the "last write wins" collapse for a handler that writes
// twice (uiRoot clears the context and then activates an org).
func TestSessionReissueHelpersAfterRefreshWriteOneCookie(t *testing.T) {
	type want struct{ project, org string }
	cases := []struct {
		name  string
		apply func(s *Server, c echo.Context)
		want  want
	}{
		{
			name:  "activateProjectInSession",
			apply: func(s *Server, c echo.Context) { s.activateProjectInSession(c, "proj-new", "org-new") },
			want:  want{project: "proj-new", org: "org-new"},
		},
		{
			name:  "activateOrg",
			apply: func(s *Server, c echo.Context) { s.activateOrg(c, "org-new") },
			want:  want{project: "", org: "org-new"},
		},
		{
			name:  "clearContext",
			apply: func(s *Server, c echo.Context) { s.clearContext(c) },
			want:  want{project: "", org: ""},
		},
		{
			// uiRoot's deleted-org path: clear, then fall through to the first
			// live org. Two writes, one cookie, the later value wins.
			name: "clearContextThenActivateOrg",
			apply: func(s *Server, c echo.Context) {
				s.clearContext(c)
				s.activateOrg(c, "org-new")
			},
			want: want{project: "", org: "org-new"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seen := &refreshSeen{Rotate: true}
			z := newRefreshZitadel(t, seen)
			defer z.Close()

			s := &Server{cfg: refreshCfg(z.URL), memory: &fakeMemory{}}
			e := echo.New()
			e.GET("/protected", func(c echo.Context) error {
				tc.apply(s, c)
				return c.String(http.StatusOK, "ok")
			}, s.requireSession)

			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			addSessionCookie(t, req, "test-secret", reissueClaims(z.URL))
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if seen.hits != 1 {
				t.Fatalf("token refresh hits = %d, want 1", seen.hits)
			}

			claims := oneSessionCookie(t, rec, "test-secret")
			if claims.AccessToken != "at-fresh" || claims.RefreshToken != "rt-rotated" {
				t.Errorf("re-issued tokens = (%q, %q), want the post-refresh pair", claims.AccessToken, claims.RefreshToken)
			}
			if claims.ActiveProjectID != tc.want.project || claims.OrgID != tc.want.org {
				t.Errorf("scope = (%q, %q), want (%q, %q)", claims.ActiveProjectID, claims.OrgID, tc.want.project, tc.want.org)
			}
		})
	}
}

// TestAvatarReissueAfterRefreshWritesOneCookie covers the profile-photo
// handlers, which carry an avatar override in the session cookie and are gated
// by requireSession like every other UI route (#610's org_members_ui sites).
func TestAvatarReissueAfterRefreshWritesOneCookie(t *testing.T) {
	t.Run("uiUploadAvatar", func(t *testing.T) {
		seen := &refreshSeen{Rotate: true}
		z := newRefreshZitadel(t, seen)
		defer z.Close()

		f := &fakeMemory{}
		s := &Server{cfg: refreshCfg(z.URL), memory: f}
		e := echo.New()
		e.POST("/profile/avatar", s.uiUploadAvatar, s.requireSession)

		body, ctype := multipartUploadBody(t, "face.png", "fake-image-bytes")
		req := httptest.NewRequest(http.MethodPost, "/profile/avatar", body)
		req.Header.Set("Content-Type", ctype)
		addSessionCookie(t, req, "test-secret", reissueClaims(z.URL))
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303 (%s)", rec.Code, rec.Body.String())
		}

		claims := oneSessionCookie(t, rec, "test-secret")
		if claims.AccessToken != "at-fresh" || claims.RefreshToken != "rt-rotated" {
			t.Errorf("re-issued tokens = (%q, %q), want the post-refresh pair", claims.AccessToken, claims.RefreshToken)
		}
		if claims.AvatarOverrideURL != "/api/user/avatar?v=key-new" {
			t.Errorf("AvatarOverrideURL = %q, want the uploaded avatar URL", claims.AvatarOverrideURL)
		}
	})

	t.Run("uiRemoveAvatar", func(t *testing.T) {
		seen := &refreshSeen{Rotate: true}
		z := newRefreshZitadel(t, seen)
		defer z.Close()

		f := &fakeMemory{}
		s := &Server{cfg: refreshCfg(z.URL), memory: f}
		e := echo.New()
		e.POST("/profile/avatar/remove", s.uiRemoveAvatar, s.requireSession)

		req := httptest.NewRequest(http.MethodPost, "/profile/avatar/remove", nil)
		claims := reissueClaims(z.URL)
		claims.AvatarOverrideURL = "/api/user/avatar?v=old"
		addSessionCookie(t, req, "test-secret", claims)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303 (%s)", rec.Code, rec.Body.String())
		}

		got := oneSessionCookie(t, rec, "test-secret")
		if got.AccessToken != "at-fresh" || got.RefreshToken != "rt-rotated" {
			t.Errorf("re-issued tokens = (%q, %q), want the post-refresh pair", got.AccessToken, got.RefreshToken)
		}
		if got.AvatarOverrideURL != "" {
			t.Errorf("AvatarOverrideURL = %q, want cleared", got.AvatarOverrideURL)
		}
	})
}

// TestReissueFallsBackToRequestCookieWithoutMiddlewareClaims pins the
// no-middleware path (dev/API-key mode, or a helper called outside the auth
// chain): with no validated claims in the context the helpers still re-issue
// from the request cookie, exactly as before #610.
func TestReissueFallsBackToRequestCookieWithoutMiddlewareClaims(t *testing.T) {
	s := &Server{cfg: sessionCfg(), memory: &fakeMemory{}}
	e := echo.New()
	e.POST("/switch", func(c echo.Context) error {
		s.activateProjectInSession(c, "proj-new", "org-new")
		return c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodPost, "/switch", nil)
	addSessionCookie(t, req, "test-secret", testSessionClaims()) // proj-sess / at-session / rt-session
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	claims := oneSessionCookie(t, rec, "test-secret")
	if claims.AccessToken != "at-session" || claims.RefreshToken != "rt-session" {
		t.Errorf("tokens = (%q, %q), want the cookie's own pair", claims.AccessToken, claims.RefreshToken)
	}
	if claims.ActiveProjectID != "proj-new" || claims.OrgID != "org-new" {
		t.Errorf("scope = (%q, %q), want (proj-new, org-new)", claims.ActiveProjectID, claims.OrgID)
	}
}

// TestSessionCookieWriteCollapsesDuplicatesDirectly pins the header helper
// itself: an earlier session cookie on the response is replaced, not appended,
// and unrelated cookies keep their place.
func TestSessionCookieWriteCollapsesDuplicatesDirectly(t *testing.T) {
	s := &Server{cfg: sessionCfg()}
	e := echo.New()
	rec := httptest.NewRecorder()
	c := e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), rec)

	s.setSessionCookie(c, sessionClaims{AccessToken: "at-1", ExpiresAt: time.Now().Add(time.Hour).Unix()})
	c.SetCookie(&http.Cookie{Name: "other", Value: "keep", Path: "/"})
	s.setSessionCookie(c, sessionClaims{AccessToken: "at-2", ExpiresAt: time.Now().Add(time.Hour).Unix()})

	cookies := findCookies(rec, sessionCookieName)
	if len(cookies) != 1 {
		t.Fatalf("Set-Cookie %s count = %d, want exactly 1", sessionCookieName, len(cookies))
	}
	claims, err := verifySession("test-secret", cookies[0].Value, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if claims.AccessToken != "at-2" {
		t.Errorf("AccessToken = %q, want the later value at-2", claims.AccessToken)
	}
	if ck := findCookie(rec, "other"); ck == nil || ck.Value != "keep" {
		t.Errorf("unrelated cookie lost: %+v", ck)
	}

	// A clear replaces the pending set: the response must not carry both.
	rec2 := httptest.NewRecorder()
	c2 := e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), rec2)
	s.setSessionCookie(c2, sessionClaims{AccessToken: "at-1", ExpiresAt: time.Now().Add(time.Hour).Unix()})
	s.clearSessionCookie(c2)
	cleared := findCookies(rec2, sessionCookieName)
	if len(cleared) != 1 {
		t.Fatalf("clear: Set-Cookie %s count = %d, want exactly 1", sessionCookieName, len(cleared))
	}
	if cleared[0].Value != "" || cleared[0].MaxAge >= 0 {
		t.Errorf("clear did not win: %+v", cleared[0])
	}
}
