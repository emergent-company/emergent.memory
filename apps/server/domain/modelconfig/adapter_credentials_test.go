package modelconfig

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/domain/provider"
	"github.com/emergent-company/emergent.memory/pkg/embeddings"
)

// TestEmbeddingCredentialErrorClassifiesGaps pins the fix that makes a missing
// project credential behave like a missing model: a project whose model config
// names a provider it has no credential for (e.g. configure-project google,
// set-models --embedding google/..., configure-project google --remove) must
// produce a not-configured sentinel so callers stay quiet and extraction treats
// the job as permanent, instead of logging per call and retrying to
// max_attempts. A transient failure must keep its own wrapping.
func TestEmbeddingCredentialErrorClassifiesGaps(t *testing.T) {
	gap := provider.ErrNoProjectProviderConfig
	wrappedGap := fmt.Errorf("lookup: %w", gap)

	got := embeddingCredentialError("google", wrappedGap)
	if !errors.Is(got, embeddings.ErrEmbeddingCredentialUnusable) {
		t.Errorf("missing credential error = %v, want it to wrap ErrEmbeddingCredentialUnusable", got)
	}
	if !embeddings.IsNotConfigured(got) {
		t.Errorf("IsNotConfigured(%v) = false, want true", got)
	}
	if !strings.Contains(got.Error(), "google") {
		t.Errorf("error = %q, want it to name the provider", got)
	}

	transient := errors.New("failed to get project provider config: connection refused")
	got = embeddingCredentialError("google", transient)
	if errors.Is(got, embeddings.ErrEmbeddingCredentialUnusable) {
		t.Errorf("transient error = %v, must not be classified as a configuration gap", got)
	}
	if embeddings.IsNotConfigured(got) {
		t.Errorf("IsNotConfigured(%v) = true, want false", got)
	}
	if !errors.Is(got, transient) {
		t.Errorf("transient error = %v, want it to wrap the cause", got)
	}
}
