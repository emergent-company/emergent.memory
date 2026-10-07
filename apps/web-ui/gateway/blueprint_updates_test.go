package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// --- MemoryClient update-check methods ---

// TestGetBlueprintUpdatesClient verifies the client GETs the per-blueprint
// update status, scopes the request with X-Project-ID, and decodes the record.
func TestGetBlueprintUpdatesClient(t *testing.T) {
	var gotMethod, gotPath, gotProject string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotProject = r.Method, r.URL.Path, r.Header.Get("X-Project-ID")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"blueprintName":"vendor-pack","sourceUrl":"https://github.com/org/repo","sourceRef":"main","latestVersion":"2.0.0","updateAvailable":true,"lastCheckedAt":"2026-10-07T09:00:00Z"}`))
	}))
	defer ts.Close()

	m := NewMemoryClient(ts.URL, "proj")
	check, err := m.GetBlueprintUpdates(t.Context(), "vendor-pack")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodGet || gotPath != "/api/blueprints/vendor-pack/updates" {
		t.Errorf("request = %s %s, want GET /api/blueprints/vendor-pack/updates", gotMethod, gotPath)
	}
	if gotProject != "proj" {
		t.Errorf("X-Project-ID = %q, want proj", gotProject)
	}
	if check == nil || !check.UpdateAvailable || check.LatestVersion != "2.0.0" || check.LastCheckedAt == "" {
		t.Fatalf("check = %+v", check)
	}
}

// TestCheckBlueprintUpdatesClient verifies the client POSTs to the check
// endpoint (empty JSON object body, not an empty body) and decodes the queued
// job.
func TestCheckBlueprintUpdatesClient(t *testing.T) {
	var gotMethod, gotPath, gotBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"id":"job-1","action":"check","status":"pending"}`))
	}))
	defer ts.Close()

	m := NewMemoryClient(ts.URL, "proj")
	job, err := m.CheckBlueprintUpdates(t.Context(), "vendor-pack")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/blueprints/vendor-pack/updates/check" {
		t.Errorf("request = %s %s, want POST /api/blueprints/vendor-pack/updates/check", gotMethod, gotPath)
	}
	if strings.TrimSpace(gotBody) != "{}" {
		t.Errorf("body = %q, want an empty JSON object", gotBody)
	}
	if job == nil || job.Status != "pending" {
		t.Fatalf("job = %+v", job)
	}
}

// TestSetGithubCredentialClient verifies the client PUTs the token as JSON and
// treats the 204 No Content response as a success (no body to decode).
func TestSetGithubCredentialClient(t *testing.T) {
	var gotMethod, gotPath, gotBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	m := NewMemoryClient(ts.URL, "proj")
	if err := m.SetGithubCredential(t.Context(), "ghp_secret"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPut || gotPath != "/api/blueprints/github-credentials" {
		t.Errorf("request = %s %s, want PUT /api/blueprints/github-credentials", gotMethod, gotPath)
	}
	var sent setGithubCredentialRequest
	if err := json.Unmarshal([]byte(gotBody), &sent); err != nil {
		t.Fatalf("body not JSON: %v (%s)", err, gotBody)
	}
	if sent.Token != "ghp_secret" {
		t.Errorf("sent token = %q", sent.Token)
	}
}

// --- UI routes ---

func newUpdateRouteServer(f *fakeMemory) (*Server, *echo.Echo) {
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.POST("/blueprints/github-credentials", s.uiSaveGithubCredential)
	e.POST("/blueprints/:name/updates/check", s.uiCheckBlueprintUpdates)
	e.POST("/blueprints/:name/updates/apply", s.uiApplyBlueprintUpdate)
	return s, e
}

func updateFormRequest(t *testing.T, path string, values url.Values) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

// TestUIRouteCheckBlueprintUpdates verifies the check action enqueues a check
// for the named blueprint and redirects with the queued flash (PRG).
func TestUIRouteCheckBlueprintUpdates(t *testing.T) {
	f := &fakeMemory{}
	_, e := newUpdateRouteServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, updateFormRequest(t, "/blueprints/vendor-pack/updates/check", nil))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/blueprints?checkQueued=1" {
		t.Errorf("location = %q, want /blueprints?checkQueued=1", loc)
	}
	if len(f.checkedUpdates) != 1 || f.checkedUpdates[0] != "vendor-pack" {
		t.Fatalf("checkedUpdates = %v, want [vendor-pack]", f.checkedUpdates)
	}
}

// TestUIRouteCheckBlueprintUpdatesFailure verifies an enqueue failure redirects
// with the mapped error instead of the queued flash.
func TestUIRouteCheckBlueprintUpdatesFailure(t *testing.T) {
	f := &fakeMemory{blueprintErr: errTest}
	_, e := newUpdateRouteServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, updateFormRequest(t, "/blueprints/vendor-pack/updates/check", nil))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/blueprints?err=") {
		t.Fatalf("location = %q, want /blueprints?err=…", loc)
	}
}

// TestUIRouteSaveGithubCredential verifies the token form stores the token and
// redirects with the saved flash, and that the token never appears in the
// redirect target.
func TestUIRouteSaveGithubCredential(t *testing.T) {
	f := &fakeMemory{}
	_, e := newUpdateRouteServer(f)

	const token = "ghp_secret"
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, updateFormRequest(t, "/blueprints/github-credentials", url.Values{"token": {token}}))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if loc != "/blueprints?ghCredentialSaved=1" {
		t.Errorf("location = %q, want /blueprints?ghCredentialSaved=1", loc)
	}
	if strings.Contains(loc, token) {
		t.Errorf("location leaks the token: %q", loc)
	}
	if f.savedGithubToken == nil || *f.savedGithubToken != token {
		t.Fatalf("savedGithubToken = %v, want %q", f.savedGithubToken, token)
	}
}

// TestUIRouteSaveGithubCredentialEmpty verifies an empty token short-circuits to
// the generic error flash without calling the backend.
func TestUIRouteSaveGithubCredentialEmpty(t *testing.T) {
	f := &fakeMemory{}
	_, e := newUpdateRouteServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, updateFormRequest(t, "/blueprints/github-credentials", url.Values{"token": {"   "}}))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/blueprints?err=1" {
		t.Errorf("location = %q, want /blueprints?err=1", loc)
	}
	if f.savedGithubToken != nil {
		t.Errorf("empty token must not reach the backend, got %q", *f.savedGithubToken)
	}
}

// --- render contracts ---

// TestRenderAppliedRowRepoUpdate asserts the installed row for a GitHub-imported
// blueprint shows its upstream repo/ref and, when a newer version is detected,
// the "vX → vY" update badge plus a live Update action that posts to the async
// apply endpoint (alongside the Check action and Remove).
func TestRenderAppliedRowRepoUpdate(t *testing.T) {
	b := AppliedBlueprint{
		BlueprintID:     "bp-repo",
		Name:            "vendor-pack",
		Version:         "1.0.0",
		AppliedAt:       "2026-08-26T10:00:00Z",
		SourceURL:       "https://github.com/acme/vendor-pack",
		SourceRef:       "main",
		UpdateAvailable: true,
		LatestVersion:   "2.0.0",
		LastCheckedAt:   "2026-10-07T09:00:00Z",
	}

	html := renderHTML(t, appliedRow(b, AvailableSchemaItem{}))
	for _, want := range []string{
		`data-testid="blueprint-source-link"`,
		`href="https://github.com/acme/vendor-pack"`,
		"github.com/acme/vendor-pack",
		"main",
		"Update available: v1.0.0 → v2.0.0",
		`data-testid="blueprint-update-vendor-pack"`,
		`action="/blueprints/vendor-pack/updates/apply"`,
		`x-data="{ updating: false }"`,
		`@submit="updating = true"`,
		`:disabled="updating"`,
		"Updating…",
		`data-testid="blueprint-check-updates-vendor-pack"`,
		`action="/blueprints/vendor-pack/updates/check"`,
		"Check for updates",
		// The check action keeps its own client-side pending state.
		`x-data="{ checking: false }"`,
		`@submit="checking = true"`,
		`:disabled="checking"`,
		"Checking…",
		"Remove",
		"checked",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("repo update row missing %q", want)
		}
	}
}

// jobWithResult builds a latest-job pointer with a JSON result payload.
func jobWithResult(status, action, result string) *BlueprintUpdateJob {
	j := &BlueprintUpdateJob{ID: "job-9", Action: action, Status: status}
	if result != "" {
		j.Result = json.RawMessage(result)
	}
	return j
}

// repoBlueprintWithJob returns a repo-imported applied blueprint carrying an
// available update and the given latest update job.
func repoBlueprintWithJob(job *BlueprintUpdateJob) AppliedBlueprint {
	return AppliedBlueprint{
		BlueprintID:     "bp-repo",
		Name:            "vendor-pack",
		Version:         "1.0.0",
		AppliedAt:       "2026-08-26T10:00:00Z",
		SourceURL:       "https://github.com/acme/vendor-pack",
		SourceRef:       "main",
		UpdateAvailable: true,
		LatestVersion:   "2.0.0",
		LastCheckedAt:   "2026-10-07T09:00:00Z",
		LatestJob:       job,
	}
}

// TestRenderAppliedRowRepoUpdatePending asserts a pending update job renders a
// disabled queued control (no second apply form) while the badge stays.
func TestRenderAppliedRowRepoUpdatePending(t *testing.T) {
	html := renderHTML(t, appliedRow(repoBlueprintWithJob(jobWithResult("pending", "update", "")), AvailableSchemaItem{}))
	if !strings.Contains(html, `data-testid="blueprint-update-pending-vendor-pack"`) {
		t.Error("pending job must render the queued control")
	}
	if !strings.Contains(html, "Queued…") {
		t.Error("pending job must label the control Queued…")
	}
	if !strings.Contains(html, "disabled") {
		t.Error("pending control must be disabled")
	}
	if strings.Contains(html, `data-testid="blueprint-update-vendor-pack"`) {
		t.Error("pending job must not render a second live Update form")
	}
	if !strings.Contains(html, "Update available: v1.0.0 → v2.0.0") {
		t.Error("pending job must keep the update badge")
	}
}

// TestRenderAppliedRowRepoUpdateRunning asserts a running update job renders the
// disabled running control.
func TestRenderAppliedRowRepoUpdateRunning(t *testing.T) {
	html := renderHTML(t, appliedRow(repoBlueprintWithJob(jobWithResult("running", "update", "")), AvailableSchemaItem{}))
	if !strings.Contains(html, `data-testid="blueprint-update-running-vendor-pack"`) {
		t.Error("running job must render the running control")
	}
	if !strings.Contains(html, "Updating…") {
		t.Error("running job must label the control Updating…")
	}
	if !strings.Contains(html, "disabled") {
		t.Error("running control must be disabled")
	}
	if strings.Contains(html, `data-testid="blueprint-update-vendor-pack"`) {
		t.Error("running job must not render a live Update form")
	}
}

// TestRenderAppliedRowRepoUpdateSucceeded asserts a succeeded update job shows
// success and clears the available-update badge.
func TestRenderAppliedRowRepoUpdateSucceeded(t *testing.T) {
	html := renderHTML(t, appliedRow(repoBlueprintWithJob(jobWithResult("succeeded", "update", `{"noop":false,"applied":true,"version":"2.0.0"}`)), AvailableSchemaItem{}))
	if !strings.Contains(html, `data-testid="blueprint-update-succeeded-vendor-pack"`) {
		t.Error("succeeded job must render the success state")
	}
	if !strings.Contains(html, "Updated") {
		t.Error("succeeded job must read Updated")
	}
	if strings.Contains(html, "Update available:") {
		t.Error("succeeded job must clear the update badge")
	}
	if strings.Contains(html, `data-testid="blueprint-update-vendor-pack"`) {
		t.Error("succeeded job must not render a live Update form")
	}
}

// TestRenderAppliedRowRepoUpdateSucceededNoop asserts a succeeded no-op update
// job (upstream already current) reads "Already up to date".
func TestRenderAppliedRowRepoUpdateSucceededNoop(t *testing.T) {
	html := renderHTML(t, appliedRow(repoBlueprintWithJob(jobWithResult("succeeded", "update", `{"noop":true}`)), AvailableSchemaItem{}))
	if !strings.Contains(html, "Already up to date") {
		t.Error("no-op succeeded job must read Already up to date")
	}
	if strings.Contains(html, "Update available:") {
		t.Error("no-op succeeded job must clear the update badge")
	}
}

// TestRenderAppliedRowRepoUpdateFailed asserts a failed update job shows the
// error and offers a retry (live Update form).
func TestRenderAppliedRowRepoUpdateFailed(t *testing.T) {
	job := jobWithResult("failed", "update", "")
	job.LastError = "fetch upstream: boom"
	html := renderHTML(t, appliedRow(repoBlueprintWithJob(job), AvailableSchemaItem{}))
	if !strings.Contains(html, `data-testid="blueprint-update-error-vendor-pack"`) {
		t.Error("failed job must render the error state")
	}
	if !strings.Contains(html, "Update failed: fetch upstream: boom") {
		t.Errorf("failed job must show the error text, got:\n%s", html)
	}
	if !strings.Contains(html, `data-testid="blueprint-update-vendor-pack"`) {
		t.Error("failed job must offer a retry Update form")
	}
	if !strings.Contains(html, `title="fetch upstream: boom"`) {
		t.Error("failed job must expose the full error in the title")
	}
}

// TestRenderAppliedRowRepoCheckJobDoesNotDriveUpdate asserts a latest CHECK job
// (e.g. still pending) never drives the Update control.
func TestRenderAppliedRowRepoCheckJobDoesNotDriveUpdate(t *testing.T) {
	html := renderHTML(t, appliedRow(repoBlueprintWithJob(jobWithResult("pending", "check", "")), AvailableSchemaItem{}))
	if strings.Contains(html, `data-testid="blueprint-update-pending-vendor-pack"`) {
		t.Error("a pending check job must not render the update-pending control")
	}
	if !strings.Contains(html, `data-testid="blueprint-update-vendor-pack"`) {
		t.Error("with only a check job, the live Update form must still render")
	}
}

// TestRenderAppliedRowRepoNoUpdate asserts a repo-imported row without a
// detected update still shows its source and the check action, but no update
// badge.
func TestRenderAppliedRowRepoNoUpdate(t *testing.T) {
	b := AppliedBlueprint{
		BlueprintID:   "bp-repo",
		Name:          "vendor-pack",
		Version:       "1.0.0",
		AppliedAt:     "2026-08-26T10:00:00Z",
		SourceURL:     "https://github.com/acme/vendor-pack.git",
		LastCheckedAt: "2026-10-07T09:00:00Z",
	}

	html := renderHTML(t, appliedRow(b, AvailableSchemaItem{}))
	if !strings.Contains(html, `data-testid="blueprint-check-updates-vendor-pack"`) {
		t.Error("repo row must offer Check for updates")
	}
	if !strings.Contains(html, "github.com/acme/vendor-pack") {
		t.Error("repo row must show the source label (trimmed .git)")
	}
	if strings.Contains(html, "Update available:") {
		t.Error("row without a detected update must not show the update badge")
	}
}

// TestRenderGithubTokenDialog asserts the token dialog keeps the import-dialog
// field pattern (password input, form action, submit hook) and never echoes a
// token value.
func TestRenderGithubTokenDialog(t *testing.T) {
	html := renderHTML(t, githubTokenDialog())
	for _, want := range []string{
		`id="blueprint-github-token-modal"`,
		`id="blueprint-github-token-title"`,
		`action="/blueprints/github-credentials"`,
		`name="token"`,
		`type="password"`,
		`autocomplete="off"`,
		`data-testid="blueprint-github-token"`,
		`data-testid="blueprint-github-token-save"`,
		"GitHub access token",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("token dialog missing %q", want)
		}
	}
	if strings.Contains(html, `value="`) {
		t.Error("token dialog must not prefill/echo a token value")
	}
}

// TestRenderBlueprintsPageRepoUpdateSurfaces asserts the composed page wires the
// repo update row and the token dialog together (the header menu trigger opens
// the dialog).
func TestRenderBlueprintsPageRepoUpdateSurfaces(t *testing.T) {
	applied := []AppliedBlueprint{{
		BlueprintID:     "bp-repo",
		Name:            "vendor-pack",
		Version:         "1.0.0",
		AppliedAt:       "2026-08-26T10:00:00Z",
		SourceURL:       "https://github.com/acme/vendor-pack",
		SourceRef:       "v1",
		UpdateAvailable: true,
		LatestVersion:   "2.0.0",
	}}
	html := renderHTML(t, BlueprintsPage(applied, nil, nil, nil, nil, "", nil))
	for _, want := range []string{
		"Update available: v1.0.0 → v2.0.0",
		`data-testid="blueprint-update-vendor-pack"`,
		`action="/blueprints/vendor-pack/updates/apply"`,
		`data-testid="blueprint-check-updates-vendor-pack"`,
		`data-testid="blueprints-github-token-link"`,
		`data-dialog-open="blueprint-github-token-modal"`,
		`data-testid="blueprint-github-token"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("blueprints page missing %q", want)
		}
	}
}

// --- apply client + routes ---

// TestApplyBlueprintUpdateClient verifies the client POSTs to the apply
// endpoint (empty JSON object body) and decodes the queued job.
func TestApplyBlueprintUpdateClient(t *testing.T) {
	var gotMethod, gotPath, gotBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"id":"job-2","action":"update","status":"pending"}`))
	}))
	defer ts.Close()

	m := NewMemoryClient(ts.URL, "proj")
	job, err := m.ApplyBlueprintUpdate(t.Context(), "vendor-pack")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/blueprints/vendor-pack/updates/apply" {
		t.Errorf("request = %s %s, want POST /api/blueprints/vendor-pack/updates/apply", gotMethod, gotPath)
	}
	if strings.TrimSpace(gotBody) != "{}" {
		t.Errorf("body = %q, want an empty JSON object", gotBody)
	}
	if job == nil || job.Action != "update" || job.Status != "pending" {
		t.Fatalf("job = %+v", job)
	}
}

// TestUIRouteApplyBlueprintUpdate verifies the Update action enqueues an update
// for the named blueprint and redirects with the queued flash (PRG).
func TestUIRouteApplyBlueprintUpdate(t *testing.T) {
	f := &fakeMemory{}
	_, e := newUpdateRouteServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, updateFormRequest(t, "/blueprints/vendor-pack/updates/apply", nil))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/blueprints?updateQueued=1" {
		t.Errorf("location = %q, want /blueprints?updateQueued=1", loc)
	}
	if len(f.appliedUpdates) != 1 || f.appliedUpdates[0] != "vendor-pack" {
		t.Fatalf("appliedUpdates = %v, want [vendor-pack]", f.appliedUpdates)
	}
}

// TestUIRouteApplyBlueprintUpdateConflict verifies a 409 (an update already
// pending/running) surfaces a clear error flash and enqueues nothing else.
func TestUIRouteApplyBlueprintUpdateConflict(t *testing.T) {
	f := &fakeMemory{blueprintErr: &memoryHTTPError{
		Status:  http.StatusConflict,
		Code:    "conflict",
		Message: "an update for this blueprint is already pending or running",
	}}
	_, e := newUpdateRouteServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, updateFormRequest(t, "/blueprints/vendor-pack/updates/apply", nil))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/blueprints?err=") {
		t.Fatalf("location = %q, want /blueprints?err=…", loc)
	}
	if !strings.Contains(loc, "already") || !strings.Contains(loc, "running") {
		t.Errorf("location = %q, want the clear already-running message", loc)
	}
}

// TestUIRouteApplyBlueprintUpdateFailure verifies a non-conflict enqueue failure
// redirects with the mapped error.
func TestUIRouteApplyBlueprintUpdateFailure(t *testing.T) {
	f := &fakeMemory{blueprintErr: errTest}
	_, e := newUpdateRouteServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, updateFormRequest(t, "/blueprints/vendor-pack/updates/apply", nil))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/blueprints?err=") {
		t.Errorf("location = %q, want /blueprints?err=…", loc)
	}
	if len(f.appliedUpdates) != 0 {
		t.Errorf("failed apply must not record an update, got %v", f.appliedUpdates)
	}
}

// TestUIRouteBlueprintsEnrichesLatestJob asserts the page handler loads each
// repo-imported row's latest job from the update-status endpoint, so job state
// (and the badge clearing that follows a success) survives a reload.
func TestUIRouteBlueprintsEnrichesLatestJob(t *testing.T) {
	f := &fakeMemory{
		appliedBlueprints: []AppliedBlueprint{repoBlueprintWithJob(nil)},
		blueprintUpdates: &BlueprintUpdateCheck{
			BlueprintName:   "vendor-pack",
			UpdateAvailable: true,
			LatestJob:       jobWithResult("succeeded", "update", `{"noop":false,"applied":true,"version":"2.0.0"}`),
		},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/blueprints", s.uiBlueprints)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/blueprints", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-testid="blueprint-update-succeeded-vendor-pack"`) {
		t.Errorf("page must reflect the succeeded job loaded on reload:\n%s", body)
	}
	if strings.Contains(body, "Update available:") {
		t.Error("a succeeded update job must clear the update badge after reload")
	}
}
