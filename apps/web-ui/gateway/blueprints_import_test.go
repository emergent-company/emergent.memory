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
	available := []AvailableSchemaItem{{ID: "code-memory", Name: "code-memory", Version: "1.7.0", Source: "registry"}}
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
		`name="schemaId"`,
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
	e.POST("/blueprints/import/confirm", s.uiImportGitHubBlueprintConfirm)
	return s, e
}

func importFormRequest(t *testing.T, values url.Values) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/blueprints/import", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

// TestUIRouteImportGitHubBlueprintSuccess verifies a preview that resolves
// cleanly (no unresolved models) imports + applies in one step (via
// ImportAndApplyBlueprint with nil overrides) and redirects to the installed
// flash (303).
func TestUIRouteImportGitHubBlueprintSuccess(t *testing.T) {
	f := &fakeMemory{
		previewImportBlueprintRes: &BlueprintPreview{
			Name:     "repo",
			Manifest: json.RawMessage(`{"agents":[]}`),
		},
	}
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
	if f.importAndApplyCalls != 1 {
		t.Fatalf("ImportAndApplyBlueprint calls = %d, want 1", f.importAndApplyCalls)
	}
	if f.importAndApplyReq == nil {
		t.Fatal("ImportAndApplyBlueprint was not called")
	}
	if got := f.importAndApplyReq.URL; got != "https://github.com/org/repo" {
		t.Errorf("url = %q", got)
	}
	if got := f.importAndApplyReq.Ref; got != "v1.2.3" {
		t.Errorf("ref = %q", got)
	}
	if got := f.importAndApplyReq.Token; got != "ghp_secret" {
		t.Errorf("token = %q, want the forwarded credential", got)
	}
	if !f.importAndApplyReq.Apply {
		t.Errorf("apply = false, want true")
	}
	if f.importAndApplyReq.ModelOverrides != nil {
		t.Errorf("modelOverrides = %v, want nil", f.importAndApplyReq.ModelOverrides)
	}
	if len(f.appliedBlueprint) != 0 {
		t.Errorf("import+apply path must not call ApplyBlueprint, applied = %v", f.appliedBlueprint)
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

// TestUIRouteImportGitHubBlueprintUnresolvedModel verifies a preview carrying
// an agent whose pinned model can't be resolved renders the model-mapping page
// (one select per unresolved agent) and does NOT import.
func TestUIRouteImportGitHubBlueprintUnresolvedModel(t *testing.T) {
	f := &fakeMemory{
		previewImportBlueprintRes: &BlueprintPreview{
			Name:     "repo",
			Manifest: json.RawMessage(`{"agents":[{"name":"writer","model":{"name":"gemini-2.5-flash"}}]}`),
		},
	}
	_, e := newImportRouteServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, importFormRequest(t, url.Values{
		"url":   {"https://github.com/org/repo"},
		"token": {"ghp_secret"},
	}))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-testid="blueprint-import-map"`,
		`data-testid="blueprint-model-map-row"`,
		`name="model.writer"`,
		`data-testid="blueprint-model-map-select"`,
		// The private-repo token must be carried through the mapping step.
		`name="token"`,
		`value="ghp_secret"`,
		"Project default",
		"Set a provider for this model (expected provider/model).",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("map page missing %q", want)
		}
	}
	if f.importAndApplyCalls != 0 {
		t.Errorf("ImportAndApplyBlueprint called %d times, want 0", f.importAndApplyCalls)
	}
	if len(f.appliedBlueprint) != 0 {
		t.Errorf("unresolved import must not apply, applied = %v", f.appliedBlueprint)
	}
}

// TestUIRouteImportGitHubBlueprintUnconfiguredProvider verifies a preview with a
// prefixed model whose provider isn't configured is flagged with the provider
// issue and, like the bare-model case, does not import.
func TestUIRouteImportGitHubBlueprintUnconfiguredProvider(t *testing.T) {
	f := &fakeMemory{
		previewImportBlueprintRes: &BlueprintPreview{
			Name:     "repo",
			Manifest: json.RawMessage(`{"agents":[{"name":"writer","model":{"name":"anthropic/claude-3"}}]}`),
		},
	}
	_, e := newImportRouteServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, importFormRequest(t, url.Values{"url": {"https://github.com/org/repo"}}))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "anthropic") || !strings.Contains(body, "is not configured in this project.") {
		t.Errorf("map page missing provider issue: %s", body)
	}
	if f.importAndApplyCalls != 0 {
		t.Errorf("ImportAndApplyBlueprint called %d times, want 0", f.importAndApplyCalls)
	}
}

// TestUIRouteImportGitHubBlueprintConfirm verifies the confirm handler parses
// the per-agent `model.<agentName>` form fields and passes the chosen overrides
// (empty value → project default) to ImportAndApplyBlueprint, then redirects.
func TestUIRouteImportGitHubBlueprintConfirm(t *testing.T) {
	f := &fakeMemory{}
	_, e := newImportRouteServer(f)

	values := url.Values{
		"url":          {"https://github.com/org/repo"},
		"ref":          {"v1.2.3"},
		"token":        {"ghp_secret"},
		"model.writer": {"openai/gpt-4o"},
		"model.editor": {""},
	}
	req := httptest.NewRequest(http.MethodPost, "/blueprints/import/confirm", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/blueprints?installed=1" {
		t.Errorf("location = %q, want /blueprints?installed=1", loc)
	}
	if f.importAndApplyCalls != 1 {
		t.Fatalf("ImportAndApplyBlueprint calls = %d, want 1", f.importAndApplyCalls)
	}
	if f.importAndApplyReq == nil {
		t.Fatal("ImportAndApplyBlueprint was not called")
	}
	if got := f.importAndApplyReq.Token; got != "ghp_secret" {
		t.Errorf("token = %q, want the forwarded credential", got)
	}
	want := map[string]string{"writer": "openai/gpt-4o", "editor": ""}
	if len(f.importAndApplyReq.ModelOverrides) != len(want) {
		t.Fatalf("modelOverrides = %v, want %v", f.importAndApplyReq.ModelOverrides, want)
	}
	for k, v := range want {
		if got := f.importAndApplyReq.ModelOverrides[k]; got != v {
			t.Errorf("modelOverrides[%q] = %q, want %q", k, got, v)
		}
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

// TestPreviewImportBlueprintClientPostsRequest verifies the preview client posts
// to /api/blueprints/import/preview and decodes the metadata + raw manifest.
func TestPreviewImportBlueprintClientPostsRequest(t *testing.T) {
	var gotMethod, gotPath, gotBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"repo","version":"1.0.0","description":"d","author":"a","manifest":{"agents":[]}}`))
	}))
	defer ts.Close()

	m := NewMemoryClient(ts.URL, "proj")
	prev, err := m.PreviewImportBlueprint(t.Context(), "https://github.com/org/repo", "v1.2.3", "")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/blueprints/import/preview" {
		t.Errorf("request = %s %s, want POST /api/blueprints/import/preview", gotMethod, gotPath)
	}
	var sent importBlueprintRequest
	if err := json.Unmarshal([]byte(gotBody), &sent); err != nil {
		t.Fatalf("request body not JSON: %v (%s)", err, gotBody)
	}
	if sent.URL != "https://github.com/org/repo" || sent.Ref != "v1.2.3" {
		t.Errorf("sent = %+v", sent)
	}
	if sent.Apply || sent.ModelOverrides != nil {
		t.Errorf("preview must not carry apply/modelOverrides: %+v", sent)
	}
	if prev == nil || prev.Name != "repo" || prev.Version != "1.0.0" || len(prev.Manifest) == 0 {
		t.Fatalf("preview = %+v", prev)
	}
}

// TestImportAndApplyBlueprintClientPostsRequest verifies the import+apply client
// posts apply:true plus the modelOverrides map to /api/blueprints/import.
func TestImportAndApplyBlueprintClientPostsRequest(t *testing.T) {
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
	rec, err := m.ImportAndApplyBlueprint(t.Context(), "https://github.com/org/repo", "v1.2.3", "", map[string]string{"writer": "openai/gpt-4o"})
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
	if !sent.Apply {
		t.Errorf("apply = false, want true")
	}
	if got := sent.ModelOverrides["writer"]; got != "openai/gpt-4o" {
		t.Errorf("modelOverrides[writer] = %q, want openai/gpt-4o", got)
	}
	if rec == nil || rec.ID != "bp-9" {
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
