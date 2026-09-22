-- +goose NO TRANSACTION
-- +goose Up
-- Migrate the remaining IVFFlat embedding indexes owned by issue #670 to HNSW:
--   * kb.idx_chunks_embedding          on kb.chunks.embedding
--   * kb.idx_skills_embedding_ivfflat  on kb.skills.description_embedding
--
-- Scope note: kb.graph_objects.embedding_v2 was already migrated to HNSW in
-- 00164 (issue #707). kb.idx_graph_relationships_embedding_ivfflat is owned by a
-- separate change and is deliberately NOT touched here.
--
-- Rationale: IVFFlat fixes its `lists` partition count at build time and must
-- be trained on a representative sample, so incremental embedding backfills
-- steadily re-fragment the lists and degrade recall until the index is REINDEXed
-- (see issue #664). HNSW has no training step, tolerates incremental inserts,
-- and needs neither `ivfflat.probes` tuning nor scheduled rebuilds. Parameters
-- match 00164 (pgvector defaults): m = 16, ef_construction = 64, cosine opclass.
--
-- NO TRANSACTION because CREATE/DROP INDEX CONCURRENTLY cannot run inside a
-- transaction. This avoids an access-exclusive lock on the tables. Each index is
-- replaced one at a time so a valid ANN index is always available and lock impact
-- stays bounded. A failed CONCURRENTLY build leaves an INVALID index behind, so
-- any leftover new index is dropped first so a retry rebuilds from scratch.
-- Requires pgvector >= 0.5.0 for HNSW.
--
-- Index names must stay unqualified: CREATE INDEX derives the schema from the
-- (schema-qualified) table reference and rejects a qualified index name.

-- ---- kb.chunks.embedding -------------------------------------------------
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_chunks_embedding_hnsw;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_chunks_embedding_hnsw
    ON kb.chunks USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 64);

DROP INDEX CONCURRENTLY IF EXISTS kb.idx_chunks_embedding;

-- ---- kb.skills.description_embedding ------------------------------------
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_skills_embedding_hnsw;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_skills_embedding_hnsw
    ON kb.skills USING hnsw (description_embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 64);

DROP INDEX CONCURRENTLY IF EXISTS kb.idx_skills_embedding_ivfflat;

-- Post-migration guard / documented post-migration check. With NO TRANSACTION
-- each statement commits on its own, so a run where a CONCURRENTLY build does
-- not take effect can still leave earlier statements (including the DROP of the
-- old index) committed. A migration recorded as applied while the new index is
-- missing or INVALID is exactly the silent-partial-application failure mode that
-- CONCURRENTLY migrations must guard against, so this block re-derives the
-- expected catalog state from pg_index/pg_am and raises if it is not met. goose
-- then refuses to record the version, and the idempotent drop-then-recreate
-- above makes the retry safe. Operators can run the equivalent read-only check:
--
--   SELECT c.relname, am.amname, i.indisvalid, i.indisready
--   FROM pg_index i
--   JOIN pg_class c ON c.oid = i.indexrelid
--   JOIN pg_namespace n ON n.oid = c.relnamespace
--   JOIN pg_am am ON am.oid = c.relam
--   WHERE n.nspname = 'kb'
--     AND c.relname IN ('idx_chunks_embedding_hnsw','idx_skills_embedding_hnsw',
--                       'idx_chunks_embedding','idx_skills_embedding_ivfflat');
--
-- StatementBegin/End is required because goose's NO TRANSACTION splitter does
-- not understand dollar-quoted bodies and would otherwise truncate at the first
-- semicolon.
-- +goose StatementBegin
DO $$
DECLARE
    bad text;
BEGIN
    SELECT string_agg(e.name, ', ') INTO bad
    FROM (VALUES ('idx_chunks_embedding_hnsw'), ('idx_skills_embedding_hnsw')) AS e(name)
    WHERE NOT EXISTS (
        SELECT 1
        FROM pg_index i
        JOIN pg_class c ON c.oid = i.indexrelid
        JOIN pg_namespace n ON n.oid = c.relnamespace
        JOIN pg_am am ON am.oid = c.relam
        WHERE n.nspname = 'kb'
          AND c.relname = e.name
          AND am.amname = 'hnsw'
          AND i.indisvalid
          AND i.indisready
    );
    IF bad IS NOT NULL THEN
        RAISE EXCEPTION '00170: HNSW embedding index missing or invalid: %', bad;
    END IF;

    IF EXISTS (
        SELECT 1
        FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'kb'
          AND c.relname IN ('idx_chunks_embedding', 'idx_skills_embedding_ivfflat')
    ) THEN
        RAISE EXCEPTION '00170: legacy ivfflat embedding index still present after migration';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- Restore the IVFFlat indexes (lists = 100, matching 00001/00052) so a rollback
-- returns the schema to its pre-00170 state. Each index is replaced one at a
-- time and the new index is dropped first for a clean retry of an interrupted
-- CONCURRENTLY build.
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_chunks_embedding_hnsw;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_chunks_embedding
    ON kb.chunks USING ivfflat (embedding vector_cosine_ops)
    WITH (lists = 100);

DROP INDEX CONCURRENTLY IF EXISTS kb.idx_skills_embedding_hnsw;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_skills_embedding_ivfflat
    ON kb.skills USING ivfflat (description_embedding vector_cosine_ops)
    WITH (lists = 100);
