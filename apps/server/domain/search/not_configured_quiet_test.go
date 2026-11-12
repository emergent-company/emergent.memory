package search

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/pkg/embeddings"
)

// failingEmbedder returns a fixed error from every EmbedQuery call.
type failingEmbedder struct {
	err error
}

func (f *failingEmbedder) EmbedQuery(context.Context, string) ([]float32, error) {
	return nil, f.err
}

// TestEmbedQueryWarnsOnlyForProviderFailures pins the #682 rule for unified
// search: a project with no usable embedding configuration is already reported
// once per project by pkg/embeddings, so the per-query path must stay quiet —
// while a real provider failure (timeout, quota, bad credentials) is still
// logged per call.
func TestEmbedQueryWarnsOnlyForProviderFailures(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantWarn bool
	}{
		{
			name:     "no embedding model configured",
			err:      fmt.Errorf("embedding credential resolver failed: %w for project p", embeddings.ErrEmbeddingModelNotSet),
			wantWarn: false,
		},
		{
			name:     "unusable provider credential",
			err:      fmt.Errorf("embedding credential resolver failed: %w", embeddings.ErrEmbeddingCredentialUnusable),
			wantWarn: false,
		},
		{
			name:     "no project context",
			err:      embeddings.ErrNoProjectContext,
			wantWarn: false,
		},
		{
			name:     "provider rate limit",
			err:      errors.New("API error 429: quota exceeded"),
			wantWarn: true,
		},
		{
			name:     "provider timeout",
			err:      errors.New("dial tcp: i/o timeout"),
			wantWarn: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			svc := &Service{
				embeddings: &failingEmbedder{err: tt.err},
				log:        slog.New(slog.NewTextHandler(&buf, nil)),
			}

			if vec := svc.embedQuery(context.Background(), "query"); vec != nil {
				t.Fatalf("embedQuery() = %v, want nil (lexical-only fallback)", vec)
			}

			warned := strings.Contains(buf.String(), "failed to generate query embedding")
			if warned != tt.wantWarn {
				t.Errorf("warned = %v, want %v (log: %s)", warned, tt.wantWarn, buf.String())
			}
		})
	}
}
