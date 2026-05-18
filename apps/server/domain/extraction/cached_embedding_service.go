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

	"github.com/emergent-company/emergent.memory/pkg/embeddings/vertex"
	"github.com/emergent-company/emergent.memory/pkg/logger"
	"github.com/uptrace/bun"
)

// CachedEmbeddingService wraps an EmbeddingService and caches results in kb.embedding_cache.
// Cache key is sha256(modelID + ":" + inputText). Misses call through to the inner service.
type CachedEmbeddingService struct {
	inner   EmbeddingService
	db      bun.IDB
	modelID string
	log     *slog.Logger
}

// embeddingCacheRow is the ORM model for kb.embedding_cache.
type embeddingCacheRow struct {
	bun.BaseModel `bun:"table:kb.embedding_cache"`
	CacheKey      string          `bun:"cache_key,pk"`
	ModelID       string          `bun:"model_id,notnull"`
	Embedding     json.RawMessage `bun:"embedding,type:jsonb,notnull"`
}

// NewCachedEmbeddingService creates a wrapper around inner that caches embeddings in Postgres.
// modelID identifies the embedding model for cache invalidation (e.g., "text-embedding-004").
func NewCachedEmbeddingService(inner EmbeddingService, db bun.IDB, modelID string, log *slog.Logger) *CachedEmbeddingService {
	return &CachedEmbeddingService{
		inner:   inner,
		db:      db,
		modelID: modelID,
		log:     log.With(logger.Scope("cached-embedding")),
	}
}

// IsEnabled delegates to the inner service.
func (c *CachedEmbeddingService) IsEnabled() bool {
	return c.inner.IsEnabled()
}

// EmbedQuery returns a cached embedding when available, otherwise calls the inner service
// and stores the result.
func (c *CachedEmbeddingService) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	key := c.cacheKey(query)

	// Try cache first.
	var row embeddingCacheRow
	err := c.db.NewSelect().Model(&row).Where("cache_key = ?", key).Scan(ctx)
	if err == nil {
		// Cache hit — decode JSON array back to []float32.
		var vec []float32
		if jsonErr := json.Unmarshal(row.Embedding, &vec); jsonErr == nil {
			return vec, nil
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		// Log unexpected DB errors but don't fail — fall through to inner service.
		c.log.Warn("embedding cache read failed", slog.String("key", key), logger.Error(err))
	}

	// Cache miss — call inner service.
	vec, err := c.inner.EmbedQuery(ctx, query)
	if err != nil {
		return nil, err
	}

	// Persist to cache (best-effort; ignore conflict if another goroutine beat us).
	raw, jsonErr := json.Marshal(vec)
	if jsonErr == nil {
		rec := &embeddingCacheRow{CacheKey: key, ModelID: c.modelID, Embedding: raw}
		if _, insErr := c.db.NewInsert().Model(rec).
			On("CONFLICT (cache_key) DO NOTHING").
			Exec(ctx); insErr != nil {
			c.log.Warn("embedding cache write failed", slog.String("key", key), logger.Error(insErr))
		}
	}

	return vec, nil
}

// EmbedQueryWithUsage delegates to the inner service (usage tracking not cached).
func (c *CachedEmbeddingService) EmbedQueryWithUsage(ctx context.Context, query string) (*vertex.EmbedResult, error) {
	return c.inner.EmbedQueryWithUsage(ctx, query)
}

// cacheKey returns a deterministic SHA-256 hex key for the given input text.
func (c *CachedEmbeddingService) cacheKey(text string) string {
	h := sha256.New()
	h.Write([]byte(fmt.Sprintf("%s:%s", c.modelID, text)))
	return hex.EncodeToString(h.Sum(nil))
}
