package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"golang.org/x/net/html"
)

// --- schema discovery: the Schema page card and the per-document trigger ---

func newSchemaDiscoveryServer(f *fakeMemory) (*Server, *echo.Echo) {
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/schema/discovery/card", s.uiSchemaDiscoveryCard)
	e.POST("/schema/discovery/start", s.uiSchemaDiscoveryStart)
	e.POST("/schema/discovery/retry/:jobID", s.uiSchemaDiscoveryRetry)
	e.POST("/schema/discovery/cancel/:jobID", s.uiSchemaDiscoveryCancel)
	e.POST("/documents/:id/discover", s.uiDocumentDiscover)
	return s, e
}

// TestSchemaDiscoveryCardPollsWhileRunning pins that a run in flight refreshes
// its own progress. The card was static: the one surface that reports a
// project-wide run only moved when the page was reloaded, so a long run looked
// frozen. The interval must be on the card itself and target the card, not the
// page shell's #main-content (htmx inherits hx-target from <main>).
func TestSchemaDiscoveryCardPollsWhileRunning(t *testing.T) {
	html := renderHTML(t, schemaDiscoveryCard(&SchemaDiscoveryView{
		Latest:        &discoveryJob{ID: "job-1", Status: "running"},
		DocumentCount: 8,
	}))

	for _, want := range []string{
		`hx-get="/schema/discovery/card"`,
		`hx-trigger="every 5s"`,
		`hx-swap="outerHTML"`,
		`hx-target="this"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("running card missing %q\n%s", want, html)
		}
	}
}

// TestSchemaDiscoveryCardPutsItsActionInTheHeaderColumn pins the #263 layout
// contract: the start action lives inside the shared header component, in a
// column beside the title, not as a flex-wrap sibling that wrapped under it
// while keeping its intrinsic width and right alignment. Below sm the header
// stacks; from sm up it is a two-column row, and the action stops stretching.
func TestSchemaDiscoveryCardPutsItsActionInTheHeaderColumn(t *testing.T) {
	doc := parseDOM(t, renderHTML(t, schemaDiscoveryCard(&SchemaDiscoveryView{DocumentCount: 8})))

	header := findElementByAttr(doc, "data-component", "components/CardHeader")
	if header == nil {
		t.Fatal("discovery card has no components.CardHeader wrapper")
	}
	for _, want := range []string{"flex-col", "gap-3", "sm:flex-row", "sm:items-start", "sm:justify-between"} {
		if !hasClasses(header, want) {
			t.Errorf("header class missing %q: %q", want, classAttr(header))
		}
	}
	if hasClasses(header, "flex-wrap") {
		t.Errorf("header still wraps instead of using the responsive column: %q", classAttr(header))
	}

	// The title block stays in the first column.
	title := findElementByTag(header, "h2")
	if title == nil {
		t.Fatalf("header lost its title: %s", breadcrumbText(header))
	}
	if got := breadcrumbText(title); got != "Schema discovery" {
		t.Errorf("header title = %q, want Schema discovery", got)
	}

	// The action is in the second column of the same header, so a narrow card
	// cannot push it out of the header.
	action := findElementByAttr(header, "data-component", "components/CardHeaderAction")
	if action == nil {
		t.Fatal("header has no action column")
	}
	for _, want := range []string{"w-full", "sm:w-auto", "sm:shrink-0"} {
		if !hasClasses(action, want) {
			t.Errorf("action column class missing %q: %q", want, classAttr(action))
		}
	}
	start := findElementByAttr(action, "data-testid", "schema-discovery-start")
	if start == nil {
		t.Fatal("schema-discovery-start is not inside the header's action column")
	}
	form := start.Parent
	if form == nil || form.Type != html.ElementNode || form.Data != "form" {
		t.Fatalf("start action is not wrapped in its POST form")
	}
	if got := attrValue(form, "action"); got != "/schema/discovery/start" {
		t.Errorf("start form action = %q, want /schema/discovery/start", got)
	}
	if got := attrValue(form, "method"); got != "post" {
		t.Errorf("start form method = %q, want post", got)
	}
}

// TestSchemaDiscoveryCardStopsPollingWhenTerminal is the other half of the
// interval's contract: the swapped-in card must not carry hx-trigger, or the
// browser polls a finished run forever. Each terminal status is checked so a
// new one cannot be added without landing on the right side of isRunning.
func TestSchemaDiscoveryCardStopsPollingWhenTerminal(t *testing.T) {
	for _, status := range []string{"completed", "failed", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			html := renderHTML(t, schemaDiscoveryCard(&SchemaDiscoveryView{
				Latest:        &discoveryJob{ID: "job-1", Status: status},
				DocumentCount: 8,
			}))
			if strings.Contains(html, "hx-trigger") {
				t.Errorf("%s card still polls\n%s", status, html)
			}
			if !strings.Contains(html, `id="discovery"`) {
				t.Errorf("%s card lost its anchor id\n%s", status, html)
			}
		})
	}
}

// discoveryJobsFlakyMemory fails the first failures ListDiscoveryJobs calls and
// then serves the fake's jobs. It models a transient read failure: the first
// card render has no state at all, the next read recovers it.
type discoveryJobsFlakyMemory struct {
	*fakeMemory
	failures int
}

func (f *discoveryJobsFlakyMemory) ListDiscoveryJobs(ctx context.Context) ([]discoveryJob, error) {
	if f.failures > 0 {
		f.failures--
		return nil, errors.New("backend unavailable")
	}
	return f.fakeMemory.ListDiscoveryJobs(ctx)
}

// TestSchemaDiscoveryCardPollsThroughALoadError pins the recovery path: when
// ListDiscoveryJobs fails, the card's state is unknown rather than terminal, so
// it must keep its interval and ask again. Without it one transient blip
// freezes the card forever — the run continues server-side, but the reader
// never sees it advance. The recovered read is a terminal job, which is the
// only state allowed to stop the interval.
func TestSchemaDiscoveryCardPollsThroughALoadError(t *testing.T) {
	f := &fakeMemory{
		discoveryJobs:  []discoveryJob{{ID: "job-1", Status: "completed"}},
		documentsTotal: 8,
	}
	memory := &discoveryJobsFlakyMemory{fakeMemory: f, failures: 1}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: memory}
	e := echo.New()
	e.GET("/schema/discovery/card", s.uiSchemaDiscoveryCard)

	// First read: the list fails, so the card says the state is unknown and
	// carries the interval to ask again.
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/schema/discovery/card", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("error card status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Could not load the latest discovery state") {
		t.Fatalf("load error not surfaced:\n%s", body)
	}
	for _, want := range []string{
		`hx-trigger="every 5s"`,
		`hx-get="/schema/discovery/card"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("error card missing %q, so a transient failure would freeze it:\n%s", want, body)
		}
	}

	// Second read: the job is known to be terminal, so the recovered card
	// stops asking.
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/schema/discovery/card", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("recovered card status = %d, want 200", rec.Code)
	}
	body = rec.Body.String()
	if strings.Contains(body, "Could not load the latest discovery state") {
		t.Fatalf("recovered card still reports the load error:\n%s", body)
	}
	if !strings.Contains(body, "Completed") {
		t.Fatalf("recovered card missing the terminal job:\n%s", body)
	}
	if strings.Contains(body, "hx-trigger") {
		t.Fatalf("terminal card still polls:\n%s", body)
	}
}

// TestSchemaDiscoveryCardEndpointRendersTheFragment pins the poll endpoint: it
// answers with the card alone (no page shell, no compiled type browser) and
// carries the interval that keeps it alive while the run is in flight.
func TestSchemaDiscoveryCardEndpointRendersTheFragment(t *testing.T) {
	f := &fakeMemory{discoveryJobs: []discoveryJob{{ID: "job-1", Status: "running"}}, documentsTotal: 8}
	_, e := newSchemaDiscoveryServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/schema/discovery/card", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("card status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-testid="schema-discovery-card"`) || !strings.Contains(body, `hx-trigger="every 5s"`) {
		t.Fatalf("card fragment missing the polling card:\n%s", body)
	}
	if strings.Contains(body, "<html") || strings.Contains(body, "compiled-type") {
		t.Errorf("poll endpoint rendered more than the card:\n%s", body)
	}
	if f.lastDocumentListParams.Limit != 1 {
		t.Errorf("document count used limit %d, want 1", f.lastDocumentListParams.Limit)
	}
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
	if !strings.HasPrefix(loc, "/schema?err=") {
		t.Fatalf("start location = %q, want /schema?err=", loc)
	}
	if !strings.HasSuffix(loc, "#discovery") {
		t.Errorf("start location = %q, want the card's fragment", loc)
	}
	if u, err := url.Parse(loc); err != nil {
		t.Fatalf("parse start location %q: %v", loc, err)
	} else if got := u.Query().Get("err"); !strings.Contains(got, "already running") {
		t.Errorf("err flash = %q, want the server's message", got)
	}
	if f.discoveryStarted != 0 {
		t.Fatalf("project runs started = %d, want 0", f.discoveryStarted)
	}
}

// TestSchemaDiscoveryStartFailureRedirectsBoostedFormsToTheCard is the htmx
// half of the error PRG. A boosted submit is an XHR, so a plain 303 would be
// followed inside the request, htmx would push the followed URL, and the
// fragment would be stripped — putting the toast above the card it describes.
// HX-Redirect makes the browser navigate to the full target, fragment intact.
func TestSchemaDiscoveryStartFailureRedirectsBoostedFormsToTheCard(t *testing.T) {
	f := &fakeMemory{discoveryStartErr: errors.New("discovery job 7 is already running for this project")}
	_, e := newSchemaDiscoveryServer(f)

	req := httptest.NewRequest(http.MethodPost, "/schema/discovery/start", nil)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("boosted start status = %d, want 200", rec.Code)
	}
	got := rec.Header().Get("HX-Redirect")
	if !strings.HasPrefix(got, "/schema?err=") || !strings.HasSuffix(got, "#discovery") {
		t.Fatalf("HX-Redirect = %q, want /schema?err=…#discovery", got)
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("boosted start sent Location %q as well", loc)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse HX-Redirect %q: %v", got, err)
	}
	if msg := u.Query().Get("err"); !strings.Contains(msg, "already running") {
		t.Errorf("err flash = %q, want the server's message", msg)
	}
	if u.Fragment != "discovery" {
		t.Errorf("fragment = %q, want discovery", u.Fragment)
	}
	if f.discoveryStarted != 0 {
		t.Fatalf("project runs started = %d, want 0", f.discoveryStarted)
	}
}

// TestSchemaDiscoveryErrorRedirectsKeepTheFragmentScoped pins that
// scopeLocation rewrites both redirect shapes. A scoped mutation's HX-Redirect
// and its 303 Location must both gain /o/{org}/p/{project} and keep the card
// fragment — a rewrite that only handled Location, or ran after the response
// committed, would send the boosted form to an unscoped 404 or drop the anchor.
func TestSchemaDiscoveryErrorRedirectsKeepTheFragmentScoped(t *testing.T) {
	const want = "/o/o1/p/p1/schema?err=boom#discovery"
	for _, tc := range []struct {
		name     string
		boosted  bool
		wantCode int
		header   string
	}{
		{name: "boosted", boosted: true, wantCode: http.StatusOK, header: "HX-Redirect"},
		{name: "plain", boosted: false, wantCode: http.StatusSeeOther, header: "Location"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeMemory{discoveryStartErr: errors.New("boom")}
			s, _ := newSchemaDiscoveryServer(f)
			e := echo.New()
			e.Pre(s.scopeRewrite)
			e.Use(s.scopeLocation)
			e.POST("/schema/discovery/start", s.uiSchemaDiscoveryStart)

			req := httptest.NewRequest(http.MethodPost, "/o/o1/p/p1/schema/discovery/start", nil)
			if tc.boosted {
				req.Header.Set("HX-Request", "true")
			}
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantCode)
			}
			if got := rec.Header().Get(tc.header); got != want {
				t.Errorf("%s = %q, want %q", tc.header, got, want)
			}
			other := "Location"
			if tc.header == "Location" {
				other = "HX-Redirect"
			}
			if got := rec.Header().Get(other); got != "" {
				t.Errorf("%s = %q, want none", other, got)
			}
		})
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

// TestSchemaDiscoveryRetryRedirectsBoostedFormsToTheCard pins the htmx half of
// the retry's PRG. The panel row and the card's own buttons are inside the
// shell's hx-boosted <main>, so their submits arrive as XHR: a plain 303 is
// followed inside the XHR and htmx pushes the *followed* URL, which has no
// fragment — the reader landed at the top of the compiled type browser instead
// of on the card they just acted on. An HX-Redirect makes the browser navigate,
// fragment included.
func TestSchemaDiscoveryRetryRedirectsBoostedFormsToTheCard(t *testing.T) {
	f := &fakeMemory{}
	_, e := newSchemaDiscoveryServer(f)

	req := httptest.NewRequest(http.MethodPost, "/schema/discovery/retry/job-1", nil)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if got := rec.Header().Get("HX-Redirect"); got != "/schema?discovered=1#discovery" {
		t.Fatalf("HX-Redirect = %q, want the card's fragment", got)
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("boosted retry sent Location %q as well", loc)
	}
	if len(f.discoveryRetries) != 1 || f.discoveryRetries[0] != "job-1" {
		t.Fatalf("retried job ids = %v, want [job-1]", f.discoveryRetries)
	}
}

// TestSchemaDiscoveryRetryEmptyJobIDRedirectsWithError pins the path guard: an
// empty id must not become a request to the jobs root. The handler is called
// directly, because the route pattern rejects an empty segment before the guard
// runs — a request through the router would prove the router's behaviour, not
// this guard's.
func TestSchemaDiscoveryRetryEmptyJobIDRedirectsWithError(t *testing.T) {
	f := &fakeMemory{}
	s, _ := newSchemaDiscoveryServer(f)

	rec := httptest.NewRecorder()
	c := echo.New().NewContext(httptest.NewRequest(http.MethodPost, "/schema/discovery/retry/", nil), rec)
	c.SetParamNames("jobID")
	c.SetParamValues("")
	if err := s.uiSchemaDiscoveryRetry(c); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/schema?err=") || !strings.HasSuffix(loc, "#discovery") {
		t.Errorf("location = %q, want the card with ?err= and its fragment", loc)
	}
	if len(f.discoveryRetries) != 0 {
		t.Fatalf("retried job ids = %v, want none", f.discoveryRetries)
	}
}

// TestSchemaDiscoveryCancelEmptyJobIDRedirectsWithError is the same guard on
// the cancel route: an empty id must not reach the jobs root either.
func TestSchemaDiscoveryCancelEmptyJobIDRedirectsWithError(t *testing.T) {
	f := &fakeMemory{}
	s, _ := newSchemaDiscoveryServer(f)

	rec := httptest.NewRecorder()
	c := echo.New().NewContext(httptest.NewRequest(http.MethodPost, "/schema/discovery/cancel/", nil), rec)
	c.SetParamNames("jobID")
	c.SetParamValues("")
	if err := s.uiSchemaDiscoveryCancel(c); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/schema?err=") || !strings.HasSuffix(loc, "#discovery") {
		t.Errorf("location = %q, want the card with ?err= and its fragment", loc)
	}
	if len(f.discoveryCancels) != 0 {
		t.Fatalf("cancelled jobs = %v, want none", f.discoveryCancels)
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

// TestSchemaDiscoveryCardDoesNotOfferCancelWhileCreatingThePack pins the card
// against the server's cancellable set: creating the pack is the run's last
// step and the API refuses a cancel once it starts, so offering the action would
// turn that refusal into a flash error for a normal state.
func TestSchemaDiscoveryCardDoesNotOfferCancelWhileCreatingThePack(t *testing.T) {
	creating := renderHTML(t, schemaDiscoveryCard(&SchemaDiscoveryView{
		Latest: &discoveryJob{ID: "j-pack", Status: "creating_pack", Trigger: "manual"},
	}))
	if !strings.Contains(creating, "Running") {
		t.Error("a run creating its pack is still running")
	}
	if strings.Contains(creating, "/schema/discovery/cancel/") {
		t.Error("a run creating its pack must not offer Cancel: the API refuses it")
	}
}

// TestSchemaPageShowsTheCancelConfirmation pins the other half of the cancel
// flow: the redirect lands on /schema?cancelled=1, and a page that ignored it
// would look as though the button did nothing.
func TestSchemaPageShowsTheCancelConfirmation(t *testing.T) {
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: &fakeMemory{compiled: &CompiledSchemaTypes{}}}
	e := echo.New()
	e.GET("/schema", s.uiSchema)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/schema?cancelled=1", nil))
	if !strings.Contains(rec.Body.String(), "Discovery run cancelled") {
		t.Error("the cancel redirect must confirm the run was cancelled")
	}

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/schema", nil))
	if strings.Contains(rec.Body.String(), "Discovery run cancelled") {
		t.Error("a plain load must not repeat the cancel confirmation")
	}
}

// TestSchemaDiscoveryCardHonoursTheServerCancellableFlag pins that the card
// renders the server's decision rather than re-deriving it: a status the old
// local list would call cancellable is not offered when the server says no, and
// the reverse also holds. The status fallback only covers a server too old to
// send the field.
func TestSchemaDiscoveryCardHonoursTheServerCancellableFlag(t *testing.T) {
	no := false
	blocked := renderHTML(t, schemaDiscoveryCard(&SchemaDiscoveryView{
		Latest: &discoveryJob{ID: "j1", Status: "extracting_types", CancellableFlag: &no},
	}))
	if strings.Contains(blocked, "/schema/discovery/cancel/") {
		t.Error("the server said the job is not cancellable; the card must not offer it")
	}

	yes := true
	allowed := renderHTML(t, schemaDiscoveryCard(&SchemaDiscoveryView{
		Latest: &discoveryJob{ID: "j2", Status: "creating_pack", CancellableFlag: &yes},
	}))
	if !strings.Contains(allowed, "/schema/discovery/cancel/") {
		t.Error("the server said the job is cancellable; the card must offer it")
	}

	// A server that does not send the field still gets the status behaviour.
	legacy := renderHTML(t, schemaDiscoveryCard(&SchemaDiscoveryView{
		Latest: &discoveryJob{ID: "j3", Status: "extracting_types"},
	}))
	if !strings.Contains(legacy, "/schema/discovery/cancel/") {
		t.Error("without the field the card falls back to the status list")
	}
}
