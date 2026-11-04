package graph

import (
	"context"
	"log/slog"
	"time"

	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// embeddingWarmTargets are the ANN (HNSW) indexes whose pages are evicted on
// restart, so the first vector search after a deploy pays seconds of disk reads
// (issue #1102). Each target also carries a bounded ANN fallback query used when
// the pg_prewarm extension is unavailable (or when prewarm fails).
//
// prewarm is the schema-qualified, correctly-quoted regclass argument for
// pg_prewarm. The object index is a quoted mixed-case identifier (migrations
// 00164/00175), so it must keep its double quotes to survive regclassin's
// downcasing; the relationships index is lowercase (migration 00171).
var embeddingWarmTargets = []struct {
	index    string
	prewarm  string
	fallback string
}{
	{
		index:   "IDX_graph_objects_embedding_v2_hnsw",
		prewarm: `kb."IDX_graph_objects_embedding_v2_hnsw"`,
		fallback: `SELECT id FROM kb.graph_objects
			WHERE embedding_v2 IS NOT NULL
			ORDER BY embedding_v2 <=> (SELECT embedding_v2 FROM kb.graph_objects WHERE embedding_v2 IS NOT NULL LIMIT 1)
			LIMIT 200`,
	},
	{
		index:   "idx_graph_relationships_embedding_hnsw",
		prewarm: `kb.idx_graph_relationships_embedding_hnsw`,
		fallback: `SELECT id FROM kb.graph_relationships
			WHERE embedding IS NOT NULL
			ORDER BY embedding <=> (SELECT embedding FROM kb.graph_relationships WHERE embedding IS NOT NULL LIMIT 1)
			LIMIT 200`,
	},
}

// RegisterIndexWarmup warms the embedding ANN indexes asynchronously on startup
// so the first vector search after a restart is not disk-bound (#1102).
func RegisterIndexWarmup(lc fx.Lifecycle, repo *Repository) {
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			// Never block startup; warm in the background.
			go warmEmbeddingIndexes(context.Background(), repo)
			return nil
		},
	})
}

// warmEmbeddingIndexes loads the ANN indexes into cache. It prefers
// pg_prewarm (full index load) and falls back to a bounded ANN query per table.
// Best-effort: any failure is logged and never surfaced to the caller.
func warmEmbeddingIndexes(ctx context.Context, repo *Repository) {
	log := repo.log.With(logger.Scope("graph.warmup"))

	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	var hasPrewarm bool
	if err := repo.db.NewRaw(
		`SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_prewarm')`,
	).Scan(ctx, &hasPrewarm); err != nil {
		log.Warn("index warmup: pg_prewarm probe failed", logger.Error(err))
		return
	}

	for _, target := range embeddingWarmTargets {
		start := time.Now()
		if hasPrewarm {
			var blocks int64
			prewarmErr := repo.db.NewRaw(`SELECT pg_prewarm(?::regclass)`, target.prewarm).Scan(ctx, &blocks)
			if prewarmErr == nil {
				log.Info("index warmup: prewarmed",
					slog.String("index", target.index),
					slog.Int64("blocks", blocks),
					slog.Duration("took", time.Since(start)))
				continue
			}
			// Prewarm can fail for benign reasons (e.g. an identifier that does
			// not resolve in this deployment). Fall through to the ANN query so
			// a name-resolution error never silently disables warming.
			log.Warn("index warmup: pg_prewarm failed; falling back to ANN query",
				slog.String("index", target.index), logger.Error(prewarmErr))
		}

		if _, err := repo.db.NewRaw(target.fallback).Exec(ctx); err != nil {
			log.Warn("index warmup: fallback query failed", slog.String("index", target.index), logger.Error(err))
			continue
		}
		log.Info("index warmup: warmed via ANN query",
			slog.String("index", target.index),
			slog.Duration("took", time.Since(start)))
	}
}
