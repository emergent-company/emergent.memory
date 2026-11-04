package extraction

import (
	"testing"

	"github.com/emergent-company/emergent.memory/domain/processingproblems"
)

func ptr(s string) *string { return &s }

func TestFilterRetryableDropsDeterministicFailures(t *testing.T) {
	candidates := []retryCandidate{
		{ID: "provider", LastError: ptr("429 rate limit exceeded")},
		{ID: "config", LastError: ptr("no generative model configured")},
		{ID: "permission", LastError: ptr("401 unauthorized")},
		{ID: "input", LastError: ptr("no such file or directory")},
		{ID: "output", LastError: ptr("invalid json: unexpected end of json")},
		{ID: "empty", LastError: nil},
	}

	for _, queue := range []string{
		processingproblems.QueueObjectExtraction,
		processingproblems.QueueChunkEmbedding,
		processingproblems.QueueGraphEmbedding,
		processingproblems.QueueDocumentParsing,
	} {
		t.Run(queue, func(t *testing.T) {
			kept := filterRetryable(queue, candidates)
			got := make(map[string]bool, len(kept))
			for _, candidate := range kept {
				got[candidate.ID] = true
			}
			for _, want := range []string{"provider", "output"} {
				if !got[want] {
					t.Errorf("queue %s dropped retryable candidate %q", queue, want)
				}
			}
			for _, want := range []string{"config", "permission", "input"} {
				if got[want] {
					t.Errorf("queue %s kept non-retryable candidate %q", queue, want)
				}
			}
			// An empty message falls back to the queue's empty failure class.
			// Embedding queues classify that as internal (retryable), parsing as
			// input (not retryable).
			if queue == processingproblems.QueueDocumentParsing && got["empty"] {
				t.Errorf("queue %s kept a nil-error parsing candidate", queue)
			}
		})
	}
}

func TestRetryableObjectExtraction(t *testing.T) {
	if !retryableObjectExtraction(&ObjectExtractionJob{ErrorMessage: ptr("503 service unavailable")}) {
		t.Error("provider failure should be retryable")
	}
	if retryableObjectExtraction(&ObjectExtractionJob{ErrorMessage: ptr("no model configured")}) {
		t.Error("missing model must not be retried unchanged")
	}
}
