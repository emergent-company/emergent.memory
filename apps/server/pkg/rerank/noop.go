package rerank

import "context"

// NoopReranker returns each document at its original index with a zero score,
// preserving input order. It is useful when reranking is disabled.
type NoopReranker struct {
	TopN int
}

// Rerank returns len(documents) results in input order (truncated to TopN).
func (n NoopReranker) Rerank(_ context.Context, _ string, documents []string) ([]Result, error) {
	results := make([]Result, len(documents))
	for i := range documents {
		results[i] = Result{Index: i, Score: 0}
	}
	if n.TopN > 0 && len(results) > n.TopN {
		results = results[:n.TopN]
	}
	return results, nil
}
