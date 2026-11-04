package extraction

import (
	"testing"

	"github.com/emergent-company/emergent.memory/domain/provider"
	"github.com/emergent-company/emergent.memory/pkg/embeddings/vertex"
)

// fakeUsageRecorder captures embedding usage events for assertions.
type fakeUsageRecorder struct {
	events []*provider.LLMUsageEvent
}

func (f *fakeUsageRecorder) RecordAsync(event *provider.LLMUsageEvent) {
	f.events = append(f.events, event)
}

func TestRecordEmbeddingUsageMapsProvider(t *testing.T) {
	tests := []struct {
		name         string
		result       *vertex.EmbedResult
		wantProvider provider.ProviderType
		wantRecorded bool
	}{
		{
			name:         "vertex provider",
			result:       &vertex.EmbedResult{Embedding: []float32{1}, Usage: &vertex.Usage{PromptTokens: 10}, Provider: "vertex", Model: "gemini-embedding-001"},
			wantProvider: provider.ProviderVertexAI,
			wantRecorded: true,
		},
		{
			name:         "googleai provider",
			result:       &vertex.EmbedResult{Embedding: []float32{1}, Usage: &vertex.Usage{PromptTokens: 10}, Provider: "googleai", Model: "gemini-embedding-001"},
			wantProvider: provider.ProviderGoogleAI,
			wantRecorded: true,
		},
		{
			name:         "openai-compatible provider (LiteLLM)",
			result:       &vertex.EmbedResult{Embedding: []float32{1}, Usage: &vertex.Usage{PromptTokens: 10}, Provider: "openai", Model: "gemini-embedding-001"},
			wantProvider: provider.ProviderOpenAI,
			wantRecorded: true,
		},
		{
			name:         "nil usage skipped",
			result:       &vertex.EmbedResult{Embedding: []float32{1}, Usage: nil, Provider: "openai", Model: "gemini-embedding-001"},
			wantProvider: provider.ProviderOpenAI,
			wantRecorded: false,
		},
		{
			name:         "zero token usage skipped",
			result:       &vertex.EmbedResult{Embedding: []float32{1}, Usage: &vertex.Usage{PromptTokens: 0}, Provider: "openai", Model: "gemini-embedding-001"},
			wantProvider: provider.ProviderOpenAI,
			wantRecorded: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &fakeUsageRecorder{}
			recordEmbeddingUsage(rec, "proj-1", "org-1", tt.result)

			if !tt.wantRecorded {
				if len(rec.events) != 0 {
					t.Fatalf("recorded %d events, want 0", len(rec.events))
				}
				return
			}
			if len(rec.events) != 1 {
				t.Fatalf("recorded %d events, want 1", len(rec.events))
			}
			ev := rec.events[0]
			if ev.Provider != tt.wantProvider {
				t.Errorf("event.Provider = %q, want %q", ev.Provider, tt.wantProvider)
			}
			if ev.Model != tt.result.Model {
				t.Errorf("event.Model = %q, want %q", ev.Model, tt.result.Model)
			}
			if ev.Operation != provider.OperationEmbed {
				t.Errorf("event.Operation = %q, want %q", ev.Operation, provider.OperationEmbed)
			}
			if ev.TextInputTokens != int64(tt.result.Usage.PromptTokens) {
				t.Errorf("event.TextInputTokens = %d, want %d", ev.TextInputTokens, tt.result.Usage.PromptTokens)
			}
		})
	}
}

func TestRecordEmbeddingUsageNilRecorderAndResult(t *testing.T) {
	// nil recorder and nil/missing-context results must be safe no-ops.
	recordEmbeddingUsage(nil, "proj-1", "org-1", &vertex.EmbedResult{Embedding: []float32{1}, Usage: &vertex.Usage{PromptTokens: 5}, Provider: "openai"})
	recordEmbeddingUsage(&fakeUsageRecorder{}, "", "org-1", &vertex.EmbedResult{Embedding: []float32{1}, Usage: &vertex.Usage{PromptTokens: 5}, Provider: "openai"})
	recordEmbeddingUsage(&fakeUsageRecorder{}, "proj-1", "", &vertex.EmbedResult{Embedding: []float32{1}, Usage: &vertex.Usage{PromptTokens: 5}, Provider: "openai"})
	recordEmbeddingUsage(&fakeUsageRecorder{}, "proj-1", "org-1", nil)
}
