package main

import (
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
		Drilldown:    "/schema",
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
	// Discovery has no per-unit retry in this change (the controls live in the
	// schema page's own work), so the row must not promise one it cannot run.
	// Absence of the confirm prompt is not enough: a retryable row has no prompt
	// by design, so assert the retry control itself is absent.
	for _, forbidden := range []string{"/schema/discovery", "Retry discovery"} {
		if strings.Contains(html, forbidden) {
			t.Errorf("discovery row offered a retry this change cannot run (%q):\n%s", forbidden, html)
		}
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

// An embedding failure resolves through its own model chain, so the row's live
// test must exercise the embedding field — posting the generative field would
// test something the failure is not about.
func TestPanelTestsTheEmbeddingModelForAnEmbeddingFailure(t *testing.T) {
	p := configurationProblem()
	p.Queue = "chunk_embedding"
	p.Area = "documents"
	p.Label = "Chunk embedding"
	p.Failure.Model = "openai/text-embedding-3-small"
	p.Failure.Cause = "model_unusable"
	p.Failure.Summary = "An embedding model is configured (openai/text-embedding-3-small), but this run could not use it."

	html := problemsPanelHTML(t, documentPageData{Problems: configurationReport(p), CanManage: true})

	for _, want := range []string{
		`name="model_type" value="embedding"`,
		`name="embedding_model" value="openai/text-embedding-3-small"`,
		"Test model",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("panel missing %q\n%s", want, html)
		}
	}
	if strings.Contains(html, `name="generative_model"`) {
		t.Errorf("embedding row posted a generative model test:\n%s", html)
	}
}

// A rejected credential now carries the model it is about, so the row can name
// it and offer the live test — but the copy and the disclosure stay the
// classifier's, because the cause was read from the error, not checked.
func TestPanelNamesTheModelOnARejectedKey(t *testing.T) {
	p := configurationProblem()
	p.Failure = ProcessingProblemFailure{
		Class:       "permission",
		Summary:     "The provider rejected this project's credentials.",
		Remediation: "Update the provider's API key in Project settings → Providers, then retry.",
		Retryable:   false,
		Model:       "openai/deepseek-v4-flash",
		Fix:         &ProcessingProblemFix{Href: "/settings/providers", Label: "Reconnect provider"},
	}

	html := problemsPanelHTML(t, documentPageData{Problems: configurationReport(p), CanManage: true})

	for _, want := range []string{
		"The provider rejected this project",
		`data-testid="processing-problem-model"`,
		"openai/deepseek-v4-flash",
		"Test model",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("panel missing %q\n%s", want, html)
		}
	}
	if strings.Contains(html, `data-testid="processing-problem-unverified"`) {
		t.Errorf("permission row rendered the configuration-only unverified note:\n%s", html)
	}
	if strings.Contains(html, "<details class=\"mt-1\" open") {
		t.Errorf("permission row forced its technical detail open:\n%s", html)
	}
}
