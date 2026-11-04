package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// --- schema discovery: the Schema page card and the per-document trigger ---

func newSchemaDiscoveryServer(f *fakeMemory) (*Server, *echo.Echo) {
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.POST("/schema/discovery/start", s.uiSchemaDiscoveryStart)
	e.POST("/schema/discovery/retry/:jobID", s.uiSchemaDiscoveryRetry)
	e.POST("/schema/discovery/cancel/:jobID", s.uiSchemaDiscoveryCancel)
	e.POST("/documents/:id/discover", s.uiDocumentDiscover)
	return s, e
}

// TestSchemaDiscoveryStartStartsProjectRun pins that the card's action asks for
// a project-scoped run and lands back on the card with the "started" flash.
func TestSchemaDiscoveryStartStartsProjectRun(t *testing.T) {
	f := &fakeMemory{}
	_, e := newSchemaDiscoveryServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/schema/discovery/start", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("start status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/schema?discovered=1#discovery" {
		t.Errorf("start location = %q", loc)
	}
	if f.discoveryStarted != 1 {
		t.Fatalf("project runs started = %d, want 1", f.discoveryStarted)
	}
}

// TestSchemaDiscoveryStartFailureSurfacesTheServerMessage pins that an overlap
// guard (409) or any other refusal reaches the user instead of silently doing
// nothing: the handler redirects with ?err=.
func TestSchemaDiscoveryStartFailureSurfacesTheServerMessage(t *testing.T) {
	f := &fakeMemory{discoveryStartErr: errors.New("discovery job 7 is already running for this project")}
	_, e := newSchemaDiscoveryServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/schema/discovery/start", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("start status = %d, want 303", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "?err=") {
		t.Fatalf("start location = %q, want ?err=", loc)
	}
	if !strings.Contains(loc, "already+running") && !strings.Contains(loc, "already%20running") {
		t.Errorf("start location = %q, want the server's message", loc)
	}
	if f.discoveryStarted != 0 {
		t.Fatalf("project runs started = %d, want 0", f.discoveryStarted)
	}
}

// TestSchemaDiscoveryRetryUsesTheRowJobID pins the panel/card retry contract:
// the job id in the path is the one re-run, and no document list is sent.
func TestSchemaDiscoveryRetryUsesTheRowJobID(t *testing.T) {
	f := &fakeMemory{}
	_, e := newSchemaDiscoveryServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/schema/discovery/retry/job-1", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("retry status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/schema?discovered=1#discovery" {
		t.Errorf("retry location = %q", loc)
	}
	if len(f.discoveryRetries) != 1 || f.discoveryRetries[0] != "job-1" {
		t.Fatalf("retried job ids = %v, want [job-1]", f.discoveryRetries)
	}
}

// TestSchemaDiscoveryRetryEmptyJobIDRedirectsWithError pins the path guard: an
// empty id must not become a request to the jobs root.
func TestSchemaDiscoveryRetryEmptyJobIDRedirectsWithError(t *testing.T) {
	f := &fakeMemory{}
	_, e := newSchemaDiscoveryServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/schema/discovery/retry/", nil))
	// The route pattern does not match an empty segment; the important part is
	// that no retry was issued either way.
	if len(f.discoveryRetries) != 0 {
		t.Fatalf("retried job ids = %v, want none", f.discoveryRetries)
	}
}

// TestDocumentDiscoverStartsOneDocumentRun pins the per-document trigger: it
// posts the document id and returns to the document page with the flash.
func TestDocumentDiscoverStartsOneDocumentRun(t *testing.T) {
	f := &fakeMemory{documents: []Document{{ID: "d1", Filename: "doc.md"}}}
	_, e := newSchemaDiscoveryServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/documents/d1/discover", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("discover status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/documents/d1?discovering=1" {
		t.Errorf("discover location = %q", loc)
	}
	if len(f.discoveryDocStarts) != 1 || f.discoveryDocStarts[0] != "d1" {
		t.Fatalf("document runs started = %v, want [d1]", f.discoveryDocStarts)
	}
}

// TestDocumentDiscoverRefusesADocumentOutsideTheProject pins the pre-check: the
// document is read first (project-scoped), so an id the current project cannot
// see is refused here instead of being forwarded to the run endpoint.
func TestDocumentDiscoverRefusesADocumentOutsideTheProject(t *testing.T) {
	f := &fakeMemory{}
	_, e := newSchemaDiscoveryServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/documents/other-project-doc/discover", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("discover status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/documents/other-project-doc?err=") {
		t.Errorf("discover location = %q, want the document page with ?err=", loc)
	}
	if len(f.discoveryDocStarts) != 0 {
		t.Fatalf("document runs started = %v, want none", f.discoveryDocStarts)
	}
}

// TestDocumentDiscoverFailureStaysOnTheDocument pins that a refused
// per-document run (e.g. a run is already in progress) reports the error on the
// document page instead of navigating away.
func TestDocumentDiscoverFailureStaysOnTheDocument(t *testing.T) {
	f := &fakeMemory{
		documents:         []Document{{ID: "d1", Filename: "doc.md"}},
		discoveryStartErr: errors.New("discovery job 7 is already running for this project"),
	}
	_, e := newSchemaDiscoveryServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/documents/d1/discover", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("discover status = %d, want 303", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/documents/d1?err=") {
		t.Errorf("discover location = %q, want /documents/d1?err=…", loc)
	}
	if len(f.discoveryDocStarts) != 0 {
		t.Fatalf("document runs started = %v, want none", f.discoveryDocStarts)
	}
}

// TestSchemaDiscoveryCardRendersStates pins what the card must say in each
// state: a running run's message and progress bar, a failure with its retry and
// technical detail, the cap note when a project-wide run covered a subset, and
// the completed run's pack link.
func TestSchemaDiscoveryCardRendersStates(t *testing.T) {
	running := renderHTML(t, schemaDiscoveryCard(&SchemaDiscoveryView{
		Latest: &discoveryJob{
			ID:       "j-running",
			Status:   "extracting_types",
			Progress: discoveryProgress{CurrentStep: 2, TotalSteps: 5, Message: "Analyzing batch 2/3…"},
			Trigger:  "manual",
		},
		DocumentCount: 12,
	}))
	for _, want := range []string{
		`data-testid="schema-discovery-card"`,
		`action="/schema/discovery/start"`,
		"Analyzing batch 2/3…",
		"<progress",
		"Started manually.",
		"12 documents in this project",
	} {
		if !strings.Contains(running, want) {
			t.Errorf("running card missing %q", want)
		}
	}
	if strings.Contains(running, "Retry discovery") {
		t.Error("a running job must not offer Retry")
	}
	if !strings.Contains(running, `action="/schema/discovery/cancel/j-running"`) {
		t.Error("a running job must offer Cancel: the run holds the project's one-run slot")
	}

	failed := renderHTML(t, schemaDiscoveryCard(&SchemaDiscoveryView{
		Latest: &discoveryJob{
			ID:             "j-failed",
			Status:         "failed",
			ErrorMessage:   "All 1 batches failed. Errors: Batch 1: failed to parse LLM response: unexpected EOF",
			Trigger:        "source_sync",
			DocumentCount:  500,
			TotalDocuments: 934,
		},
		DocumentCount: 934,
	}))
	for _, want := range []string{
		`action="/schema/discovery/retry/j-failed"`,
		"Retry discovery",
		"unexpected EOF",
		"Started automatically after a source sync.",
		"Covered 500 of 934 documents",
	} {
		if !strings.Contains(failed, want) {
			t.Errorf("failed card missing %q", want)
		}
	}

	completed := renderHTML(t, schemaDiscoveryCard(&SchemaDiscoveryView{
		Latest: &discoveryJob{
			ID:                      "j-done",
			Status:                  "completed",
			SchemaID:                "pack-1",
			DiscoveredTypes:         []json.RawMessage{json.RawMessage(`{}`), json.RawMessage(`{}`), json.RawMessage(`{}`)},
			DiscoveredRelationships: []json.RawMessage{json.RawMessage(`{}`)},
		},
	}))
	for _, want := range []string{
		"3 object types and 1 relationship proposed.",
		`href="/schema/packs/pack-1"`,
		"Review pack",
	} {
		if !strings.Contains(completed, want) {
			t.Errorf("completed card missing %q", want)
		}
	}
}

// TestSchemaDiscoveryCardDegradesOnLoadError pins that a failed job list only
// disables that part of the card: the start action stays available.
func TestSchemaDiscoveryCardDegradesOnLoadError(t *testing.T) {
	html := renderHTML(t, schemaDiscoveryCard(&SchemaDiscoveryView{Err: errors.New("backend unavailable")}))
	if !strings.Contains(html, "Could not load the latest discovery state: backend unavailable") {
		t.Error("load error not surfaced")
	}
	if !strings.Contains(html, `action="/schema/discovery/start"`) {
		t.Error("start action must stay available when the state read fails")
	}
}

// TestSchemaPageRendersDiscoveryCard pins that the card is on the page the
// /schema#discovery links target, with the anchor the links rely on.
func TestSchemaPageRendersDiscoveryCard(t *testing.T) {
	html := renderHTML(t, SchemaPage(&SchemaListView{
		Compiled:  &CompiledSchemaTypes{ObjectTypes: []CompiledType{{Name: "person"}}},
		Discovery: &SchemaDiscoveryView{DocumentCount: 3},
	}, nil))
	if !strings.Contains(html, `id="discovery"`) {
		t.Error(`schema page missing the id="discovery" anchor`)
	}
	if !strings.Contains(html, `data-testid="schema-discovery-card"`) {
		t.Error("schema page missing the discovery card")
	}
}

// TestProcessingProblemRetryDiscovery pins the Documents "Needs attention"
// panel action: a failed discovery row retries through the gateway's discovery
// retry route (a plain POST form), not a dead "Open schema" link.
func TestProcessingProblemRetryDiscovery(t *testing.T) {
	action, ok := processingProblemRetry(ProcessingProblem{
		Queue:   "discovery",
		JobID:   "job-9",
		Label:   "Schema discovery",
		Failure: ProcessingProblemFailure{Class: "limit", Retryable: true},
	})
	if !ok {
		t.Fatal("discovery row should offer a retry")
	}
	if action.Href != "/schema/discovery/retry/job-9" {
		t.Errorf("retry href = %q", action.Href)
	}
	if action.Label != "Retry" {
		t.Errorf("retry label = %q", action.Label)
	}

	// A row without a job id has nothing to re-run and keeps navigation only.
	if _, ok := processingProblemRetry(ProcessingProblem{Queue: "discovery"}); ok {
		t.Error("a discovery row without a job id must not offer a retry")
	}
}

// TestSchemaDiscoveryCancelStopsTheRun pins the cancel action: a run that is in
// flight holds the project's one-run slot, so the user must be able to free it
// without waiting for the process.
func TestSchemaDiscoveryCancelStopsTheRun(t *testing.T) {
	f := &fakeMemory{}
	_, e := newSchemaDiscoveryServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/schema/discovery/cancel/job-9", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("cancel status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/schema?cancelled=1#discovery" {
		t.Errorf("cancel location = %q", loc)
	}
	if len(f.discoveryCancels) != 1 || f.discoveryCancels[0] != "job-9" {
		t.Fatalf("cancelled jobs = %v, want [job-9]", f.discoveryCancels)
	}
}

// TestSchemaDiscoveryCancelFailureSurfacesTheMessage pins that a refused cancel
// (already terminal, or a permission problem) reaches the card.
func TestSchemaDiscoveryCancelFailureSurfacesTheMessage(t *testing.T) {
	f := &fakeMemory{discoveryStartErr: errors.New("job cannot be cancelled (already completed)")}
	_, e := newSchemaDiscoveryServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/schema/discovery/cancel/job-9", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("cancel status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/schema?err=") {
		t.Errorf("cancel location = %q, want /schema?err=…", loc)
	}
	if len(f.discoveryCancels) != 0 {
		t.Fatalf("cancelled jobs = %v, want none", f.discoveryCancels)
	}
}

// TestSchemaDiscoveryCardExplainsAnEmptyProject pins that a project with no
// documents says so: the start action would 400, so the card must not leave the
// user without an explanation.
func TestSchemaDiscoveryCardExplainsAnEmptyProject(t *testing.T) {
	html := renderHTML(t, schemaDiscoveryCard(&SchemaDiscoveryView{DocumentCount: 0}))
	if !strings.Contains(html, "No documents in this project yet") {
		t.Error("an empty project must explain why there is nothing to discover")
	}
	if !strings.Contains(html, `action="/schema/discovery/start"`) {
		t.Error("the start action must still render for an empty project")
	}
}

// TestSchemaDiscoveryCardReportsAnUnreadableCount pins the degraded count: the
// card says the number is unavailable rather than silently omitting it (which
// would read as zero documents).
func TestSchemaDiscoveryCardReportsAnUnreadableCount(t *testing.T) {
	html := renderHTML(t, schemaDiscoveryCard(&SchemaDiscoveryView{
		Latest:   &discoveryJob{ID: "j-1", Status: "completed"},
		CountErr: errors.New("documents unavailable"),
	}))
	if !strings.Contains(html, "document count is unavailable") {
		t.Error("an unreadable count must be reported")
	}
	if strings.Contains(html, "No documents in this project yet") {
		t.Error("an unreadable count must not be rendered as an empty project")
	}
}
