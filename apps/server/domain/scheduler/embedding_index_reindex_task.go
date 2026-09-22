package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// embeddingIndexTarget identifies an ivfflat embedding index maintained by the
// periodic reindex task.
type embeddingIndexTarget struct {
	schema string
	name   string
}

// embeddingIndexTargets lists every ivfflat embedding index rebuilt on a
// schedule. HNSW indexes are excluded: they need no periodic REINDEX. The
// graph_objects ivfflat index was dropped in migration 00164 and the
// graph_relationships ivfflat index in migration 00171, both replaced by HNSW,
// so neither is reindexed here.
var embeddingIndexTargets = []embeddingIndexTarget{
	{schema: "kb", name: "idx_chunks_embedding"},
	{schema: "kb", name: "idx_skills_embedding_ivfflat"},
}

// EmbeddingIndexReindexTask rebuilds the ivfflat embedding indexes on a
// schedule. ivfflat clustering degrades as embeddings are backfilled
// incrementally, so a vector search with probes=10 reads far more index pages
// than expected (issue #664). REINDEX INDEX CONCURRENTLY rebuilds without
// blocking writes; any index left INVALID by an interrupted concurrent build is
// recovered first with a plain REINDEX.
type EmbeddingIndexReindexTask struct {
	db  *bun.DB
	log *slog.Logger
}

// NewEmbeddingIndexReindexTask creates a new embedding index reindex task.
func NewEmbeddingIndexReindexTask(db *bun.DB, log *slog.Logger) *EmbeddingIndexReindexTask {
	return &EmbeddingIndexReindexTask{
		db:  db,
		log: log.With(logger.Scope("scheduler.embedding_index_reindex")),
	}
}

// Run reindexes every target ivfflat embedding index. Per-index failures are
// accumulated and returned as a single aggregate error so the scheduler records
// the maintenance job as failed when any index could not be reindexed.
func (t *EmbeddingIndexReindexTask) Run(ctx context.Context) error {
	start := time.Now()
	var errs []error

	for _, target := range embeddingIndexTargets {
		targetStart := time.Now()

		// Recover an index left INVALID by a previously interrupted concurrent
		// rebuild. A plain REINDEX INDEX is required (CONCURRENTLY cannot fix an
		// invalid index) and is acceptable here since the index is already broken.
		invalid, err := t.isIndexInvalid(ctx, target)
		if err != nil {
			t.log.Warn("failed to check index validity",
				slog.String("index", target.qualified()),
				slog.String("error", err.Error()))
			errs = append(errs, fmt.Errorf("check validity of %s: %w", target.qualified(), err))
			continue
		}
		if invalid {
			t.log.Warn("recovering invalid index with plain reindex",
				slog.String("index", target.qualified()))
			if err := t.reindex(ctx, target, false); err != nil {
				t.log.Error("failed to recover invalid index",
					slog.String("index", target.qualified()),
					slog.String("error", err.Error()))
				errs = append(errs, fmt.Errorf("recover %s: %w", target.qualified(), err))
				continue
			}
		}

		if err := t.reindex(ctx, target, true); err != nil {
			t.log.Error("failed to reindex embedding index",
				slog.String("index", target.qualified()),
				slog.String("error", err.Error()))
			errs = append(errs, fmt.Errorf("reindex %s: %w", target.qualified(), err))
			continue
		}

		t.log.Info("reindexed embedding index",
			slog.String("index", target.qualified()),
			slog.Duration("duration", time.Since(targetStart)))
	}

	t.log.Info("embedding index reindex completed",
		slog.Duration("duration", time.Since(start)))
	return errors.Join(errs...)
}

// reindex rebuilds target, concurrently when concurrent is true. Concurrent
// builds run outside a transaction (bun ExecContext is autocommit) and do not
// block writes; a plain rebuild takes an ACCESS EXCLUSIVE lock and is used only
// to recover invalid indexes.
func (t *EmbeddingIndexReindexTask) reindex(ctx context.Context, target embeddingIndexTarget, concurrent bool) error {
	verb := "REINDEX INDEX"
	if concurrent {
		verb = "REINDEX INDEX CONCURRENTLY"
	}
	_, err := t.db.ExecContext(ctx, verb+" "+target.qualified())
	return err
}

// isIndexInvalid reports whether target's pg_index entry is marked invalid.
func (t *EmbeddingIndexReindexTask) isIndexInvalid(ctx context.Context, target embeddingIndexTarget) (bool, error) {
	var invalid bool
	err := t.db.NewRaw(`
		SELECT NOT i.indisvalid
		FROM pg_index i
		JOIN pg_class c ON c.oid = i.indexrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = ? AND c.relname = ?
	`, target.schema, target.name).Scan(ctx, &invalid)
	if err != nil {
		return false, err
	}
	return invalid, nil
}

// qualified returns the schema-qualified, identifier-quoted index name.
func (t embeddingIndexTarget) qualified() string {
	return quoteIdent(t.schema) + "." + quoteIdent(t.name)
}

// quoteIdent quotes a SQL identifier, doubling embedded double quotes.
func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
