package invites

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// newAcceptLinkContext builds the echo context AcceptViaLink expects, with no
// authenticated user (a browser arriving from an email link).
func newAcceptLinkContext(token string) (*echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/invites/accept?token="+token, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	return &c, rec
}

// TestAcceptViaLinkRedirectsBrowserToWebUI is the regression guard for the
// dead invite link: an unauthenticated browser must be handed to the web UI's
// accept page (which owns the login redirect), never to Zitadel's retired
// legacy /ui/login.
func TestAcceptViaLinkRedirectsBrowserToWebUI(t *testing.T) {
	c, rec := newAcceptLinkContext("tok-123")
	cfg := &config.Config{
		AppURL:   "https://memory.example.com",
		WebUIURL: "https://app.example.com",
	}
	h := NewHandler(nil, cfg, nil, nil, nil)

	if err := h.AcceptViaLink(*c); err != nil {
		t.Fatalf("AcceptViaLink: %v", err)
	}

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	want := "https://app.example.com/invites/accept?token=tok-123"
	if got := rec.Header().Get("Location"); got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}
	if strings.Contains(rec.Header().Get("Location"), "/ui/login") {
		t.Errorf("Location must not point at the retired Zitadel login page")
	}
}

// TestAcceptViaLinkRefusesWithoutWebUIHost proves the handler never redirects
// to the API host it is already serving: without a distinct WEB_UI_URL the
// redirect would loop forever, so it answers 503 instead.
func TestAcceptViaLinkRefusesWithoutWebUIHost(t *testing.T) {
	c, _ := newAcceptLinkContext("tok-123")
	cfg := &config.Config{AppURL: "https://memory.example.com"}
	h := NewHandler(nil, cfg, nil, nil, nil)

	err := h.AcceptViaLink(*c)
	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("AcceptViaLink error = %v, want *apperror.Error", err)
	}
	if appErr.HTTPStatus != http.StatusServiceUnavailable {
		t.Errorf("HTTPStatus = %d, want %d", appErr.HTTPStatus, http.StatusServiceUnavailable)
	}
}

// TestAcceptViaLinkRequiresToken covers the missing-parameter case.
func TestAcceptViaLinkRequiresToken(t *testing.T) {
	c, _ := newAcceptLinkContext("")
	cfg := &config.Config{AppURL: "https://memory.example.com", WebUIURL: "https://app.example.com"}
	h := NewHandler(nil, cfg, nil, nil, nil)

	err := h.AcceptViaLink(*c)
	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("AcceptViaLink error = %v, want *apperror.Error", err)
	}
	if appErr.HTTPStatus != http.StatusBadRequest {
		t.Errorf("HTTPStatus = %d, want %d", appErr.HTTPStatus, http.StatusBadRequest)
	}
}

// TestFrontendURLFallsBackToAppURL pins the config contract the invite URLs
// rely on.
func TestFrontendURLFallsBackToAppURL(t *testing.T) {
	cfg := &config.Config{AppURL: "https://memory.example.com/"}
	if got := cfg.FrontendURL(); got != "https://memory.example.com" {
		t.Errorf("FrontendURL() = %q, want trailing-slash-free AppURL", got)
	}
	cfg.WebUIURL = "https://app.example.com/"
	if got := cfg.FrontendURL(); got != "https://app.example.com" {
		t.Errorf("FrontendURL() = %q, want trailing-slash-free WebUIURL", got)
	}
}
