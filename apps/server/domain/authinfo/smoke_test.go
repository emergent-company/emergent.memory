package authinfo_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/domain/authinfo"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

func routeSet(e *echo.Echo) map[string]bool {
	set := map[string]bool{}
	for _, r := range e.Routes() {
		set[r.Method+" "+r.Path] = true
	}
	return set
}

func TestRegisterRoutes(t *testing.T) {
	e := echo.New()
	h := authinfo.NewHandler(nil, &config.Config{})
	authinfo.RegisterRoutes(e, h, &auth.Middleware{})

	got := routeSet(e)
	for _, want := range []string{
		"GET /api/auth/issuer",
		"GET /api/auth/me",
	} {
		if !got[want] {
			t.Fatalf("route %q not registered; have %v", want, got)
		}
	}
}

func TestIssuerPublicNoAuth(t *testing.T) {
	e := echo.New()
	h := authinfo.NewHandler(nil, &config.Config{})

	req := httptest.NewRequest(http.MethodGet, "/api/auth/issuer", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := h.Issuer(c); err != nil {
		t.Fatalf("Issuer returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body authinfo.IssuerResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if body.Standalone {
		t.Fatal("standalone must be false for a zero config")
	}
}

func TestMeSessionPath(t *testing.T) {
	e := echo.New()
	h := authinfo.NewHandler(nil, &config.Config{})

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set(string(auth.UserContextKey), &auth.AuthUser{
		ID:        "u1",
		Email:     "a@b.c",
		ProjectID: "p1",
		OrgID:     "o1",
	})

	if err := h.Me(c); err != nil {
		t.Fatalf("Me returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body authinfo.TokenInfoResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if body.Type != "session" {
		t.Fatalf("type = %q, want session", body.Type)
	}
	if body.UserID != "u1" || body.ProjectID != "p1" || body.OrgID != "o1" {
		t.Fatalf("session context not propagated: %+v", body)
	}
}
