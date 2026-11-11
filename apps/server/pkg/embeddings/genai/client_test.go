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

// recordingEmbedCaller captures the exact request the client builds, so tests
// can assert request shape without calling the real Google API.
type recordingEmbedCaller struct {
	calls []embedCall
}

type embedCall struct {
	model     string
	text      string
	taskType  string
	outputDim int32
}

func (r *recordingEmbedCaller) EmbedContent(_ context.Context, model string, contents []*genai.Content, cfg *genai.EmbedContentConfig) (*genai.EmbedContentResponse, error) {
	text := ""
	if len(contents) > 0 && len(contents[0].Parts) > 0 {
		text = contents[0].Parts[0].Text
	}
	call := embedCall{model: model, text: text}
	if cfg != nil {
		call.taskType = cfg.TaskType
		if cfg.OutputDimensionality != nil {
			call.outputDim = *cfg.OutputDimensionality
		}
	}
	r.calls = append(r.calls, call)
	return &genai.EmbedContentResponse{
		Embeddings: []*genai.ContentEmbedding{{Values: []float32{0.1, 0.2}}},
	}, nil
}

// TestEmbedRequestTaskInstructionPerModelFamily locks the request shape per
// model family: gemini-embedding-2 must carry the documented task prefix in the
// text and no task_type, while the families Google documents task_type for keep
// sending the field unchanged.
func TestEmbedRequestTaskInstructionPerModelFamily(t *testing.T) {
	const (
		queryText = "what is the capital of France?"
		docText   = "Paris is the capital of France."
	)

	tests := []struct {
		name string
		// model is the id configured on the client (what the catalog stores).
		model string
		// taskPrefix is true when the model takes its task in the text.
		taskPrefix bool
	}{
		{"gemini-embedding-2", "gemini-embedding-2", true},
		{"gemini-embedding-2 with models/ prefix", "models/gemini-embedding-2", true},
		{"gemini-embedding-2-preview", "gemini-embedding-2-preview", false},
		{"gemini-embedding-001", "gemini-embedding-001", false},
		{"text-embedding-004", "text-embedding-004", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			c, err := NewClient(ctx, Config{APIKey: "test-key", Model: tt.model})
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			rec := &recordingEmbedCaller{}
			c.models = rec

			if _, err := c.EmbedQueryWithUsage(ctx, queryText); err != nil {
				t.Fatalf("EmbedQueryWithUsage() error = %v", err)
			}
			if _, err := c.EmbedDocumentsWithUsage(ctx, []string{docText}); err != nil {
				t.Fatalf("EmbedDocumentsWithUsage() error = %v", err)
			}

			if len(rec.calls) != 2 {
				t.Fatalf("embed calls = %d, want 2 (one query, one document)", len(rec.calls))
			}

			wantQueryTaskType := "RETRIEVAL_QUERY"
			wantDocTaskType := "RETRIEVAL_DOCUMENT"
			wantQueryText := queryText
			wantDocText := docText
			if tt.taskPrefix {
				wantQueryTaskType = ""
				wantDocTaskType = ""
				wantQueryText = "task: search result | query: " + queryText
				wantDocText = "title: none | text: " + docText
			}

			query, doc := rec.calls[0], rec.calls[1]

			if query.model != tt.model || doc.model != tt.model {
				t.Errorf("request models = (%q, %q), want both %q", query.model, doc.model, tt.model)
			}
			if query.taskType != wantQueryTaskType {
				t.Errorf("query task_type = %q, want %q", query.taskType, wantQueryTaskType)
			}
			if doc.taskType != wantDocTaskType {
				t.Errorf("document task_type = %q, want %q", doc.taskType, wantDocTaskType)
			}
			if query.text != wantQueryText {
				t.Errorf("query text = %q, want %q", query.text, wantQueryText)
			}
			if doc.text != wantDocText {
				t.Errorf("document text = %q, want %q", doc.text, wantDocText)
			}
			if query.outputDim != 768 || doc.outputDim != 768 {
				t.Errorf("output dimensionality = (%d, %d), want 768 (kb vector(768) columns)", query.outputDim, doc.outputDim)
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
