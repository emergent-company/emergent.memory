package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// TestRenderBlueprintsPageImportForm asserts the gallery renders the GitHub
// import surface as a modal — the dialog shell, the header trigger, the form
// action, the three fields, and the submit hook — while the existing install
// form action is still present. The import fields keep their original ids,
// names, and test hooks even though the form moved out of the page body.
func TestRenderBlueprintsPageImportForm(t *testing.T) {
	applied := []AppliedBlueprint{{BlueprintID: "bp1", Name: "personal-memory", Version: "1.0.0"}}
	available := []AvailableSchemaItem{{ID: "code-memory", Name: "code-memory", Version: "1.7.0", Source: "bundled"}}
	html := renderHTML(t, BlueprintsPage(applied, nil, available, nil, nil, "", nil))

	for _, want := range []string{
		"Import from GitHub",
		// The import surface is a modal, opened from a header action.
		`id="blueprint-import-modal"`,
		`data-dialog-open="blueprint-import-modal"`,
		`data-dialog-close="blueprint-import-modal"`,
		`data-testid="blueprint-import-open"`,
		`action="/blueprints/import"`,
		`name="url"`,
		`name="ref"`,
		`name="token"`,
		`type="password"`,
		`placeholder="https://github.com/org/repo"`,
		`data-testid="blueprint-import-url"`,
		`data-testid="blueprint-import-ref"`,
		`data-testid="blueprint-import-token"`,
		`data-testid="blueprint-import-submit"`,
		// The secondary page actions collapse into the options menu.
		`data-testid="blueprints-actions-menu"`,
		`data-testid="blueprints-migrations-link"`,
		`data-testid="blueprints-save-as-link"`,
		"Migrations",
		"Save as blueprint",
		// The pre-existing install path must survive unchanged.
		`action="/blueprints/install"`,
		`name="name"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("blueprints page missing %q", want)
		}
	}
}

// newImportRouteServer builds a bare server exposing only the import PRG route.
func newImportRouteServer(f *fakeMemory) (*Server, *echo.Echo) {
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.POST("/blueprints/import", s.uiImportGitHubBlueprint)
	return s, e
}

func importFormRequest(t *testing.T, values url.Values) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/blueprints/import", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

// TestUIRouteImportGitHubBlueprintSuccess verifies a successful import creates +
// publishes (via ImportBlueprint) and applies the returned blueprint, then
// redirects to the installed flash (303).
func TestUIRouteImportGitHubBlueprintSuccess(t *testing.T) {
	f := &fakeMemory{importBlueprintRes: &BlueprintRecord{ID: "bp-imported", Name: "repo", Status: "published"}}
	_, e := newImportRouteServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, importFormRequest(t, url.Values{
		"url":   {"https://github.com/org/repo"},
		"ref":   {"v1.2.3"},
		"token": {"ghp_secret"},
	}))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/blueprints?installed=1" {
		t.Errorf("location = %q, want /blueprints?installed=1", loc)
	}
	if f.importBlueprintReq == nil {
		t.Fatal("ImportBlueprint was not called")
	}
	if got := f.importBlueprintReq.URL; got != "https://github.com/org/repo" {
		t.Errorf("url = %q", got)
	}
	if got := f.importBlueprintReq.Ref; got != "v1.2.3" {
		t.Errorf("ref = %q", got)
	}
	if got := f.importBlueprintReq.Token; got != "ghp_secret" {
		t.Errorf("token = %q, want the forwarded credential", got)
	}
	if len(f.appliedBlueprint) != 1 || f.appliedBlueprint[0] != "bp-imported" {
		t.Fatalf("appliedBlueprint = %v, want [bp-imported]", f.appliedBlueprint)
	}
}

// TestUIRouteImportGitHubBlueprintEmptyURL verifies an empty URL short-circuits
// to the generic error flash without calling the backend.
func TestUIRouteImportGitHubBlueprintEmptyURL(t *testing.T) {
	f := &fakeMemory{}
	_, e := newImportRouteServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, importFormRequest(t, url.Values{}))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/blueprints?err=1" {
		t.Errorf("location = %q, want /blueprints?err=1", loc)
	}
	if f.importBlueprintReq != nil || len(f.appliedBlueprint) != 0 {
		t.Errorf("empty url must not call the backend: import=%+v applied=%v", f.importBlueprintReq, f.appliedBlueprint)
	}
}

// TestUIRouteImportGitHubBlueprintFailure verifies an import failure redirects
// with the mapped error and installs nothing. The private token must not leak
// into the redirect target.
func TestUIRouteImportGitHubBlueprintFailure(t *testing.T) {
	f := &fakeMemory{importBlueprintErr: errors.New("memory 502 fetch failure")}
	_, e := newImportRouteServer(f)

	const token = "ghp_secret"
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, importFormRequest(t, url.Values{
		"url":   {"https://github.com/org/repo"},
		"token": {token},
	}))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/blueprints?err=") {
		t.Fatalf("location = %q, want /blueprints?err=…", loc)
	}
	if !strings.Contains(loc, "fetch+failure") && !strings.Contains(loc, "fetch%20failure") {
		t.Errorf("location = %q, want the mapped error text", loc)
	}
	if strings.Contains(loc, token) {
		t.Errorf("location leaks the token: %q", loc)
	}
	if len(f.appliedBlueprint) != 0 {
		t.Errorf("failed import must not apply, applied = %v", f.appliedBlueprint)
	}
}

// TestImportBlueprintClientPostsRequest verifies the client posts the expected
// JSON body to /api/blueprints/import and decodes the created record.
func TestImportBlueprintClientPostsRequest(t *testing.T) {
	var gotMethod, gotPath, gotBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"bp-9","name":"repo","version":"1.0.0","status":"published"}`))
	}))
	defer ts.Close()

	m := NewMemoryClient(ts.URL, "proj")
	const token = "ghp_secret"
	rec, err := m.ImportBlueprint(t.Context(), "https://github.com/org/repo", "v1.2.3", token)
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/blueprints/import" {
		t.Errorf("request = %s %s, want POST /api/blueprints/import", gotMethod, gotPath)
	}
	var sent importBlueprintRequest
	if err := json.Unmarshal([]byte(gotBody), &sent); err != nil {
		t.Fatalf("request body not JSON: %v (%s)", err, gotBody)
	}
	if sent.URL != "https://github.com/org/repo" || sent.Ref != "v1.2.3" || sent.Token != token {
		t.Errorf("sent = %+v", sent)
	}
	if rec == nil || rec.ID != "bp-9" || rec.Status != "published" {
		t.Fatalf("record = %+v", rec)
	}
}

// TestImportBlueprintClientErrorOmitsToken verifies an upstream auth failure is
// surfaced without the private token in the error text.
func TestImportBlueprintClientErrorOmitsToken(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"unauthorized","message":"unauthorized"}}`))
	}))
	defer ts.Close()

	m := NewMemoryClient(ts.URL, "proj")
	const token = "ghp_secret"
	_, err := m.ImportBlueprint(t.Context(), "https://github.com/org/repo", "", token)
	if err == nil {
		t.Fatal("want error for 401, got nil")
	}
	if strings.Contains(err.Error(), token) {
		t.Errorf("error leaks the token: %v", err)
	}
	if !isMemoryStatus(err, http.StatusUnauthorized) {
		t.Errorf("error status = %d, want 401: %v", memoryStatus(err), err)
	}
}
