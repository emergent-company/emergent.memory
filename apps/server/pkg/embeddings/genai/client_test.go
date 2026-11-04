package genai

import (
	"context"
	"testing"

	"google.golang.org/genai"
)

func TestEmbeddingPromptTokens(t *testing.T) {
	tests := []struct {
		name string
		emb  *genai.ContentEmbedding
		want int
	}{
		{
			name: "nil embedding",
			emb:  nil,
			want: 0,
		},
		{
			name: "nil statistics",
			emb:  &genai.ContentEmbedding{Values: []float32{1, 2, 3}},
			want: 0,
		},
		{
			name: "statistics present",
			emb: &genai.ContentEmbedding{
				Values:     []float32{1, 2, 3},
				Statistics: &genai.ContentEmbeddingStatistics{TokenCount: 12},
			},
			want: 12,
		},
		{
			name: "statistics zero",
			emb: &genai.ContentEmbedding{
				Statistics: &genai.ContentEmbeddingStatistics{},
			},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := embeddingPromptTokens(tt.emb); got != tt.want {
				t.Errorf("embeddingPromptTokens() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestEmbedDocumentsWithUsage_Empty(t *testing.T) {
	ctx := context.Background()
	c, err := NewClient(ctx, Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	result, err := c.EmbedDocumentsWithUsage(ctx, nil)
	if err != nil {
		t.Fatalf("EmbedDocumentsWithUsage(nil) error = %v", err)
	}
	if result == nil {
		t.Fatal("EmbedDocumentsWithUsage(nil) = nil, want non-nil")
	}
	if len(result.Embeddings) != 0 {
		t.Errorf("len(Embeddings) = %d, want 0", len(result.Embeddings))
	}
	if result.Provider != "googleai" {
		t.Errorf("Provider = %q, want %q", result.Provider, "googleai")
	}
	if result.Model != DefaultModel {
		t.Errorf("Model = %q, want %q", result.Model, DefaultModel)
	}
}
