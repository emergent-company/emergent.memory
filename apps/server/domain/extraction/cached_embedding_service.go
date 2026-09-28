package extraction

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/embeddings/vertex"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// cachedInner is the embedding interface CachedEmbeddingService requires of its
// wrapped service: single-query embedding plus multi-document batch embedding.
// *embeddings.Service satisfies it.
type cachedInner interface {
	EmbeddingService
	batchEmbeddingService
}

// CachedEmbeddingService wraps an EmbeddingService and caches results in
// kb.embedding_cache. The cache key is sha256(modelID + ":" + inputText), where
// modelID is the effective embedding model for the request (resolved per-request
// when a model resolver is wired, falling back to the static configured model).
// Misses call through to the inner service.
type CachedEmbeddingService struct {
	inner        cachedInner
	store        embeddingCacheStore
	modelID      string                       // static fallback model (env default)
	resolveModel func(context.Context) string // optional per-request model resolver
	log          *slog.Logger
}

// embeddingCacheStore is the persistence seam for cached embedding vectors,
// abstracted from bun so the caching logic can be unit-tested without Postgres.
type embeddingCacheStore interface {
	// Get returns the cached vector for key, or (nil, false, nil) on a miss.
	Get(ctx context.Context, key string) ([]float32, bool, error)
	// Put stores the vector under key, ignoring conflicts (best-effort).
	Put(ctx context.Context, key, modelID string, vec []float32) error
}

// embeddingCacheRow is the ORM model for kb.embedding_cache.
type embeddingCacheRow struct {
	bun.BaseModel `bun:"table:kb.embedding_cache"`
	CacheKey      string          `bun:"cache_key,pk"`
	ModelID       string          `bun:"model_id,notnull"`
	Embedding     json.RawMessage `bun:"embedding,type:jsonb,notnull"`
}

// bunEmbeddingCache persists cached embeddings to kb.embedding_cache via bun.
type bunEmbeddingCache struct {
	db bun.IDB
}

func newBunEmbeddingCache(db bun.IDB) *bunEmbeddingCache {
	return &bunEmbeddingCache{db: db}
}

func (b *bunEmbeddingCache) Get(ctx context.Context, key string) ([]float32, bool, error) {
	var row embeddingCacheRow
	err := b.db.NewSelect().Model(&row).Where("cache_key = ?", key).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var vec []float32
	if jsonErr := json.Unmarshal(row.Embedding, &vec); jsonErr != nil {
		// Corrupt/unreadable cached value — treat as a miss.
		return nil, false, nil
	}
	return vec, true, nil
}

func (b *bunEmbeddingCache) Put(ctx context.Context, key, modelID string, vec []float32) error {
	raw, err := json.Marshal(vec)
	if err != nil {
		return err
	}
	rec := &embeddingCacheRow{CacheKey: key, ModelID: modelID, Embedding: raw}
	_, err = b.db.NewInsert().Model(rec).
		On("CONFLICT (cache_key) DO NOTHING").
		Exec(ctx)
	return err
}

// NewCachedEmbeddingService creates a wrapper around inner that caches embeddings in Postgres.
// modelID identifies the embedding model for cache invalidation (e.g., "text-embedding-004").
func NewCachedEmbeddingService(inner cachedInner, db bun.IDB, modelID string, log *slog.Logger) *CachedEmbeddingService {
	return &CachedEmbeddingService{
		inner:   inner,
		store:   newBunEmbeddingCache(db),
		modelID: modelID,
		log:     log.With(logger.Scope("cached-embedding")),
	}
}

// WithModelResolver wires a per-request embedding model resolver. When set, the
// cache key is derived from the model resolved for the request (falling back to
// the static modelID when resolution fails or returns empty). This guarantees
// the cache never mixes vectors produced by different embedding models, even
// when a project overrides the embedding model away from the env default.
func (c *CachedEmbeddingService) WithModelResolver(resolve func(context.Context) string) *CachedEmbeddingService {
	c.resolveModel = resolve
	return c
}

// resolveModelID returns the effective embedding model id for this request,
// preferring the per-request resolved model over the static fallback.
func (c *CachedEmbeddingService) resolveModelID(ctx context.Context) string {
	if c.resolveModel != nil {
		if m := c.resolveModel(ctx); m != "" {
			return m
		}
	}
	return c.modelID
}

// IsEnabled delegates to the inner service.
func (c *CachedEmbeddingService) IsEnabled() bool {
	return c.inner.IsEnabled()
}

// EmbedQuery returns a cached embedding when available, otherwise calls the inner service
// and stores the result.
func (c *CachedEmbeddingService) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	model := c.resolveModelID(ctx)
	key := c.cacheKey(model, query)

	// Try cache first.
	if vec, ok, err := c.store.Get(ctx, key); err == nil && ok {
		return vec, nil
	} else if err != nil {
		// Log unexpected DB errors but don't fail — fall through to inner service.
		c.log.Warn("embedding cache read failed", slog.String("key", key), logger.Error(err))
	}

	// Cache miss — call inner service.
	vec, err := c.inner.EmbedQuery(ctx, query)
	if err != nil {
		return nil, err
	}

	// Persist to cache (best-effort; ignore conflict if another goroutine beat us).
	if putErr := c.store.Put(ctx, key, model, vec); putErr != nil {
		c.log.Warn("embedding cache write failed", slog.String("key", key), logger.Error(putErr))
	}

	return vec, nil
}

// EmbedQueryWithUsage returns a cached embedding when available, otherwise calls
// the inner service and stores the result. Usage tracking is not cached — a cache
// hit reports no usage (no tokens were consumed), while a miss propagates the
// inner result's usage, model, and provider.
func (c *CachedEmbeddingService) EmbedQueryWithUsage(ctx context.Context, query string) (*vertex.EmbedResult, error) {
	model := c.resolveModelID(ctx)
	key := c.cacheKey(model, query)

	// Try cache first.
	if vec, ok, err := c.store.Get(ctx, key); err == nil && ok {
		return &vertex.EmbedResult{Embedding: vec, Model: model}, nil
	} else if err != nil {
		c.log.Warn("embedding cache read failed", slog.String("key", key), logger.Error(err))
	}

	// Cache miss — call inner service.
	result, err := c.inner.EmbedQueryWithUsage(ctx, query)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}

	if putErr := c.store.Put(ctx, key, model, result.Embedding); putErr != nil {
		c.log.Warn("embedding cache write failed", slog.String("key", key), logger.Error(putErr))
	}

	return result, nil
}

// EmbedDocumentsWithUsage embeds several documents in one pass, routing through
// the embedding cache: per-document cache hits are served without a call, misses
// are embedded in a single underlying batch call, and the returned vectors are
// ordered to match the input documents. Usage reflects only the misses (hits
// consume no tokens).
func (c *CachedEmbeddingService) EmbedDocumentsWithUsage(ctx context.Context, documents []string) (*vertex.BatchEmbedResult, error) {
	model := c.resolveModelID(ctx)

	out := make([][]float32, len(documents))
	missIdx := make([]int, 0, len(documents))
	missTexts := make([]string, 0, len(documents))

	for i, doc := range documents {
		key := c.cacheKey(model, doc)
		if vec, ok, err := c.store.Get(ctx, key); err == nil && ok {
			out[i] = vec
			continue
		} else if err != nil {
			c.log.Warn("embedding cache read failed", slog.String("key", key), logger.Error(err))
		}
		missIdx = append(missIdx, i)
		missTexts = append(missTexts, doc)
	}

	// All hits — no underlying call, no token usage.
	if len(missTexts) == 0 {
		return &vertex.BatchEmbedResult{Embeddings: out, Model: model}, nil
	}

	// Call the underlying service once for all misses.
	result, err := c.inner.EmbedDocumentsWithUsage(ctx, missTexts)
	if err != nil {
		return nil, err
	}
	if result == nil || len(result.Embeddings) != len(missTexts) {
		return nil, fmt.Errorf("embedding batch returned %d vectors for %d documents", embeddingVectorCount(result), len(missTexts))
	}

	// Place miss vectors back at their original positions and persist them.
	for k, i := range missIdx {
		out[i] = result.Embeddings[k]
		key := c.cacheKey(model, missTexts[k])
		if putErr := c.store.Put(ctx, key, model, result.Embeddings[k]); putErr != nil {
			c.log.Warn("embedding cache write failed", slog.String("key", key), logger.Error(putErr))
		}
	}

	return &vertex.BatchEmbedResult{
		Embeddings: out,
		Usage:      result.Usage,
		Model:      result.Model,
		Provider:   result.Provider,
	}, nil
}

// cacheKey returns a deterministic SHA-256 hex key for the given model and input text.
func (c *CachedEmbeddingService) cacheKey(modelID, text string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s:%s", modelID, text)
	return hex.EncodeToString(h.Sum(nil))
}
