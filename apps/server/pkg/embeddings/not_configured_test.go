package embeddings

import (
	"errors"
	"fmt"
	"testing"
)

// TestIsNotConfigured pins the classification callers use to stay quiet: the
// three fail-closed configuration sentinels are "not configured" no matter how
// many layers wrap them, and a provider failure is not.
func TestIsNotConfigured(t *testing.T) {
	wrapped := fmt.Errorf("embedding credential resolver failed: %w",
		fmt.Errorf("%w for provider %q", ErrEmbeddingCredentialUnusable, "google"))

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"no project context", ErrNoProjectContext, true},
		{"no embedding model", ErrEmbeddingModelNotSet, true},
		{"unusable credential", ErrEmbeddingCredentialUnusable, true},
		{"wrapped unusable credential", wrapped, true},
		{"adapter wrap", fmt.Errorf("embedding resolver: %w", ErrEmbeddingCredentialUnusable), true},
		{"rate limit", errors.New("API error 429: quota exceeded"), false},
		{"unauthorized", errors.New("API error 401: invalid credentials"), false},
		{"timeout", errors.New("dial tcp: i/o timeout"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNotConfigured(tt.err); got != tt.want {
				t.Errorf("IsNotConfigured(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
