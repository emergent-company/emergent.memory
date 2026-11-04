-- +goose NO TRANSACTION
-- +goose Up
-- Re-ensure the HNSW index on kb.graph_objects.embedding_v2 that 00164 was
-- supposed to create (issue #734).
--
-- 00164 is recorded as applied in goose_db_version on some environments, but
-- its effects are absent there: kb."IDX_graph_objects_embedding_v2_hnsw" does
-- not exist and the legacy ~420MB kb."IDX_graph_objects_embedding_v2_ivfflat"
-- is still present and valid. The Up ran CONCURRENTLY without a transaction, so
-- each statement commits on its own; a killed CREATE INDEX CONCURRENTLY (the
-- build was terminated via pg_terminate_backend) left no index, and goose still
-- recorded version 164 out-of-band. Because goose de-duplicates by version and
-- treats 164 as applied, 00164 can never run again, so the object stays missing
-- on every environment where this happened. Only a new, forward-only migration
-- can repair the state.
--
-- This migration converges on the intended catalog state (HNSW present and
-- valid, legacy ivfflat absent). On environments where 00164 did complete it is
-- a no-op for the HNSW index: dropping and rebuilding a ~420MB index there would
-- be needlessly expensive and, because 00164 already removed the ivfflat index,
-- would leave graph_objects similarity queries without ANY ANN index for the
-- whole duration of the CONCURRENTLY rebuild. The target index is therefore only
-- replaced when it is missing or not usable. The migration is deliberately a
-- superset of 00164 rather than a copy of it so the repair also works when
-- 00164's DROP of the ivfflat index never ran.
--
-- Built CONCURRENTLY (NO TRANSACTION) to avoid an access-exclusive lock on a
-- high-volume graph table. A failed CONCURRENTLY build leaves an INVALID index
-- behind, and CREATE INDEX ... IF NOT EXISTS will NOT rebuild a name that already
-- exists even when it is INVALID, so a leftover of the target name that is not
-- usable is cleared first (see below) and a retry rebuilds from scratch (same
-- rationale as 00162/00163/00164/00170). Requires pgvector >= 0.5.0 for HNSW.

-- Clear any stale leftover from a previous interrupted attempt of this migration.
-- This runs before the rename below so that rename can never collide with an
-- existing name. It is a no-op in the common case.
DROP INDEX CONCURRENTLY IF EXISTS kb."IDX_graph_objects_embedding_v2_hnsw_stale";

-- Clear an existing target index only when it is NOT usable (missing => nothing
-- to clear; valid => leave it in place so no rebuild is ever needed). A leftover
-- INVALID index cannot be removed with DROP INDEX CONCURRENTLY from inside a DO
-- block (CONCURRENTLY is rejected inside a function) and a plain DROP INDEX would
-- take ACCESS EXCLUSIVE on the table, blocking reads and writes; renaming it
-- aside instead is a fast, metadata-only change that takes no lock on the table,
-- and the renamed index is dropped CONCURRENTLY below. StatementBegin/End is
-- required because goose's NO TRANSACTION splitter does not understand
-- dollar-quoted bodies and would otherwise truncate at the first semicolon.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_index i
        JOIN pg_class c ON c.oid = i.indexrelid
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'kb'
          AND c.relname = 'IDX_graph_objects_embedding_v2_hnsw'
          AND NOT (i.indisvalid AND i.indisready)
    ) THEN
        ALTER INDEX kb."IDX_graph_objects_embedding_v2_hnsw"
            RENAME TO "IDX_graph_objects_embedding_v2_hnsw_stale";
    END IF;
END
$$;
-- +goose StatementEnd

-- Drop the renamed leftover (no-op when the target index was valid or absent).
DROP INDEX CONCURRENTLY IF EXISTS kb."IDX_graph_objects_embedding_v2_hnsw_stale";

-- Create the target index only when it is absent. When a valid HNSW index already
-- exists this is a no-op, so a healthy environment never pays the rebuild. The
-- index name must stay unqualified: CREATE INDEX derives the schema from the
-- (already schema-qualified) table reference and rejects a qualified index name.
-- Parameters match 00164 (pgvector defaults): m = 16, ef_construction = 64,
-- cosine opclass.
CREATE INDEX CONCURRENTLY IF NOT EXISTS "IDX_graph_objects_embedding_v2_hnsw"
    ON kb.graph_objects USING hnsw (embedding_v2 vector_cosine_ops)
    WITH (m = 16, ef_construction = 64);

-- Drop the now-superseded IVFFlat index once HNSW is in place (create-new-before-
-- drop-old, so an ANN index is available throughout). Two ~420MB ANN indexes on
-- one column doubles storage and gives the planner a second approximate path to
-- choose poorly. Idempotent when 00164 already removed it.
DROP INDEX CONCURRENTLY IF EXISTS kb."IDX_graph_objects_embedding_v2_ivfflat";

-- Post-migration guard. With NO TRANSACTION each statement commits on its own,
-- so a run where the CONCURRENTLY build does not take effect can still leave
-- earlier statements (including the DROP of the old index) committed. A
-- migration recorded as applied while the new index is missing or INVALID is
-- exactly the silent-partial-application failure mode that #734 is: 00164 was
-- recorded applied while its index never appeared. This block re-derives the
-- expected catalog state from pg_index/pg_am and raises if it is not met, so
-- goose refuses to record the version and the conditional drop-then-recreate
-- above makes the retry safe. Operators can run the equivalent read-only check:
--
--   SELECT c.relname, am.amname, i.indisvalid, i.indisready
--   FROM pg_index i
--   JOIN pg_class c ON c.oid = i.indexrelid
--   JOIN pg_namespace n ON n.oid = c.relnamespace
--   JOIN pg_am am ON am.oid = c.relam
--   WHERE n.nspname = 'kb'
--     AND c.relname IN ('IDX_graph_objects_embedding_v2_hnsw',
--                       'IDX_graph_objects_embedding_v2_ivfflat');
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
    FROM (VALUES ('IDX_graph_objects_embedding_v2_hnsw')) AS e(name)
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
        RAISE EXCEPTION '00175: HNSW embedding index missing or invalid: %', bad;
    END IF;

    IF EXISTS (
        SELECT 1
        FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'kb'
          AND c.relname = 'IDX_graph_objects_embedding_v2_ivfflat'
    ) THEN
        RAISE EXCEPTION '00175: legacy ivfflat embedding index still present after migration';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- Restore the IVFFlat index (lists = 100, matching 00001/00164) so a rollback
-- returns the schema to its pre-00175 state. Build the replacement (ivfflat)
-- while HNSW is still present and drop HNSW last, so an ANN index is available
-- throughout the rollback: if the ivfflat build fails, the working HNSW index is
-- left untouched.
--
-- Symmetric with Up: a valid ivfflat index is left in place instead of being
-- needlessly dropped and rebuilt, and only an ivfflat index that is missing or
-- not usable is (re)built. As in Up, a leftover INVALID ivfflat cannot be
-- replaced with CREATE INDEX ... IF NOT EXISTS (it would be skipped), cannot be
-- dropped CONCURRENTLY from inside the DO block, and a plain DROP INDEX would
-- take ACCESS EXCLUSIVE on the table, so it is renamed aside first and dropped
-- CONCURRENTLY below. The HNSW index (usable or not) is dropped unconditionally,
-- which is the point of the rollback.

-- Clear any stale leftover from a previous interrupted attempt of this rollback.
DROP INDEX CONCURRENTLY IF EXISTS kb."IDX_graph_objects_embedding_v2_ivfflat_stale";

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_index i
        JOIN pg_class c ON c.oid = i.indexrelid
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'kb'
          AND c.relname = 'IDX_graph_objects_embedding_v2_ivfflat'
          AND NOT (i.indisvalid AND i.indisready)
    ) THEN
        ALTER INDEX kb."IDX_graph_objects_embedding_v2_ivfflat"
            RENAME TO "IDX_graph_objects_embedding_v2_ivfflat_stale";
    END IF;
END
$$;
-- +goose StatementEnd

DROP INDEX CONCURRENTLY IF EXISTS kb."IDX_graph_objects_embedding_v2_ivfflat_stale";

CREATE INDEX CONCURRENTLY IF NOT EXISTS "IDX_graph_objects_embedding_v2_ivfflat"
    ON kb.graph_objects USING ivfflat (embedding_v2 vector_cosine_ops)
    WITH (lists = 100);

DROP INDEX CONCURRENTLY IF EXISTS kb."IDX_graph_objects_embedding_v2_hnsw";
