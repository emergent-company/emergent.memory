package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	ui "github.com/emergent-company/go-daisy/components/ui"
	"github.com/labstack/echo/v4"
)

// problemsPanelHTML renders the needs-attention panel the way the Documents page
// does, at the given page state.
func problemsPanelHTML(t *testing.T, data documentPageData) string {
	t.Helper()
	var buf bytes.Buffer
	if err := documentsProblemsPanel(data).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

// chunkEmbeddingProblems is the shape the panel reported as a dead end: one
// document whose chunk embeddings all failed, so the documents area is non-zero
// while every document's derived status is still "extracted".
func chunkEmbeddingProblems(area string, n int) []ProcessingProblem {
	problems := make([]ProcessingProblem, n)
	for i := range problems {
		problems[i] = ProcessingProblem{
			Queue:      "chunk_embedding",
			Area:       area,
			JobID:      "chunk-job-" + strconv.Itoa(i),
			DocumentID: "doc-1",
			Label:      "Huma HW & SW sync",
			Drilldown:  "/documents/doc-1",
			Failure: ProcessingProblemFailure{
				Class:       "configuration",
				Summary:     "The model or provider is not configured correctly.",
				Remediation: "Configure a model in Project settings → Providers, then retry.",
				Retryable:   false,
			},
			Action: ProcessingProblemAction{Kind: "retry_embeddings", Label: "Retry failed embeddings"},
		}
	}
	return problems
}

func sampleProblemsReport() *ProcessingProblemsReport {
	return &ProcessingProblemsReport{
		ProjectID: "p1",
		Total:     2,
		Areas: []ProcessingArea{
			{
				Area:    "documents",
				Label:   "Documents",
				Count:   1,
				Summary: "Invalid model output 1",
				Problems: []ProcessingProblem{
					{
						Queue:      "object_extraction",
						Area:       "documents",
						JobID:      "job-1",
						DocumentID: "doc-1",
						Label:      "Io_Zigzag_Mo_Wire_Cost.md",
						Failure: ProcessingProblemFailure{
							Class:       "invalid_output",
							Summary:     "The model returned output that could not be read.",
							Remediation: "Retry.",
							Retryable:   true,
						},
						Action: ProcessingProblemAction{
							Kind:  "retry_extraction",
							Label: "Retry extraction",
						},
					},
				},
			},
			{
				Area:    "graph",
				Label:   "Knowledge graph",
				Count:   1,
				Summary: "Provider error 1",
				Problems: []ProcessingProblem{
					{
						Queue: "graph_embedding",
						Area:  "graph",
						Label: "Project",
						Failure: ProcessingProblemFailure{
							Class:     "provider",
							Summary:   "The model or embedding provider rejected the request.",
							Retryable: true,
						},
						Action: ProcessingProblemAction{
							Kind:         "retry_embeddings",
							Label:        "Retry failed embeddings",
							RequiresRole: "project_admin",
						},
					},
				},
			},
		},
	}
}

func TestDocumentsProblemsPanelRendersActionsAndCauses(t *testing.T) {
	html := problemsPanelHTML(t, documentPageData{Problems: sampleProblemsReport(), CanManage: true})

	for _, want := range []string{
		"Needs attention",
		"2 open",
		"Io_Zigzag_Mo_Wire_Cost.md",
		"Invalid output",
		"/documents/doc-1/extractions/job-1/retry",
		"Retry all extractions in project",
		"re-run only failures marked retryable",
		"Retry failed embeddings",
		"/embeddings/retrigger",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("panel missing %q", want)
		}
	}
}

func TestDocumentsProblemsPanelHidesAdminEmbeddingRetry(t *testing.T) {
	// A project member can retry extractions/conversions, but the embedding
	// retrigger is project_admin-only. Do not render a button the API rejects.
	html := problemsPanelHTML(t, documentPageData{Problems: sampleProblemsReport(), CanManage: false})
	if strings.Contains(html, "Retry failed embeddings") || strings.Contains(html, "/embeddings/retrigger") {
		t.Errorf("non-admin panel rendered the embedding retrigger: %s", html)
	}
	if !strings.Contains(html, "Retry all extractions in project") {
		t.Errorf("member-level extraction retry disappeared: %s", html)
	}
}

func TestDocumentsProblemsPanelOffersConversionRetry(t *testing.T) {
	report := &ProcessingProblemsReport{
		Total: 1,
		Areas: []ProcessingArea{{
			Area:  "documents",
			Label: "Documents",
			Count: 1,
			Problems: []ProcessingProblem{{
				Queue: "document_parsing",
				Label: "scanned.pdf",
				Failure: ProcessingProblemFailure{
					Class:     "provider",
					Retryable: true,
				},
				Action: ProcessingProblemAction{
					Kind:  "open",
					Label: "Open document",
				},
			}},
		}},
	}

	html := problemsPanelHTML(t, documentPageData{Problems: report, CanManage: true})
	if !strings.Contains(html, "Retry all conversions in project") {
		t.Errorf("panel missing conversion retry: %s", html)
	}
	if strings.Contains(html, "Retry all extractions") {
		t.Errorf("panel offered an extraction retry with no extraction problem: %s", html)
	}
}

func TestDocumentsProblemsPanelHiddenWhenClear(t *testing.T) {
	for _, report := range []*ProcessingProblemsReport{nil, {Total: 0}} {
		html := problemsPanelHTML(t, documentPageData{Problems: report, CanManage: true})
		if strings.Contains(html, "Needs attention") {
			t.Errorf("empty report rendered the panel: %q", html)
		}
	}
}

func TestSidebarDocumentsBadge(t *testing.T) {
	groups := sidebarGroupsWithWarn("", false, 7)
	found := false
	for _, g := range groups {
		for _, item := range g.Items {
			if item.Label != "Documents" {
				continue
			}
			found = true
			if item.Warn == nil {
				t.Fatal("Documents item has no warning component")
			}
			var buf bytes.Buffer
			if err := item.Warn.Render(context.Background(), &buf); err != nil {
				t.Fatalf("render badge: %v", err)
			}
			if !strings.Contains(buf.String(), ">7<") {
				t.Fatalf("badge = %q, want count 7", buf.String())
			}
		}
	}
	if !found {
		t.Fatal("Documents item missing from sidebar")
	}

	for _, g := range sidebarGroupsWithWarn("", false, 0) {
		for _, item := range g.Items {
			if item.Label == "Documents" && item.Warn != nil {
				t.Fatal("zero problems rendered a badge")
			}
		}
	}
}

func TestRetryExtractionRoute(t *testing.T) {
	f := &fakeMemory{}
	s, e := newClientEcho(f, Config{})
	e.POST("/documents/:id/extractions/:jobId/retry", s.uiRetryExtraction)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/documents/doc-1/extractions/job-1/retry", nil)
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/documents/doc-1/extractions?retried=1" {
		t.Fatalf("location = %q", loc)
	}
	if len(f.retriedJobIDs) != 1 || f.retriedJobIDs[0] != "job-1" {
		t.Fatalf("retried ids = %v", f.retriedJobIDs)
	}
}

func TestExtractionJobStatusLabels(t *testing.T) {
	tests := []struct {
		status       string
		deadLettered bool
		want         string
	}{
		{"queued", false, "Queued"},
		{"running", false, "Extracting"},
		{"completed", false, "Completed"},
		{"failed", false, "Failed"},
		{"failed", true, "Failed permanently"},
		{"requires_review", false, "Review needed"},
		{"cancelled", false, "Cancelled"},
	}
	for _, tc := range tests {
		if got := extractionJobStatusLabel(tc.status, tc.deadLettered); got != tc.want {
			t.Errorf("label(%q, %v) = %q, want %q", tc.status, tc.deadLettered, got, tc.want)
		}
	}
	if got := extractionJobStatusIntent("failed", true); got != ui.BadgeError {
		t.Errorf("dead-lettered intent = %v, want error", got)
	}
}

func TestProcessingProblemsCacheInvalidation(t *testing.T) {
	f := &fakeMemory{problems: &ProcessingProblemsReport{Total: 1}}
	s, _ := newClientEcho(f, Config{MemoryProjectID: "p1"})
	ctx := context.Background()

	first, err := s.cachedProcessingProblems(ctx)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if first.Total != 1 {
		t.Fatalf("first total = %d", first.Total)
	}

	// A new server-side report is not visible until the cache is dropped.
	f.problems = &ProcessingProblemsReport{Total: 2}
	cached, err := s.cachedProcessingProblems(ctx)
	if err != nil {
		t.Fatalf("cached: %v", err)
	}
	if cached.Total != 1 {
		t.Fatalf("cache was not used: total = %d", cached.Total)
	}

	s.invalidateProcessingProblems(ctx)
	fresh, err := s.cachedProcessingProblems(ctx)
	if err != nil {
		t.Fatalf("fresh: %v", err)
	}
	if fresh.Total != 2 {
		t.Fatalf("invalidation did not refresh: total = %d", fresh.Total)
	}
}

// TestDocumentsProblemsPanelOffersRetryAfterFix pins the policy the panel now
// follows: a configuration failure is not retried *automatically*, but the user
// who has just configured a model must be able to retry from the row instead of
// hunting for another page. The row carries the retry, a guard prompt, and the
// link to where the cause is fixed; the bulk action stays opt-in behind the
// explicit "after fixing the cause" button.
func TestDocumentsProblemsPanelOffersRetryAfterFix(t *testing.T) {
	report := &ProcessingProblemsReport{
		Total: 2,
		Areas: []ProcessingArea{{
			Area:  "documents",
			Label: "Documents",
			Count: 2,
			Problems: []ProcessingProblem{
				{
					Queue:      "object_extraction",
					JobID:      "job-1",
					DocumentID: "doc-1",
					Label:      "report.pdf",
					Failure: ProcessingProblemFailure{
						Class:       "configuration",
						Summary:     "The model or provider is not configured correctly.",
						Remediation: "Configure a model in Project settings → Providers, then use Retry on the failed run.",
						Retryable:   false,
					},
					Action: ProcessingProblemAction{Kind: "retry_extraction", Label: "Retry extraction"},
				},
				{
					// A conversion with no known document cannot be retried from
					// the row: the job id alone does not say which page to land on.
					Queue:   "document_parsing",
					Label:   "scanned.pdf",
					Failure: ProcessingProblemFailure{Class: "input", Retryable: false},
					Action:  ProcessingProblemAction{Kind: "open", Label: "Open document"},
				},
			},
		}},
	}

	html := problemsPanelHTML(t, documentPageData{Problems: report, CanManage: true})

	for _, want := range []string{
		"/documents/doc-1/extractions/job-1/retry",
		"/settings/providers",
		"Configure model",
		"Retry anyway?",
		"Retry all after fixing the cause",
		"include_non_retryable=1",
		"Configure a model in Project settings",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("non-retryable row missing %q: %s", want, html)
		}
	}
	// The plain bulk button would silently skip every unit in this report, so
	// it must not be offered as if it acted on them.
	if strings.Contains(html, "Retry all extractions in project") {
		t.Errorf("panel offered a plain bulk extraction retry where nothing is retryable: %s", html)
	}
	if strings.Contains(html, "/documents/conversions/") {
		t.Errorf("panel offered a conversion retry without a job id: %s", html)
	}
}

// TestRetryAllExtractionsPassesIncludeFlag pins that the bulk handler forwards
// the panel's explicit opt-in and that a plain click does not.
func TestRetryAllExtractionsPassesIncludeFlag(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    string
		query   string
		wantInc bool
	}{
		{name: "safe default", body: "", wantInc: false},
		{name: "explicit form field", body: "include_non_retryable=1", wantInc: true},
		{name: "explicit query", query: "?include_non_retryable=true", wantInc: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeMemory{bulkRetried: 2}
			s, _ := newClientEcho(f, Config{})
			c, _ := prgContext(context.Background(), http.MethodPost, "/documents/retry-extraction-failures"+tc.query)
			if tc.body != "" {
				c.Request().Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
				c.Request().Body = io.NopCloser(strings.NewReader(tc.body))
			}
			if err := s.uiRetryAllExtractions(c); err != nil {
				t.Fatalf("handler: %v", err)
			}
			if len(f.bulkIncludeNonRetryable) != 1 || f.bulkIncludeNonRetryable[0] != tc.wantInc {
				t.Fatalf("include flag = %v, want [%v]", f.bulkIncludeNonRetryable, tc.wantInc)
			}
		})
	}
}

// TestRetriggerEmbeddingsPassesIncludeFlag pins the end of the embeddings
// after-fixing path: the form's query string reaches the gateway handler and
// must be forwarded to the API, which filters retryable failures by default.
// Without the forward the button posts, resets nothing, and reports zero on the
// configuration-only area it exists for.
func TestRetriggerEmbeddingsPassesIncludeFlag(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    string
		query   string
		wantInc bool
	}{
		{name: "safe default", body: "", wantInc: false},
		{name: "explicit form field", body: "include_non_retryable=1", wantInc: true},
		{name: "explicit query", query: "?include_non_retryable=1", wantInc: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeMemory{embeddingsRetriggered: 2}
			s, _ := newClientEcho(f, Config{})
			c, _ := prgContext(context.Background(), http.MethodPost, "/embeddings/retrigger"+tc.query)
			if tc.body != "" {
				c.Request().Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
				c.Request().Body = io.NopCloser(strings.NewReader(tc.body))
			}
			if err := s.uiRetriggerEmbeddings(c); err != nil {
				t.Fatalf("handler: %v", err)
			}
			if len(f.embeddingIncludeNonRetryable) != 1 || f.embeddingIncludeNonRetryable[0] != tc.wantInc {
				t.Fatalf("include flag = %v, want [%v]", f.embeddingIncludeNonRetryable, tc.wantInc)
			}
		})
	}
}

// TestMemoryClientRetriggerEmbeddingsCarriesOptIn pins the wire request: the
// project retrigger forwards the explicit opt-in as include_non_retryable, and a
// plain sweep does not. The server filters retryable failures by default, so
// this query is what makes the panel's after-fixing action reset the
// configuration failures it exists for.
func TestMemoryClientRetriggerEmbeddingsCarriesOptIn(t *testing.T) {
	for _, tc := range []struct {
		name    string
		include bool
		wantQ   string
	}{
		{name: "plain sweep", include: false, wantQ: ""},
		{name: "after fixing the cause", include: true, wantQ: "include_non_retryable=true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("method = %s, want POST", r.Method)
				}
				if r.URL.Path != "/api/projects/p1/embeddings/retrigger" {
					t.Errorf("path = %q", r.URL.Path)
				}
				if got := r.URL.Query().Get("include_non_retryable"); got != map[bool]string{true: "true", false: ""}[tc.include] {
					t.Errorf("include_non_retryable = %q, want %q", got, tc.wantQ)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"objects_reset":2,"chunks_reset":3}`))
			}))
			defer ts.Close()

			n, err := NewMemoryClient(ts.URL, "p1").RetriggerEmbeddings(context.Background(), tc.include)
			if err != nil {
				t.Fatalf("RetriggerEmbeddings: %v", err)
			}
			if n != 5 {
				t.Fatalf("reset count = %d, want 5", n)
			}
		})
	}
}

// TestRetryConversionRoute pins the per-row conversion retry: it resets the
// job and lands the user back on the document the panel named.
func TestRetryConversionRoute(t *testing.T) {
	f := &fakeMemory{}
	s, e := newClientEcho(f, Config{})
	e.POST("/documents/conversions/:jobId/retry", s.uiRetryConversion)

	body := strings.NewReader("document_id=doc-9")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/documents/conversions/job-9/retry", body)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/documents/doc-9?conversion_retried=1" {
		t.Fatalf("location = %q", loc)
	}
	if len(f.retriedParsingJobIDs) != 1 || f.retriedParsingJobIDs[0] != "job-9" {
		t.Fatalf("retried conversion ids = %v", f.retriedParsingJobIDs)
	}
}

// TestProcessingAreaCountLabelIsHonest pins that a capped area never presents
// the page size as the backlog.
func TestProcessingAreaCountLabelIsHonest(t *testing.T) {
	withoutCap := ProcessingArea{Count: 12, Problems: make([]ProcessingProblem, 12)}
	if got := processingAreaCountLabel(withoutCap); got != "12" {
		t.Errorf("exact label = %q, want %q", got, "12")
	}

	// 264 failed conversions, of which the report carried 200 rows.
	capped := ProcessingArea{Count: 264, Truncated: true, Problems: make([]ProcessingProblem, 200)}
	if got := processingAreaCountLabel(capped); got != "200 of 264" {
		t.Errorf("truncated label = %q, want %q", got, "200 of 264")
	}
}

// TestDocumentsProblemsPanelExplainsInternalFailure pins the copy that replaced
// the cryptic "after 3 attempts": a deterministic pipeline crash says so, shows
// the run count, deep-links the run, and does not offer a futile Retry.
func TestDocumentsProblemsPanelExplainsInternalFailure(t *testing.T) {
	report := &ProcessingProblemsReport{
		Total: 1,
		Areas: []ProcessingArea{{
			Area:  "documents",
			Label: "Documents",
			Count: 1,
			Problems: []ProcessingProblem{{
				Queue:      "object_extraction",
				JobID:      "job-1",
				DocumentID: "doc-1",
				Label:      "Taurus design considerations.md",
				Attempts:   3,
				Drilldown:  "/documents/doc-1/extractions/job-1",
				Failure: ProcessingProblemFailure{
					Class:       "internal",
					Summary:     "The extraction pipeline stopped before saving results.",
					Remediation: "This is a bug in Memory, not your document. Retrying the same input will not fix it — report it with the technical detail.",
					Retryable:   false,
				},
				Action: ProcessingProblemAction{Kind: "retry_extraction", Label: "Retry extraction"},
			}},
		}},
	}

	html := problemsPanelHTML(t, documentPageData{Problems: report, CanManage: true})

	for _, want := range []string{
		"3 automatic runs exhausted",
		"not your document",
		"/documents/doc-1/extractions/job-1",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("internal-failure row missing %q", want)
		}
	}
	if strings.Contains(html, "/extractions/job-1/retry") {
		t.Errorf("internal-failure row offered a retry that cannot succeed: %s", html)
	}
}

// TestDocumentsProblemsPanelShowsCappedCount renders the capped case end to end:
// the badge and the "View all" link must both carry the real total, and the
// header badge must total it. A truncated area's link promises only the rows the
// expanded view can render, so it cannot over-state the backlog the way the
// removed document-status filter did.
func TestDocumentsProblemsPanelShowsCappedCount(t *testing.T) {
	problems := make([]ProcessingProblem, 200)
	for i := range problems {
		problems[i] = ProcessingProblem{
			Queue:   "document_parsing",
			Label:   "scan.pdf",
			Failure: ProcessingProblemFailure{Class: "provider", Retryable: true},
			Action:  ProcessingProblemAction{Kind: "open", Label: "Open document"},
		}
	}
	report := &ProcessingProblemsReport{
		Total: 264,
		Areas: []ProcessingArea{{
			Area:      "documents",
			Label:     "Documents",
			Count:     264,
			Truncated: true,
			Problems:  problems,
		}},
	}

	html := problemsPanelHTML(t, documentPageData{Problems: report, CanManage: true})

	for _, want := range []string{"264 open", "200 of 264", "View 200 of 264 in Documents"} {
		if !strings.Contains(html, want) {
			t.Errorf("capped panel missing %q", want)
		}
	}
	if strings.Contains(html, "View all 264") {
		t.Errorf("capped panel promised a full list the report cannot render: %s", html)
	}
}

// TestDocumentsProblemsPanelRetryableAttemptsWording pins that "exhausted" is
// reserved for non-retryable rows; a retryable failure still says how many
// automatic attempts happened.
func TestDocumentsProblemsPanelRetryableAttemptsWording(t *testing.T) {
	report := &ProcessingProblemsReport{
		Total: 1,
		Areas: []ProcessingArea{{
			Area:  "documents",
			Label: "Documents",
			Count: 1,
			Problems: []ProcessingProblem{{
				Queue:      "object_extraction",
				JobID:      "job-1",
				DocumentID: "doc-1",
				Label:      "doc.md",
				Attempts:   2,
				Failure: ProcessingProblemFailure{
					Class:     "invalid_output",
					Retryable: true,
				},
				Action: ProcessingProblemAction{Kind: "retry_extraction", Label: "Retry extraction"},
			}},
		}},
	}
	html := problemsPanelHTML(t, documentPageData{Problems: report, CanManage: true})
	if !strings.Contains(html, "2 automatic attempts so far") {
		t.Fatalf("retryable row missing attempts wording: %s", html)
	}
	if strings.Contains(html, "automatic runs exhausted") {
		t.Fatalf("retryable row claimed retries were exhausted: %s", html)
	}
}

// TestProcessingAreaHrefStaysInThePanel is the regression test for the "View all
// 104 in Documents → No documents match" dead end: the area's full list must be
// the panel's own rows, never the documents listing's derived-status filter.
// A chunk-embedding failure leaves its document "extracted", and a conversion
// job without a document row has none at all, so neither can ever match
// ?status=failed.
func TestProcessingAreaHrefStaysInThePanel(t *testing.T) {
	data := documentPageData{Sort: documentSortCreated, Order: documentOrderDesc}

	if got := data.attentionHref("documents"); got != "/documents?attention=documents#needs-attention" {
		t.Errorf("attention href = %q, want the panel's expanded view", got)
	}
	for _, href := range []string{
		data.attentionHref("documents"),
		data.attentionHref("graph"),
		data.attentionHref("schema"),
	} {
		if strings.Contains(href, "status=") {
			t.Errorf("area href %q filters on a document status, which cannot list queue failures", href)
		}
	}

	// The expansion is page state: the collapse link keeps the active filters.
	filtered := documentPageData{Query: "spec", Status: documentStatusFailed, Attention: "documents"}
	if got := filtered.attentionCollapseHref(); got != "/documents?q=spec&status=failed#needs-attention" {
		t.Errorf("collapse href = %q, want the active filters without the expansion", got)
	}
}

// TestDocumentsProblemsPanelExpandsTheRequestedArea pins the full-list view end
// to end: the requested area renders every row the report carried, the clip link
// is replaced by a collapse link, and the other areas stay clipped.
func TestDocumentsProblemsPanelExpandsTheRequestedArea(t *testing.T) {
	report := &ProcessingProblemsReport{
		Total: 20,
		Areas: []ProcessingArea{
			{Area: "documents", Label: "Documents", Count: 12, Summary: "Configuration 12", Problems: chunkEmbeddingProblems("documents", 12)},
			{Area: "graph", Label: "Knowledge graph", Count: 8, Problems: chunkEmbeddingProblems("graph", 8)},
		},
	}

	clipped := problemsPanelHTML(t, documentPageData{Problems: report})
	if got := strings.Count(clipped, `data-testid="processing-problem-row"`); got != processingProblemsClip+8 {
		t.Errorf("clipped panel rows = %d, want %d (8 per area)", got, processingProblemsClip+8)
	}
	if !strings.Contains(clipped, "View all 12 in Documents") {
		t.Errorf("clipped panel missing the documents full-list link: %s", clipped)
	}
	if !strings.Contains(clipped, "attention=documents") {
		t.Errorf("full-list link does not open the panel's documents area: %s", clipped)
	}

	expanded := problemsPanelHTML(t, documentPageData{Problems: report, Attention: "documents"})
	if got := strings.Count(expanded, `data-testid="processing-problem-row"`); got != 12+8 {
		t.Errorf("expanded panel rows = %d, want 20 (documents expanded, graph clipped)", got)
	}
	if strings.Contains(expanded, "View all 12 in Documents") {
		t.Error("expanded area still offered its own full-list link")
	}
	if !strings.Contains(expanded, `data-testid="processing-area-collapse-documents"`) {
		t.Errorf("expanded area has no way back to the clipped panel: %s", expanded)
	}
}

// TestNormalizeAttentionArea pins that only an area-shaped value reaches the
// panel: the gateway keeps no list of areas (the server owns them), so the check
// is a shape check, and a stale, hand-edited or injected value renders the
// default clipped panel instead of erroring or echoing into the page.
func TestNormalizeAttentionArea(t *testing.T) {
	tests := map[string]string{
		"documents":                   "documents",
		" Documents ":                 "documents",
		"graph":                       "graph",
		"chunk-embedding":             "chunk-embedding",
		"doc_parsing":                 "doc_parsing",
		"an-area-name-we-do-not-know": "an-area-name-we-do-not-know",
		"":                            "",
		"documents; drop table":       "",
		"<script>alert(1)</script>":   "",
		"-leading":                    "",
		strings.Repeat("a", 33):       "",
	}
	for raw, want := range tests {
		if got := normalizeAttentionArea(raw); got != want {
			t.Errorf("normalizeAttentionArea(%q) = %q, want %q", raw, got, want)
		}
	}
}

// TestUIDocumentsAttentionExpandsArea drives the panel through the real handler:
// /documents?attention=documents is the destination of the area's "View all"
// link, and it must render that area's rows instead of the document listing's
// empty state. A value that is not an area name keeps the clipped panel, so a
// stale bookmark is harmless.
func TestUIDocumentsAttentionExpandsArea(t *testing.T) {
	report := &ProcessingProblemsReport{
		Total: 12,
		Areas: []ProcessingArea{
			{
				Area:     "documents",
				Label:    "Documents",
				Count:    12,
				Summary:  "Configuration 12",
				Problems: chunkEmbeddingProblems("documents", 12),
			},
		},
	}

	// A fresh server per request: the report is cached per project, and each
	// case asserts what a cold render produces.
	render := func(query string) string {
		t.Helper()
		f := &fakeMemory{
			documents:      []Document{{ID: "doc-1", Filename: "report.md", ProcessingStatus: "completed"}},
			documentsTotal: 1,
			problems:       report,
		}
		s := &Server{cfg: Config{DefaultAgent: "memory", MemoryProjectID: "p1"}, memory: f}
		e := echo.New()
		e.GET("/documents", s.uiDocuments)

		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/documents"+query, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /documents%s: status = %d, body = %s", query, rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}

	clipped := render("")
	if got := strings.Count(clipped, `data-testid="processing-problem-row"`); got != processingProblemsClip {
		t.Errorf("clipped rows = %d, want %d", got, processingProblemsClip)
	}
	// The full-list link stays on this page and opens this panel.
	if !strings.Contains(clipped, "href=\"/documents?attention=documents#needs-attention\"") {
		t.Errorf("View all link does not open the panel's own documents area: %s", clipped)
	}

	expanded := render("?attention=documents")
	if got := strings.Count(expanded, `data-testid="processing-problem-row"`); got != 12 {
		t.Errorf("expanded rows = %d, want all 12 the report carried", got)
	}
	if !strings.Contains(expanded, `data-testid="processing-area-collapse-documents"`) {
		t.Error("expanded area has no way back to the clipped panel")
	}
	// The active filters survive the expansion, so toggling does not discard a
	// search the reader typed.
	filtered := render("?attention=documents&q=report")
	if !strings.Contains(filtered, "q=report") {
		t.Errorf("filter lost when an area is expanded: %s", filtered)
	}

	stale := render("?attention=documents%3BDROP%20TABLE")
	if got := strings.Count(stale, `data-testid="processing-problem-row"`); got != processingProblemsClip {
		t.Errorf("unknown attention value rows = %d, want the clipped %d", got, processingProblemsClip)
	}
}

// TestDocumentsProblemsPanelOffersEmbeddingsRetryAfterFix pins the embeddings
// case of the same policy: a project imported before an embedding model was
// configured holds only `configuration` failures, which the classifier marks
// non-retryable, so the plain project retrigger is suppressed and the area
// offers the explicit "after fixing the cause" action instead. Without it a
// project in that state had no bulk path at all.
func TestDocumentsProblemsPanelOffersEmbeddingsRetryAfterFix(t *testing.T) {
	report := &ProcessingProblemsReport{
		Total: 1,
		Areas: []ProcessingArea{{
			Area:  "documents",
			Label: "Documents",
			Count: 1,
			Problems: []ProcessingProblem{{
				Queue: "chunk_embedding",
				Label: "report.pdf",
				Failure: ProcessingProblemFailure{
					Class:       "configuration",
					Summary:     "The model or provider is not configured correctly.",
					Remediation: "Configure a model in Project settings → Providers, then use Retry on the failed run.",
					Retryable:   false,
				},
				Action: ProcessingProblemAction{
					Kind:         "retry_embeddings",
					Label:        "Retry failed embeddings",
					RequiresRole: "project_admin",
				},
			}},
		}},
	}

	html := problemsPanelHTML(t, documentPageData{Problems: report, CanManage: true})

	for _, want := range []string{
		"/embeddings/retrigger?include_non_retryable=1",
		"/settings/providers",
		"Retry all embeddings after fixing the cause",
		"configured an embedding model",
		// The project retrigger resets object and chunk embedding jobs; a
		// relationship-embedding failure can sit in the same area and is not
		// covered, so the confirmation must not promise "every" embedding job.
		"every failed object and chunk embedding job",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("non-retryable embeddings row missing %q: %s", want, html)
		}
	}
	if strings.Contains(html, "Retry every failed embedding job") {
		t.Errorf("confirmation claims coverage the project retrigger does not have: %s", html)
	}
	if strings.Contains(html, "Retry failed embeddings") {
		t.Errorf("panel offered the plain embeddings retrigger where nothing is retryable: %s", html)
	}

	anon := problemsPanelHTML(t, documentPageData{Problems: report, CanManage: false})
	if strings.Contains(anon, "/embeddings/retrigger") {
		t.Errorf("a non-admin was offered the project-wide embeddings retrigger: %s", anon)
	}
}

// TestDocumentsProblemsPanelSkipsEmbeddingsActionItCannotHonour pins the two
// shapes the after-fixing embeddings action must not cover: a relationship
// embedding (the project retrigger does not reset those, so the server gives it
// the `open` action) and a failure class no retry serves.
func TestDocumentsProblemsPanelSkipsEmbeddingsActionItCannotHonour(t *testing.T) {
	report := &ProcessingProblemsReport{
		Total: 2,
		Areas: []ProcessingArea{{
			Area:  "graph",
			Label: "Graph",
			Count: 2,
			Problems: []ProcessingProblem{
				{
					Queue:   "graph_relationship_embedding",
					Failure: ProcessingProblemFailure{Class: "configuration", Retryable: false},
					Action:  ProcessingProblemAction{Kind: "open", Label: "Review embeddings"},
				},
				{
					Queue:   "chunk_embedding",
					Failure: ProcessingProblemFailure{Class: "input", Retryable: false},
					Action:  ProcessingProblemAction{Kind: "retry_embeddings", Label: "Retry failed embeddings", RequiresRole: "project_admin"},
				},
			},
		}},
	}

	if html := problemsPanelHTML(t, documentPageData{Problems: report, CanManage: true}); strings.Contains(html, "/embeddings/retrigger") {
		t.Errorf("panel offered an embeddings retrigger it cannot honour: %s", html)
	}
}

// TestDocumentsProblemsPanelRetryableEmbeddingsKeepPlainBulk pins the other
// side: when the area's embedding failures are retryable, the plain retrigger is
// the right action and the after-fixing variant is not offered as well.
func TestDocumentsProblemsPanelRetryableEmbeddingsKeepPlainBulk(t *testing.T) {
	report := &ProcessingProblemsReport{
		Total: 1,
		Areas: []ProcessingArea{{
			Area:  "graph",
			Label: "Graph",
			Count: 1,
			Problems: []ProcessingProblem{{
				Queue:   "graph_embedding",
				Failure: ProcessingProblemFailure{Class: "provider", Retryable: true},
				Action: ProcessingProblemAction{
					Kind:         "retry_embeddings",
					Label:        "Retry failed embeddings",
					RequiresRole: "project_admin",
				},
			}},
		}},
	}

	html := problemsPanelHTML(t, documentPageData{Problems: report, CanManage: true})
	if !strings.Contains(html, "Retry failed embeddings") {
		t.Fatalf("retryable embeddings area lost its plain retrigger: %s", html)
	}
	if strings.Contains(html, "Retry all embeddings after fixing the cause") {
		t.Fatalf("panel offered the after-fixing variant on a fully retryable area: %s", html)
	}
}
