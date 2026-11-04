package embeddings

import (
	"testing"

	embgenai "github.com/emergent-company/emergent.memory/pkg/embeddings/genai"
	embopenai "github.com/emergent-company/emergent.memory/pkg/embeddings/openai"
	"github.com/emergent-company/emergent.memory/pkg/embeddings/vertex"
)

// TestUsageReportingDispatch verifies that the clients selected by
// resolveClientWithMeta satisfy usageReportingClient, so
// EmbedQueryWithUsage/EmbedDocumentsWithUsage take the usage-reporting branch
// instead of the plain fallback.
func TestUsageReportingDispatch(t *testing.T) {
	tests := []struct {
		name      string
		client    any
		wantUsage bool
	}{
		{
			name:      "googleai genai client reports usage",
			client:    (*embgenai.Client)(nil),
			wantUsage: true,
		},
		{
			name:      "openai-compatible client reports usage",
			client:    (*embopenai.Client)(nil),
			wantUsage: true,
		},
		{
			name:      "vertex client reports usage",
			client:    (*vertex.Client)(nil),
			wantUsage: true,
		},
		{
			name:      "noop client falls back",
			client:    NewNoopClient(),
			wantUsage: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := tt.client.(usageReportingClient)
			if ok != tt.wantUsage {
				t.Errorf("usageReportingClient assertion = %v, want %v", ok, tt.wantUsage)
			}
		})
	}
}
