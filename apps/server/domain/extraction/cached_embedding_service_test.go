package extraction

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/embeddings/vertex"
)

// fakeCachedEmbedder implements EmbeddingService + batchEmbeddingService,
// recording every batch call so tests can assert when the underlying service is
// (and isn't) invoked.
type fakeCachedEmbedder struct {
	mu         sync.Mutex
	batchCalls [][]string
}

func (f *fakeCachedEmbedder) IsEnabled() bool { return true }

func (f *fakeCachedEmbedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	r, err := f.EmbedQueryWithUsage(ctx, query)
	if err != nil {
		return nil, err
	}
	return r.Embedding, nil
}

func (f *fakeCachedEmbedder) EmbedQueryWithUsage(_ context.Context, query string) (*vertex.EmbedResult, error) {
	return &vertex.EmbedResult{Embedding: vecForDoc(query), Model: "test", Provider: "vertex"}, nil
}

func (f *fakeCachedEmbedder) EmbedDocumentsWithUsage(_ context.Context, documents []string) (*vertex.BatchEmbedResult, error) {
	f.mu.Lock()
	f.batchCalls = append(f.batchCalls, append([]string(nil), documents...))
	f.mu.Unlock()

	vecs := make([][]float32, len(documents))
	for i, d := range documents {
		vecs[i] = vecForDoc(d)
	}
	return &vertex.BatchEmbedResult{
		Embeddings: vecs,
		Usage:      &vertex.Usage{PromptTokens: len(documents), TotalTokens: len(documents)},
		Model:      "test",
		Provider:   "vertex",
	}, nil
}

func (f *fakeCachedEmbedder) batchCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.batchCalls)
}

// vecForDoc returns a deterministic, per-document vector so output order can be
// asserted.
func vecForDoc(d string) []float32 {
	return []float32{float32(len(d)), float32(d[0])}
}

// fakeEmbeddingCache is an in-memory embeddingCacheStore.
type fakeEmbeddingCache struct {
	mu sync.Mutex
	m  map[string][]float32
}

func newFakeEmbeddingCache() *fakeEmbeddingCache {
	return &fakeEmbeddingCache{m: make(map[string][]float32)}
}

func (f *fakeEmbeddingCache) Get(_ context.Context, key string) ([]float32, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.m[key]
	return v, ok, nil
}

func (f *fakeEmbeddingCache) Put(_ context.Context, key, _ string, vec []float32) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.m[key] = append([]float32(nil), vec...)
	return nil
}

func (f *fakeEmbeddingCache) has(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.m[key]
	return ok
}

func newCachedServiceForTest(embedder *fakeCachedEmbedder, store embeddingCacheStore, resolve func(context.Context) string) *CachedEmbeddingService {
	return &CachedEmbeddingService{
		inner:        embedder,
		store:        store,
		modelID:      "test-model",
		resolveModel: resolve,
		log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestCachedEmbeddingService_BatchMissCallsUnderlyingAndPopulatesCache(t *testing.T) {
	embedder := &fakeCachedEmbedder{}
	store := newFakeEmbeddingCache()
	svc := newCachedServiceForTest(embedder, store, nil)

	docs := []string{"alpha", "beta", "gamma"}
	result, err := svc.EmbedDocumentsWithUsage(context.Background(), docs)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Underlying called exactly once with the full document set.
	require.Equal(t, 1, embedder.batchCount())
	assert.Equal(t, [][]string{docs}, embedder.batchCalls)

	// Output order preserved.
	require.Len(t, result.Embeddings, len(docs))
	for i, d := range docs {
		assert.Equal(t, vecForDoc(d), result.Embeddings[i])
	}

	// Every document was cached under the resolved (static fallback) model.
	for _, d := range docs {
		assert.True(t, store.has(svc.cacheKey("test-model", d)), "expected cache entry for %q", d)
	}
}

func TestCachedEmbeddingService_BatchHitSkipsUnderlying(t *testing.T) {
	embedder := &fakeCachedEmbedder{}
	store := newFakeEmbeddingCache()
	svc := newCachedServiceForTest(embedder, store, nil)

	docs := []string{"alpha", "beta", "gamma"}
	if _, err := svc.EmbedDocumentsWithUsage(context.Background(), docs); err != nil {
		t.Fatal(err)
	}
	require.Equal(t, 1, embedder.batchCount())

	// Second call is fully served from cache — no underlying call.
	result, err := svc.EmbedDocumentsWithUsage(context.Background(), docs)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, embedder.batchCount(), "underlying service must not be called on a full cache hit")

	require.Len(t, result.Embeddings, len(docs))
	for i, d := range docs {
		assert.Equal(t, vecForDoc(d), result.Embeddings[i])
	}
}

func TestCachedEmbeddingService_BatchMixedHitMissPreservesOrder(t *testing.T) {
	embedder := &fakeCachedEmbedder{}
	store := newFakeEmbeddingCache()
	svc := newCachedServiceForTest(embedder, store, nil)

	// Pre-populate "beta" by embedding it once.
	if _, err := svc.EmbedDocumentsWithUsage(context.Background(), []string{"beta"}); err != nil {
		t.Fatal(err)
	}
	require.Equal(t, 1, embedder.batchCount())

	// Embed ["alpha", "beta", "gamma"]: alpha and gamma are misses, beta is a hit.
	docs := []string{"alpha", "beta", "gamma"}
	result, err := svc.EmbedDocumentsWithUsage(context.Background(), docs)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Only the misses were sent to the underlying service, in order.
	assert.Equal(t, 2, embedder.batchCount())
	assert.Equal(t, [][]string{{"alpha", "gamma"}}, embedder.batchCalls[1:])

	// Output order matches input order regardless of hit/miss interleaving.
	require.Len(t, result.Embeddings, len(docs))
	for i, d := range docs {
		assert.Equal(t, vecForDoc(d), result.Embeddings[i])
	}
}

func TestCachedEmbeddingService_KeysByResolvedModel(t *testing.T) {
	embedder := &fakeCachedEmbedder{}
	store := newFakeEmbeddingCache()

	svcA := newCachedServiceForTest(embedder, store, func(context.Context) string { return "model-A" })
	svcB := newCachedServiceForTest(embedder, store, func(context.Context) string { return "model-B" })

	// Embed the same text under model-A.
	if _, err := svcA.EmbedDocumentsWithUsage(context.Background(), []string{"same-text"}); err != nil {
		t.Fatal(err)
	}
	require.Equal(t, 1, embedder.batchCount())
	require.True(t, store.has(svcA.cacheKey("model-A", "same-text")))

	// The same text under model-B must MISS (different key) and hit the underlying
	// service again — proving vectors from different models never collide.
	if _, err := svcB.EmbedDocumentsWithUsage(context.Background(), []string{"same-text"}); err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, 2, embedder.batchCount(), "same text under a different model must not be served from cache")
	assert.True(t, store.has(svcB.cacheKey("model-B", "same-text")))
}
