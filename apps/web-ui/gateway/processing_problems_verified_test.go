package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// configurationProblem is the shape that started this: a model resolves for the
// project, the run failed, and the classifier's own copy used to say the
// provider was not configured.
func configurationProblem() ProcessingProblem {
	return ProcessingProblem{
		Queue:        "discovery",
		Area:         "schema",
		JobID:        "job-9",
		Label:        "Schema discovery",
		ErrorMessage: "All 2 batches failed. Errors: Batch 1: LLM call failed: model \"qwen3:8b\" not found",
		Drilldown:    "/schema/discovery",
		Failure: ProcessingProblemFailure{
			Class:       "configuration",
			Cause:       "model_unusable",
			Summary:     "A model is configured (openai/deepseek-v4-flash), but this run could not use it.",
			Remediation: "Open the technical detail below. If the model or the provider instance looks wrong, update Project settings → Providers, then retry.",
			Retryable:   true,
			Verified:    true,
			Model:       "openai/deepseek-v4-flash",
			Fix:         &ProcessingProblemFix{Href: "/settings/providers", Label: "Review model settings"},
		},
		Action: ProcessingProblemAction{Kind: "retry_discovery", Label: "Retry discovery", Href: "/schema/discovery"},
	}
}

func configurationReport(p ProcessingProblem) *ProcessingProblemsReport {
	return &ProcessingProblemsReport{
		Total: 1,
		Areas: []ProcessingArea{{
			Area:     "schema",
			Label:    "Schema",
			Count:    1,
			Problems: []ProcessingProblem{p},
		}},
	}
}

// A verified configuration failure must name the model it is talking about, hand
// back the evidence, and offer a live test — not send the user to a Providers
// page that already looks correct.
func TestPanelNamesTheModelAndOpensTheEvidence(t *testing.T) {
	html := problemsPanelHTML(t, documentPageData{Problems: configurationReport(configurationProblem()), CanManage: true})

	for _, want := range []string{
		"A model is configured (openai/deepseek-v4-flash), but this run could not use it.",
		`data-testid="processing-problem-model"`,
		"openai/deepseek-v4-flash",
		`hx-post="/settings/providers/model-config/test"`,
		`name="generative_model" value="openai/deepseek-v4-flash"`,
		"Test model",
		"Review model settings",
		`href="/settings/providers"`,
		"/schema/discovery/job-9/retry",
		"Retry discovery",
		"Technical detail",
		"model &#34;qwen3:8b&#34; not found",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("panel missing %q\n%s", want, html)
		}
	}

	// The raw error is the evidence, so a configuration failure opens it.
	if !strings.Contains(html, "<details class=\"mt-1\" open") {
		t.Errorf("configuration failure did not open its technical detail:\n%s", html)
	}
	if strings.Contains(html, `data-testid="processing-problem-unverified"`) {
		t.Errorf("verified copy rendered the unverified note:\n%s", html)
	}
	// Retryable=True means no "will likely fail the same way" confirm.
	if strings.Contains(html, "Retry anyway?") {
		t.Errorf("retryable configuration failure asked for confirmation:\n%s", html)
	}
}

// When the project's model state could not be read, the copy must say it was not
// checked and must not offer a test of a model it did not resolve.
func TestPanelSaysWhenTheCauseWasNotVerified(t *testing.T) {
	p := configurationProblem()
	p.Failure.Cause = ""
	p.Failure.Verified = false
	p.Failure.Model = ""
	p.Failure.Retryable = false
	p.Failure.Summary = "The run reported a model configuration problem."
	p.Failure.Fix = &ProcessingProblemFix{Href: "/settings/providers", Label: "Review model settings"}

	html := problemsPanelHTML(t, documentPageData{Problems: configurationReport(p), CanManage: true})

	if !strings.Contains(html, `data-testid="processing-problem-unverified"`) {
		t.Errorf("unverified configuration copy did not say so:\n%s", html)
	}
	if !strings.Contains(html, "the run&#39;s own report, not a check of your settings") &&
		!strings.Contains(html, "the run's own report, not a check of your settings") {
		t.Errorf("unverified note text missing:\n%s", html)
	}
	if strings.Contains(html, "Test model") {
		t.Errorf("unverified copy offered a model test with no resolved model:\n%s", html)
	}
	if !strings.Contains(html, "Retry anyway?") {
		t.Errorf("non-retryable failure did not ask for confirmation:\n%s", html)
	}
}

// Testing a model costs a provider call and touches the project's credentials,
// so a plain member is not offered it. The fix link still is.
func TestPanelHidesModelTestFromMembers(t *testing.T) {
	html := problemsPanelHTML(t, documentPageData{Problems: configurationReport(configurationProblem()), CanManage: false})
	if strings.Contains(html, "Test model") {
		t.Errorf("member-level panel offered a provider test:\n%s", html)
	}
	if !strings.Contains(html, "Review model settings") {
		t.Errorf("member-level panel lost the fix link:\n%s", html)
	}
}

// --- schema discovery page ---

func TestSchemaDiscoveryPageListsRunsAndOffersRetry(t *testing.T) {
	errText := "model \"qwen3:8b\" not found"
	jobs := []DiscoveryJob{
		{ID: "job-1", Status: "failed", CreatedAt: "2026-11-01T10:00:00Z", ErrorMessage: &errText},
		{ID: "job-2", Status: "completed", CreatedAt: "2026-11-01T09:00:00Z", DiscoveredTypes: []any{"Person", "Company"}},
	}

	var buf bytes.Buffer
	if err := SchemaDiscoveryPage(jobs, nil).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	html := buf.String()

	for _, want := range []string{
		"Schema discovery",
		"Failed",
		"Completed",
		"model &#34;qwen3:8b&#34; not found",
		"/schema/discovery/job-1/retry",
		"Retry discovery",
		"2 types proposed",
		`href="/schema/discovery"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("page missing %q\n%s", want, html)
		}
	}
	// The completed run has nothing to retry.
	if strings.Contains(html, "/schema/discovery/job-2/retry") {
		t.Errorf("completed run rendered a retry action:\n%s", html)
	}
}

func TestSchemaDiscoveryPageEmptyState(t *testing.T) {
	var buf bytes.Buffer
	if err := SchemaDiscoveryPage(nil, nil).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(buf.String(), "No discovery runs yet") {
		t.Errorf("empty page missing its empty state:\n%s", buf.String())
	}
}

func TestSchemaDiscoveryPageLoadError(t *testing.T) {
	var buf bytes.Buffer
	if err := SchemaDiscoveryPage(nil, context.DeadlineExceeded).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(buf.String(), "Failed to load discovery runs") {
		t.Errorf("load error not rendered:\n%s", buf.String())
	}
}

func TestRetryDiscoveryRoute(t *testing.T) {
	f := &fakeMemory{}
	s, e := newClientEcho(f, Config{})
	e.POST("/schema/discovery/:jobId/retry", s.uiRetryDiscovery)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/schema/discovery/job-1/retry", nil)
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/schema/discovery" {
		t.Fatalf("location = %q, want /schema/discovery", loc)
	}
	if len(f.retriedDiscoveryIDs) != 1 || f.retriedDiscoveryIDs[0] != "job-1" {
		t.Fatalf("retried discovery ids = %v", f.retriedDiscoveryIDs)
	}
}
