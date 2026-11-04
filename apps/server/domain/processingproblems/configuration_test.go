package processingproblems

import (
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/domain/modelstate"
)

// A rejected credential is a permission problem, not a model-selection one. It
// used to land in ClassConfiguration through the bare "credential" needle, which
// told a user whose provider was already configured to go configure it.
func TestClassifyCredentialIsPermission(t *testing.T) {
	got := Classify(QueueObjectExtraction, "provider rejected the request: credential expired")
	if got.Class != ClassPermission {
		t.Fatalf("class = %q, want %q", got.Class, ClassPermission)
	}
	if got.Verified {
		t.Fatal("verified = true: permission copy is written from the error, not checked against state")
	}
}

// Internal wiring messages ("handler not configured") are not the user's model
// configuration. They must fall through to the retry-and-report copy instead of
// sending the user to Providers.
func TestClassifyNotConfiguredAloneIsNotConfiguration(t *testing.T) {
	got := Classify(QueueAgentRun, "remember-status: extraction job finder not configured")
	if got.Class == ClassConfiguration {
		t.Fatalf("class = %q, want a non-configuration class: %q", got.Class, got.Summary)
	}
}

func TestClassifyModelNotServedIsItsOwnCause(t *testing.T) {
	got := Classify(QueueDiscovery, "All 2 batches failed. Errors: Batch 1: LLM call failed: model 'qwen3:8b' not found")
	if got.Class != ClassConfiguration {
		t.Fatalf("class = %q, want %q", got.Class, ClassConfiguration)
	}
	if got.Cause != CauseModelUnavailable {
		t.Fatalf("cause = %q, want %q", got.Cause, CauseModelUnavailable)
	}
	if got.Verified {
		t.Fatal("verified = true: the classifier cannot confirm the provider's catalogue")
	}
}

func TestClassifyBaseURLIsItsOwnCause(t *testing.T) {
	got := Classify(QueueDiscovery, "create model: invalid base url: parse \"\": empty url")
	if got.Class != ClassConfiguration || got.Cause != CauseBaseURL {
		t.Fatalf("class/cause = %q/%q, want %q/%q", got.Class, got.Cause, ClassConfiguration, CauseBaseURL)
	}
}

// resolveConfiguration is the whole point of the change: the same error text is
// three different fixes depending on what the project actually has configured.
func TestResolveConfigurationCopy(t *testing.T) {
	fallback := Classify(QueueDiscovery, "no generative model configured for project 0b1e")

	tests := []struct {
		name        string
		state       modelstate.State
		wantCause   FailureCause
		wantRetry   bool
		wantSummary string
		wantFix     string
	}{
		{
			name:        "no provider connected",
			state:       modelstate.State{},
			wantCause:   CauseNoProvider,
			wantRetry:   false,
			wantSummary: "No model provider is connected to this project.",
			wantFix:     "/settings/providers/new",
		},
		{
			name:        "provider connected, no default model",
			state:       modelstate.State{ProviderCount: 2},
			wantCause:   CauseNoDefaultModel,
			wantRetry:   false,
			wantSummary: "A provider is connected, but no default generative model is selected.",
			wantFix:     "/settings/providers",
		},
		{
			name:        "model resolves, so the configuration is not the problem",
			state:       modelstate.State{GenerativeModel: "openai/deepseek-v4-flash", GenerativeSource: "project", ProviderCount: 1},
			wantCause:   CauseModelUnusable,
			wantRetry:   true,
			wantSummary: "A model is configured (openai/deepseek-v4-flash), but this run could not use it.",
			wantFix:     "/settings/providers",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveConfiguration(fallback, tc.state, modelstate.KindGenerative)
			if got.Cause != tc.wantCause {
				t.Fatalf("cause = %q, want %q", got.Cause, tc.wantCause)
			}
			if got.Retryable != tc.wantRetry {
				t.Fatalf("retryable = %v, want %v", got.Retryable, tc.wantRetry)
			}
			if got.Summary != tc.wantSummary {
				t.Fatalf("summary = %q, want %q", got.Summary, tc.wantSummary)
			}
			if !got.Verified {
				t.Fatal("verified = false: resolved copy was checked against project state")
			}
			if got.Fix == nil || got.Fix.Href != tc.wantFix {
				t.Fatalf("fix = %+v, want href %q", got.Fix, tc.wantFix)
			}
			if got.Remediation == "" || !strings.Contains(got.Remediation, "Providers") {
				t.Fatalf("remediation = %q, want it to name Providers", got.Remediation)
			}
		})
	}
}

// A model name the provider does not serve stays its own message even when a
// model resolves: the fix is the catalogue, not the model selection.
func TestResolveConfigurationKeepsModelUnavailable(t *testing.T) {
	got := resolveConfiguration(Classify(QueueDiscovery, "model not found"), modelstate.State{
		GenerativeModel:  "openai/gpt-5-not-real",
		GenerativeSource: "project",
		ProviderCount:    1,
	}, modelstate.KindGenerative)
	if got.Cause != CauseModelUnavailable {
		t.Fatalf("cause = %q, want %q", got.Cause, CauseModelUnavailable)
	}
	if !strings.Contains(got.Summary, "openai/gpt-5-not-real") {
		t.Fatalf("summary = %q, want it to name the model", got.Summary)
	}
	if !strings.Contains(got.Remediation, "catalog") {
		t.Fatalf("remediation = %q, want it to point at the model catalog", got.Remediation)
	}
}

// The resolver must not touch classes that describe the error itself.
func TestResolveConfigurationLeavesOtherClassesAlone(t *testing.T) {
	in := Classify(QueueObjectExtraction, "429 rate limit exceeded")
	got := resolveConfiguration(in, modelstate.State{GenerativeModel: "openai/gpt-4o"}, modelstate.KindGenerative)
	if got.Class != ClassProvider || got.Verified || got.Model != "" {
		t.Fatalf("failure = %+v, want the provider failure untouched", got)
	}
}

// Model is filled from the project state, so the row can name the model it is
// talking about even when the copy is about that model failing to be used.
func TestResolveConfigurationNamesTheModel(t *testing.T) {
	got := resolveConfiguration(Classify(QueueObjectExtraction, "no model configured"), modelstate.State{
		GenerativeModel:  "google-vertex/projects/p/locations/l/publishers/google/models/gemini-2.5-flash",
		GenerativeSource: "provider",
		ProviderCount:    1,
	}, modelstate.KindGenerative)
	if got.Model == "" {
		t.Fatal("model = empty, want the configured model")
	}
}

// Embedding models resolve through their own chain, so an embedding failure must
// be written from the embedding state — not told to select a generative model
// the project may already have.
func TestResolveConfigurationUsesTheEmbeddingModelFamily(t *testing.T) {
	state := modelstate.State{
		GenerativeModel:  "openai/deepseek-v4-flash",
		GenerativeSource: "project",
		ProviderCount:    1,
	}
	fallback := Classify(QueueChunkEmbedding, "no embedding model configured for project 0b1e")

	got := resolveConfiguration(fallback, state, modelKindOfQueue(QueueChunkEmbedding))
	if got.Cause != CauseNoDefaultModel {
		t.Fatalf("cause = %q, want %q", got.Cause, CauseNoDefaultModel)
	}
	if strings.Contains(got.Summary, "generative") {
		t.Fatalf("summary = %q, want the embedding family", got.Summary)
	}
	if !strings.Contains(got.Summary, "embedding") {
		t.Fatalf("summary = %q, want it to name the embedding model", got.Summary)
	}
	if got.Fix == nil || got.Fix.Label != "Select an embedding model" {
		t.Fatalf("fix = %+v, want the embedding selection", got.Fix)
	}
	if got.Model != "" {
		t.Fatalf("model = %q, want empty: no embedding model resolves", got.Model)
	}

	// With an embedding model resolved, the run failed anyway — and the row
	// names the embedding model, not the generative one.
	state.EmbeddingModel = "openai/text-embedding-3-small"
	state.EmbeddingSource = "project"
	got = resolveConfiguration(fallback, state, modelKindOfQueue(QueueChunkEmbedding))
	if got.Cause != CauseModelUnusable {
		t.Fatalf("cause = %q, want %q", got.Cause, CauseModelUnusable)
	}
	if !strings.Contains(got.Summary, "openai/text-embedding-3-small") {
		t.Fatalf("summary = %q, want it to name the embedding model", got.Summary)
	}
	if got.Model != "openai/text-embedding-3-small" {
		t.Fatalf("model = %q, want the embedding model", got.Model)
	}
}

// The rewrite picks the model family from the queue, applies it only to the
// classes that depend on state, and gives a permission failure the model it is
// about without touching the classifier's copy.
func TestApplyModelStateSelectsTheRightFamily(t *testing.T) {
	state := modelstate.State{
		GenerativeModel:  "openai/deepseek-v4-flash",
		GenerativeSource: "project",
		EmbeddingModel:   "openai/text-embedding-3-small",
		EmbeddingSource:  "project",
		ProviderCount:    1,
	}

	config := Problem{Queue: QueueDiscovery, Failure: Classify(QueueDiscovery, "no model configured")}
	embed := Problem{Queue: QueueChunkEmbedding, Failure: Classify(QueueChunkEmbedding, "no embedding model configured")}
	perm := Problem{Queue: QueueObjectExtraction, Failure: Classify(QueueObjectExtraction, "provider rejected the request: invalid api key")}
	sourcePerm := Problem{Queue: QueueSourceSync, Failure: Classify(QueueSourceSync, "permission denied reading source")}
	limit := Problem{Queue: QueueObjectExtraction, Failure: Classify(QueueObjectExtraction, "429 rate limit exceeded")}

	byArea := map[Area][]Problem{
		AreaDocuments: {embed, limit},
		AreaSchema:    {config},
		AreaSources:   {sourcePerm, perm},
	}
	applyModelState(byArea, state)

	if got := byArea[AreaSchema][0]; got.Failure.Model != state.GenerativeModel || !got.Failure.Verified {
		t.Fatalf("discovery failure = %+v, want the generative model and verified copy", got.Failure)
	}
	if got := byArea[AreaDocuments][0]; got.Failure.Model != state.EmbeddingModel {
		t.Fatalf("embedding failure = %+v, want the embedding model", got.Failure)
	}
	if got := byArea[AreaSources][0]; got.Failure.Model != "" || got.Failure.Verified {
		t.Fatalf("source permission failure = %+v, want no model: it is about the source", got.Failure)
	}
	if got := byArea[AreaSources][1]; got.Failure.Model != state.GenerativeModel {
		t.Fatalf("model permission failure = %+v, want the generative model so the row can test it", got.Failure)
	}
	if got := byArea[AreaDocuments][1]; got.Failure.Model != "" || got.Failure.Verified {
		t.Fatalf("limit failure = %+v, want it untouched", got.Failure)
	}
}

// needsModelState decides whether the state read happens at all: a report with
// no model-dependent row must not pay for (or fail on) it.
func TestNeedsModelState(t *testing.T) {
	if needsModelState(map[Area][]Problem{
		AreaDocuments: {{Queue: QueueDocumentParsing, Failure: Classify(QueueDocumentParsing, "unsupported file type")}},
	}) {
		t.Fatal("needsModelState = true for a report with no model-dependent failure")
	}
	if !needsModelState(map[Area][]Problem{
		AreaDocuments: {{Queue: QueueObjectExtraction, Failure: Classify(QueueObjectExtraction, "provider rejected the request: invalid api key")}},
	}) {
		t.Fatal("needsModelState = false for a permission failure on a model queue")
	}
	if needsModelState(map[Area][]Problem{
		AreaSources: {{Queue: QueueSourceSync, Failure: Classify(QueueSourceSync, "permission denied")}},
	}) {
		t.Fatal("needsModelState = true for a source permission failure, which is about the source")
	}
}
