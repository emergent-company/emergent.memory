package search

import (
	"context"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/graph"
)

// blockingEmbedder never returns a vector: every EmbedQuery call blocks until
// the context is done. This lets a test prove each call is bounded by the
// context deadline the caller supplies (production: queryEmbedTimeout).
type blockingEmbedder struct {
	calls int32
}

func (b *blockingEmbedder) EmbedQuery(ctx context.Context, _ string) ([]float32, error) {
	atomic.AddInt32(&b.calls, 1)
	<-ctx.Done()
	return nil, ctx.Err()
}

// autoEmbeddingGraph fakes graph.Service for the embedding-bound test. It
// replicates the production auto-embed contract: when no vector is supplied and
// auto-embed is not disabled, HybridSearch embeds the query itself with the
// caller's context only (the pre-#1405 hole allowed this to outlive the bound).
type autoEmbeddingGraph struct {
	embedder       queryEmbedder
	autoEmbedCalls int32
}

func (g *autoEmbeddingGraph) HybridSearch(ctx context.Context, _ uuid.UUID, req *graph.HybridSearchRequest, _ *graph.HybridSearchOptions) (*graph.SearchResponse, error) {
	vectorWeight := float32(0.5)
	if req.VectorWeight != nil {
		vectorWeight = *req.VectorWeight
	}
	if !req.DisableAutoEmbed && req.Query != "" && len(req.Vector) == 0 && vectorWeight > 0 {
		atomic.AddInt32(&g.autoEmbedCalls, 1)
		_, _ = g.embedder.EmbedQuery(ctx, req.Query)
	}
	return &graph.SearchResponse{Data: []*graph.SearchResultItem{}}, nil
}

func (g *autoEmbeddingGraph) GetEdgesBatch(context.Context, uuid.UUID, []uuid.UUID, graph.GetEdgesParams) (map[uuid.UUID]*graph.GetObjectEdgesResponse, error) {
	return nil, nil
}

func (g *autoEmbeddingGraph) UpdateAccessTimestamps(context.Context, []uuid.UUID) error { return nil }

// TestUnifiedSearchEmbeddingStagesAreBounded proves the #1405 fix: the unified
// search bounds every embedding attempt, and the graph leg no longer fires a
// third, unbounded auto-embed once the shared embed and its own bounded
// re-embed have both failed. Run with `resultTypes: graph` so the text leg
// (which needs a repository) is skipped.
//
// Failing before the fix: executeGraphSearch left DisableAutoEmbed false, so the
// fake graph auto-embedded with the caller's 1s deadline — elapsed far beyond
// the tight bound and autoEmbedCalls == 1.
func TestUnifiedSearchEmbeddingStagesAreBounded(t *testing.T) {
	prev := queryEmbedTimeout
	queryEmbedTimeout = 50 * time.Millisecond
	t.Cleanup(func() { queryEmbedTimeout = prev })

	emb := &blockingEmbedder{}
	g := &autoEmbeddingGraph{embedder: emb}
	svc := &Service{
		graphService: g,
		embeddings:   emb,
		log:          slog.New(slog.NewTextHandler(os.Stdout, nil)),
		defaultLimit: 32,
		maxLimit:     100,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	start := time.Now()
	resp, err := svc.Search(ctx, uuid.New(), &UnifiedSearchRequest{
		Query:       "quarterly report",
		ResultTypes: ResultTypeGraph,
	}, nil)
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.NotNil(t, resp)

	// Exactly the two bounded stages: the shared pre-computed embed and the
	// graph leg's own bounded re-embed. A third call means the hole reopened.
	assert.Equal(t, int32(2), atomic.LoadInt32(&emb.calls),
		"unified search must issue exactly the two bounded embed attempts")
	assert.Equal(t, int32(0), atomic.LoadInt32(&g.autoEmbedCalls),
		"graph leg must not auto-embed once unified search is out of vectors")

	// Two sequential 50ms bounded stages (~100ms), nowhere near the 1s caller
	// deadline that the old unbounded third attempt would have consumed.
	assert.Less(t, elapsed, 300*time.Millisecond,
		"embedding stages must settle within the bound, not the caller deadline")
}

// TestHybridSearchRequestFromUnifiedSuppressesGraphAutoEmbed pins the wiring:
// with no vector the graph request carries DisableAutoEmbed, and with a vector
// it is left unset so hybrid search is unaffected.
func TestHybridSearchRequestFromUnifiedSuppressesGraphAutoEmbed(t *testing.T) {
	req := &UnifiedSearchRequest{Query: "q", Limit: 5}

	withoutVector := hybridSearchRequestFromUnified(req, nil)
	assert.True(t, withoutVector.DisableAutoEmbed,
		"no vector: suppress the graph leg's redundant auto-embed")
	assert.Nil(t, withoutVector.VectorWeight,
		"weights stay at defaults so lexical-only scoring is unchanged")

	withVector := hybridSearchRequestFromUnified(req, []float32{0.1, 0.2})
	assert.False(t, withVector.DisableAutoEmbed,
		"vector available: auto-embed is irrelevant and left unset")
	assert.Equal(t, []float32{0.1, 0.2}, withVector.Vector)
}
