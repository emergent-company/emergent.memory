package embeddings

import (
	"context"
	"crypto/sha256"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// TestLLMChecker reports whether a project has deterministic test-LLM mode
// enabled. When true, the embeddings service returns deterministic vectors
// without calling a real provider.
type TestLLMChecker interface {
	IsTestLLM(ctx context.Context, projectID string) bool
}

// cannedEmbedding returns a deterministic embedding vector for text.
// The same text always produces the same vector, and different texts produce
// different vectors — sufficient for deterministic e2e runs without a provider.
func cannedEmbedding(text string) []float32 {
	sum := sha256.Sum256([]byte(text))
	vec := make([]float32, EmbeddingDimension)
	for i := range vec {
		vec[i] = float32(sum[i%len(sum)]) / 255.0
	}
	return vec
}

// isTestLLM returns true when the project in ctx has test-LLM mode enabled.
func (s *Service) isTestLLM(ctx context.Context) bool {
	if s.testLLM == nil {
		return false
	}
	pid := auth.ProjectIDFromContext(ctx)
	if pid == "" {
		return false
	}
	return s.testLLM.IsTestLLM(ctx, pid)
}
