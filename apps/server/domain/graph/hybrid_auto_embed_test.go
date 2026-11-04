package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestShouldAutoEmbedQuery pins the auto-embed decision that guards the
// unbounded embedding attempt inside HybridSearch (issue #1405). Unified search
// sets DisableAutoEmbed once it has exhausted its own bounded attempts, so the
// graph leg must not embed the query again.
func TestShouldAutoEmbedQuery(t *testing.T) {
	vw := float32(0.5)
	zero := float32(0)

	tests := []struct {
		name         string
		req          *HybridSearchRequest
		hasVector    bool
		vectorWeight float32
		want         bool
	}{
		{
			name:         "default hybrid query auto-embeds",
			req:          &HybridSearchRequest{Query: "q"},
			hasVector:    false,
			vectorWeight: vw,
			want:         true,
		},
		{
			name:         "DisableAutoEmbed suppresses the call",
			req:          &HybridSearchRequest{Query: "q", DisableAutoEmbed: true},
			hasVector:    false,
			vectorWeight: vw,
			want:         false,
		},
		{
			name:         "supplied vector never re-embeds",
			req:          &HybridSearchRequest{Query: "q", Vector: []float32{0.1}},
			hasVector:    true,
			vectorWeight: vw,
			want:         false,
		},
		{
			name:         "lexical-only mode never embeds",
			req:          &HybridSearchRequest{Query: "q"},
			hasVector:    false,
			vectorWeight: zero,
			want:         false,
		},
		{
			name:         "empty query never embeds",
			req:          &HybridSearchRequest{},
			hasVector:    false,
			vectorWeight: vw,
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, shouldAutoEmbedQuery(tt.req, tt.hasVector, tt.vectorWeight))
		})
	}
}
