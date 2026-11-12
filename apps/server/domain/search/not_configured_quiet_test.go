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

// TestWarnEmbedFailureCoversEveryLeg pins the single guard all four search legs
// call. It exists because the legs embed independently on their standalone call
// paths (graph, text, relationship) as well as through embedQuery, so a change
// that only guarded one of them would otherwise pass unnoticed.
func TestWarnEmbedFailureCoversEveryLeg(t *testing.T) {
	legs := []string{
		"failed to generate query embedding, falling back to lexical-only search",
		"failed to generate query embedding for graph search",
		"failed to generate query embedding for text search",
		"failed to generate query embedding for relationship search",
	}

	for _, msg := range legs {
		t.Run(msg, func(t *testing.T) {
			var quiet bytes.Buffer
			svc := &Service{log: slog.New(slog.NewTextHandler(&quiet, nil))}
			svc.warnEmbedFailure(fmt.Errorf("resolver: %w", embeddings.ErrEmbeddingModelNotSet), msg)
			if quiet.Len() != 0 {
				t.Errorf("not-configured failure logged %q, want silence", quiet.String())
			}

			var loud bytes.Buffer
			svc = &Service{log: slog.New(slog.NewTextHandler(&loud, nil))}
			svc.warnEmbedFailure(errors.New("API error 429: quota exceeded"), msg)
			if !strings.Contains(loud.String(), msg) {
				t.Errorf("provider failure log = %q, want it to contain %q", loud.String(), msg)
			}
		})
	}
}
