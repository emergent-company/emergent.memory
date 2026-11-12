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
	docCalls   []string
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
	if resolve == nil {
		// The production resolver always returns a model when the project has one;
		// tests that do not care about resolution use a fixed model so caching is
		// still exercised.
		resolve = func(context.Context) string { return "test-model" }
	}
	return &CachedEmbeddingService{
		inner:        embedder,
		store:        store,
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

	// Every document was cached under the resolved model.
	for _, d := range docs {
		assert.True(t, store.has(svc.cacheKey("test-model", cacheFormatDocument, d)), "expected cache entry for %q", d)
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
	require.True(t, store.has(svcA.cacheKey("model-A", cacheFormatDocument, "same-text")))

	// The same text under model-B must MISS (different key) and hit the underlying
	// service again — proving vectors from different models never collide.
	if _, err := svcB.EmbedDocumentsWithUsage(context.Background(), []string{"same-text"}); err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, 2, embedder.batchCount(), "same text under a different model must not be served from cache")
	assert.True(t, store.has(svcB.cacheKey("model-B", cacheFormatDocument, "same-text")))
}

// EmbedDocumentWithUsage returns a vector that is deliberately distinguishable
// from the query one for the same text, the way a real provider does: that is
// what makes a cache-key collision visible as a wrong vector rather than as a
// coincidence.
func (f *fakeCachedEmbedder) EmbedDocumentWithUsage(_ context.Context, text string) (*vertex.EmbedResult, error) {
	f.mu.Lock()
	f.docCalls = append(f.docCalls, text)
	f.mu.Unlock()
	return &vertex.EmbedResult{Embedding: append(vecForDoc(text), 1), Model: "test", Provider: "vertex"}, nil
}

// docCount reports how many single-document embeddings reached the underlying
// service (cache misses), which is how the cache tests observe a format miss.
func (f *fakeCachedEmbedder) docCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.docCalls)
}

// TestCachedEmbeddingService_FormatsDoNotShareCacheEntries is the regression
// test for issue #615: a document and a query of the same text are embedded
// differently, so they must not share a cache entry. Before the format was part
// of the key, whichever call landed first was served to the other -- and the
// corpus would silently be searched with a query-format vector, or a search
// would be answered with a corpus vector.
func TestCachedEmbeddingService_FormatsDoNotShareCacheEntries(t *testing.T) {
	embedder := &fakeCachedEmbedder{}
	store := newFakeEmbeddingCache()
	svc := newCachedServiceForTest(embedder, store, nil)
	ctx := context.Background()

	const text = "the same words"

	query, err := svc.EmbedQueryWithUsage(ctx, text)
	require.NoError(t, err)

	doc, err := svc.EmbedDocumentWithUsage(ctx, text)
	require.NoError(t, err)

	require.Equal(t, 1, embedder.docCount(), "the document embedding must miss the query entry, not reuse it")
	assert.NotEqual(t, query.Embedding, doc.Embedding, "a query and a document of the same text must not be the same vector")

	// Second calls are cache hits, each in its own format.
	if _, err := svc.EmbedQueryWithUsage(ctx, text); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EmbedDocumentWithUsage(ctx, text); err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, 1, embedder.docCount(), "a repeated document embedding must be served from cache")

	assert.True(t, store.has(svc.cacheKey("test-model", cacheFormatQuery, text)))
	assert.True(t, store.has(svc.cacheKey("test-model", cacheFormatDocument, text)))
}

// TestCachedEmbeddingService_NoResolvedModelBypassesCache pins that an
// unresolved model never produces a cache entry: without a model the key would
// collide across providers, and the inner service is the one that reports the
// missing configuration.
func TestCachedEmbeddingService_NoResolvedModelBypassesCache(t *testing.T) {
	embedder := &fakeCachedEmbedder{}
	store := newFakeEmbeddingCache()
	svc := newCachedServiceForTest(embedder, store, func(context.Context) string { return "" })

	if _, err := svc.EmbedDocumentsWithUsage(context.Background(), []string{"alpha"}); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := svc.EmbedDocumentsWithUsage(context.Background(), []string{"alpha"}); err != nil {
		t.Fatalf("second call: %v", err)
	}

	assert.Equal(t, 2, embedder.batchCount(), "an unresolved model must bypass the cache")
	assert.Empty(t, store.m, "no cache entry may be written without a resolved model")
}
