package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ui "github.com/emergent-company/go-daisy/components/ui"
)

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
	var buf bytes.Buffer
	if err := documentsProblemsPanel(sampleProblemsReport(), true).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	html := buf.String()

	for _, want := range []string{
		"Needs attention",
		"2 open",
		"Io_Zigzag_Mo_Wire_Cost.md",
		"Invalid output",
		"/documents/doc-1/extractions/job-1/retry",
		"Retry all extractions",
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
	var buf bytes.Buffer
	if err := documentsProblemsPanel(sampleProblemsReport(), false).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	html := buf.String()
	if strings.Contains(html, "Retry failed embeddings") || strings.Contains(html, "/embeddings/retrigger") {
		t.Errorf("non-admin panel rendered the embedding retrigger: %s", html)
	}
	if !strings.Contains(html, "Retry all extractions") {
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

	var buf bytes.Buffer
	if err := documentsProblemsPanel(report, true).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "Retry all conversions") {
		t.Errorf("panel missing conversion retry: %s", html)
	}
	if strings.Contains(html, "Retry all extractions") {
		t.Errorf("panel offered an extraction retry with no extraction problem: %s", html)
	}
}

func TestDocumentsProblemsPanelHiddenWhenClear(t *testing.T) {
	for _, report := range []*ProcessingProblemsReport{nil, {Total: 0}} {
		var buf bytes.Buffer
		if err := documentsProblemsPanel(report, true).Render(context.Background(), &buf); err != nil {
			t.Fatalf("render: %v", err)
		}
		if strings.Contains(buf.String(), "Needs attention") {
			t.Errorf("empty report rendered the panel: %q", buf.String())
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

// TestDocumentsProblemsPanelHonoursRetryable pins that a deterministic failure
// (configuration, unreadable input) renders no retry action: the remediation
// tells the user what to fix, and a Retry button that would fail identically is
// the loop the panel exists to stop. The bulk buttons follow the same rule.
func TestDocumentsProblemsPanelHonoursRetryable(t *testing.T) {
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
						Remediation: "Configure a model in Project settings → Providers, then retry.",
						Retryable:   false,
					},
					Action: ProcessingProblemAction{Kind: "retry_extraction", Label: "Retry extraction"},
				},
				{
					Queue:   "document_parsing",
					Label:   "scanned.pdf",
					Failure: ProcessingProblemFailure{Class: "input", Retryable: false},
					Action:  ProcessingProblemAction{Kind: "open", Label: "Open document"},
				},
			},
		}},
	}

	var buf bytes.Buffer
	if err := documentsProblemsPanel(report, true).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	html := buf.String()

	if strings.Contains(html, "/documents/doc-1/extractions/job-1/retry") {
		t.Errorf("panel offered a retry for a non-retryable failure: %s", html)
	}
	if strings.Contains(html, "Retry all extractions") {
		t.Errorf("panel offered a bulk extraction retry where nothing is retryable: %s", html)
	}
	if strings.Contains(html, "Retry all conversions") {
		t.Errorf("panel offered a bulk conversion retry where nothing is retryable: %s", html)
	}
	// The remediation is what the user acts on instead.
	if !strings.Contains(html, "Configure a model in Project settings") {
		t.Errorf("panel dropped the remediation for the non-retryable failure: %s", html)
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

// TestDocumentsProblemsPanelShowsCappedCount renders the capped case end to end:
// the badge and the "View all" link must both carry the real total, and the
// header badge must total it.
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

	var buf bytes.Buffer
	if err := documentsProblemsPanel(report, true).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	html := buf.String()

	for _, want := range []string{"264 open", "200 of 264", "View all 264 in Documents"} {
		if !strings.Contains(html, want) {
			t.Errorf("capped panel missing %q", want)
		}
	}
	if strings.Contains(html, "View all 200") {
		t.Errorf("capped panel reported the page size as the total: %s", html)
	}
}
