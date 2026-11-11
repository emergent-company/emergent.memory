package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func TestSessionIssuerStale(t *testing.T) {
	s := &Server{cfg: Config{ZitadelIssuer: "https://auth.memory.example/"}}
	cases := []struct {
		name         string
		cookieIssuer string
		want         bool
	}{
		{"matching issuer", "https://auth.memory.example", false},
		{"matching issuer with trailing slash", "https://auth.memory.example/", false},
		{"different issuer", "https://auth.old.example", true},
		{"legacy cookie without issuer", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := s.sessionIssuerStale(&sessionClaims{Issuer: tc.cookieIssuer}); got != tc.want {
				t.Fatalf("sessionIssuerStale(%q) = %v, want %v", tc.cookieIssuer, got, tc.want)
			}
		})
	}

	// An unset configured issuer disables the check entirely.
	if (&Server{}).sessionIssuerStale(&sessionClaims{Issuer: "https://anything"}) {
		t.Fatal("sessionIssuerStale should be false when ZITADEL_ISSUER is unset")
	}
}

// TestEnsureFreshSessionRefreshesOnIssuerChange proves a live session whose
// tokens were minted by a different issuer is refreshed before it is used — the
// condition that produced 401 invalid_token after the public-domain cutover.
func TestEnsureFreshSessionRefreshesOnIssuerChange(t *testing.T) {
	var gotGrant string
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"authorization_endpoint":%q,"token_endpoint":%q}`,
				ts.URL+"/authorize", ts.URL+"/token")
		case "/token":
			_ = r.ParseForm()
			gotGrant = r.Form.Get("grant_type")
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"access_token":"new-at","refresh_token":"new-rt","expires_in":3600,"token_type":"Bearer"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	s := &Server{cfg: Config{
		AuthMode:        "session",
		SessionSecret:   "test-secret",
		ZitadelIssuer:   ts.URL,
		ZitadelClientID: "cid",
	}}
	e := echo.New()
	e.Use(s.requireSession)
	e.GET("/p", func(c echo.Context) error {
		sc, ok := sessionContextFrom(c.Request().Context())
		if !ok {
			return c.String(http.StatusInternalServerError, "no session")
		}
		return c.String(http.StatusOK, sc.Token)
	})

	claims := sessionClaims{
		AccessToken:  "old-at",
		RefreshToken: "old-rt",
		Issuer:       "https://auth.old.example",
		ExpiresAt:    time.Now().Add(time.Hour).Unix(), // far from expiry: grace-window refresh would not fire
	}
	req := httptest.NewRequest(http.MethodGet, "/p", nil)
	addSessionCookie(t, req, "test-secret", claims)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
	if gotGrant != "refresh_token" {
		t.Fatalf("expected a refresh_token grant, got %q", gotGrant)
	}
	if body := rec.Body.String(); body != "new-at" {
		t.Fatalf("handler saw access token %q, want refreshed %q", body, "new-at")
	}
	ck := findCookie(rec, sessionCookieName)
	if ck == nil {
		t.Fatal("expected the session cookie to be re-issued after refresh")
	}
	refreshed, err := verifySession("test-secret", ck.Value, time.Now())
	if err != nil {
		t.Fatalf("re-issued cookie does not verify: %v", err)
	}
	if refreshed.AccessToken != "new-at" || refreshed.Issuer != ts.URL {
		t.Fatalf("re-issued claims = {token:%q issuer:%q}, want {token:%q issuer:%q}",
			refreshed.AccessToken, refreshed.Issuer, "new-at", ts.URL)
	}
}

// TestEnsureFreshSessionSkipsRefreshWhenIssuerMatches guards against a refresh
// storm: a session minted by the configured issuer is used as-is while it is
// outside the expiry grace window.
func TestEnsureFreshSessionSkipsRefreshWhenIssuerMatches(t *testing.T) {
	s := &Server{cfg: Config{
		AuthMode:      "session",
		SessionSecret: "test-secret",
		ZitadelIssuer: "https://auth.memory.example",
	}}
	e := echo.New()
	e.Use(s.requireSession)
	e.GET("/p", func(c echo.Context) error {
		sc, _ := sessionContextFrom(c.Request().Context())
		return c.String(http.StatusOK, sc.Token)
	})

	claims := sessionClaims{
		AccessToken:  "live-at",
		RefreshToken: "live-rt",
		Issuer:       "https://auth.memory.example",
		ExpiresAt:    time.Now().Add(time.Hour).Unix(),
	}
	req := httptest.NewRequest(http.MethodGet, "/p", nil)
	addSessionCookie(t, req, "test-secret", claims)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || rec.Body.String() != "live-at" {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
	if ck := findCookie(rec, sessionCookieName); ck != nil {
		t.Fatal("session cookie should not be re-issued when the issuer matches")
	}
}
