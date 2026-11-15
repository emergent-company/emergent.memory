package market

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

type fakeInstallService struct {
	resolveProject string
	resolveMarket  string
	resolveCode    string
	resolveResult  *ResolveResult
	resolveErr     error

	applyProject   string
	applyUser      string
	applyMarket    string
	applyCode      string
	applyOverrides map[string]string
	applyResult    *ApplyResult
	applyErr       error
}

func (f *fakeInstallService) Resolve(_ context.Context, projectID, marketIssuer, code string) (*ResolveResult, error) {
	f.resolveProject = projectID
	f.resolveMarket = marketIssuer
	f.resolveCode = code
	return f.resolveResult, f.resolveErr
}

func (f *fakeInstallService) Apply(_ context.Context, projectID, installedBy, marketIssuer, code string, overrides map[string]string) (*ApplyResult, error) {
	f.applyProject = projectID
	f.applyUser = installedBy
	f.applyMarket = marketIssuer
	f.applyCode = code
	f.applyOverrides = overrides
	return f.applyResult, f.applyErr
}

func handlerContext(t *testing.T, method, path, body string, user *auth.AuthUser) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if user != nil {
		c.Set(string(auth.UserContextKey), user)
	}
	return c, rec
}

func TestHandlerResolveUsesAuthenticatedProject(t *testing.T) {
	fake := &fakeInstallService{resolveResult: &ResolveResult{Listing: ListingPreview{Slug: "demo"}}}
	h := NewHandler(fake, nil)
	c, rec := handlerContext(t, http.MethodPost, "/api/market/installs/resolve", `{"market":"https://market.example","code":"c1"}`,
		&auth.AuthUser{ID: "user-1", ProjectID: "proj-1"})
	if err := h.Resolve(c); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if fake.resolveProject != "proj-1" || fake.resolveMarket != "https://market.example" || fake.resolveCode != "c1" {
		t.Fatalf("service args = %q %q %q", fake.resolveProject, fake.resolveMarket, fake.resolveCode)
	}
	var body ResolveResult
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Listing.Slug != "demo" {
		t.Fatalf("body = %+v", body)
	}
}

func TestHandlerResolveRequiresProjectContext(t *testing.T) {
	h := NewHandler(&fakeInstallService{}, nil)
	c, _ := handlerContext(t, http.MethodPost, "/api/market/installs/resolve", `{"market":"https://market.example","code":"c1"}`,
		&auth.AuthUser{ID: "user-1"})
	err := h.Resolve(c)
	if ErrorCode(err) != "bad_request" {
		t.Fatalf("err = %v, want bad_request", err)
	}
}

func TestHandlerApplyForwardsProjectAndOverrides(t *testing.T) {
	fake := &fakeInstallService{applyResult: &ApplyResult{BlueprintID: "bp-1", InstallID: "inst-1"}}
	h := NewHandler(fake, nil)
	c, rec := handlerContext(t, http.MethodPost, "/api/market/installs/apply",
		`{"market":"https://market.example","code":"c1","project_id":"proj-9","model_overrides":{"agent":"openai/x"}}`,
		&auth.AuthUser{ID: "user-1", ProjectID: "proj-1"})
	if err := h.Apply(c); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if fake.applyProject != "proj-9" {
		t.Fatalf("project = %q, want proj-9", fake.applyProject)
	}
	if fake.applyUser != "user-1" {
		t.Fatalf("installedBy = %q", fake.applyUser)
	}
	if fake.applyOverrides["agent"] != "openai/x" {
		t.Fatalf("overrides = %+v", fake.applyOverrides)
	}
}

func TestHandlerApplyFallsBackToAuthenticatedProject(t *testing.T) {
	fake := &fakeInstallService{applyResult: &ApplyResult{}}
	h := NewHandler(fake, nil)
	c, _ := handlerContext(t, http.MethodPost, "/api/market/installs/apply",
		`{"market":"https://market.example","code":"c1"}`, &auth.AuthUser{ID: "user-1", ProjectID: "proj-1"})
	if err := h.Apply(c); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if fake.applyProject != "proj-1" {
		t.Fatalf("project = %q, want proj-1", fake.applyProject)
	}
}

func TestHandlerApplyRequiresProject(t *testing.T) {
	h := NewHandler(&fakeInstallService{}, nil)
	c, _ := handlerContext(t, http.MethodPost, "/api/market/installs/apply",
		`{"market":"https://market.example","code":"c1"}`, &auth.AuthUser{ID: "user-1"})
	if err := h.Apply(c); ErrorCode(err) != "bad_request" {
		t.Fatalf("err = %v, want bad_request", err)
	}
}

func TestHandlerRejectsMissingCode(t *testing.T) {
	h := NewHandler(&fakeInstallService{}, nil)
	c, _ := handlerContext(t, http.MethodPost, "/api/market/installs/resolve", `{"market":"https://market.example"}`,
		&auth.AuthUser{ID: "user-1", ProjectID: "proj-1"})
	if err := h.Resolve(c); ErrorCode(err) != "bad_request" {
		t.Fatalf("err = %v, want bad_request", err)
	}
}
