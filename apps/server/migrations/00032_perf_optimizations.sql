-- +goose Up
-- +goose StatementBegin

-- Performance Optimization: Smarter FTS triggers + remove redundant index
--
-- Problem 1: trg_graph_objects_fts fires on EVERY insert/update, including
--   version inserts where key/type/properties haven't changed. It runs
--   jsonb_each_text() across all properties unconditionally, making it
--   expensive on the most-written table in the system.
--
-- Fix: Only recompute fts when key, type, or properties actually change.
--   On INSERT, always compute it. On UPDATE, skip if source fields are identical.
--
-- Problem 2: Same unconditional firing for trg_chunks_tsv on kb.chunks.
--   During bulk rechunking this fires N times unnecessarily.
--
-- Fix: Only recompute tsv when the text column changes.
--
-- Problem 3: IDX_graph_objects_key is a non-unique B-tree on (project_id, type, key).
--   Migration 00017 added partial unique indexes covering the same columns.
--   The old non-unique index is now redundant write overhead on every insert.
--
-- Fix: Drop the redundant index.

-- ─── 1. Optimized graph_objects FTS trigger ───────────────────────────────────

CREATE OR REPLACE FUNCTION kb.update_graph_objects_fts() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    -- On UPDATE, skip the expensive recompute if the source fields are unchanged.
    -- On INSERT, TG_OP = 'INSERT' so OLD is undefined — always compute.
    IF TG_OP = 'UPDATE' AND
       NEW.key IS NOT DISTINCT FROM OLD.key AND
       NEW.type IS NOT DISTINCT FROM OLD.type AND
       NEW.properties IS NOT DISTINCT FROM OLD.properties THEN
        RETURN NEW;
    END IF;

    NEW.fts :=
        setweight(to_tsvector('simple', coalesce(NEW.key, '')), 'A') ||
        setweight(to_tsvector('simple', coalesce(NEW.type, '')), 'B') ||
        setweight(to_tsvector('simple', coalesce(
            (SELECT string_agg(value::text, ' ')
             FROM jsonb_each_text(CASE WHEN jsonb_typeof(NEW.properties) = 'object' THEN NEW.properties ELSE '{}'::jsonb END)),
            ''
        )), 'C');
    RETURN NEW;
END;
$$;

-- ─── 2. Optimized chunks TSV trigger ─────────────────────────────────────────

CREATE OR REPLACE FUNCTION kb.update_tsv() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    -- On UPDATE, only recompute when text actually changed.
    IF TG_OP = 'UPDATE' AND NEW.text IS NOT DISTINCT FROM OLD.text THEN
        RETURN NEW;
    END IF;

    NEW.tsv := to_tsvector('simple', NEW.text);
    RETURN NEW;
END;
$$;

-- ─── 3. Drop the redundant non-unique index on graph_objects ─────────────────
-- IDX_graph_objects_key indexes (project_id, type, key) non-uniquely.
-- IDX_graph_objects_upsert_main (from 00017) indexes the same columns as a
-- partial unique index. The non-unique one is now dead write overhead.
-- Query plans that previously used IDX_graph_objects_key will use the partial
-- unique index instead (same columns, planner prefers more selective indexes).

DROP INDEX IF EXISTS kb."IDX_graph_objects_key";

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Restore original unconditional graph_objects FTS trigger
CREATE OR REPLACE FUNCTION kb.update_graph_objects_fts() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    NEW.fts :=
        setweight(to_tsvector('simple', coalesce(NEW.key, '')), 'A') ||
        setweight(to_tsvector('simple', coalesce(NEW.type, '')), 'B') ||
        setweight(to_tsvector('simple', coalesce(
            (SELECT string_agg(value::text, ' ')
             FROM jsonb_each_text(CASE WHEN jsonb_typeof(NEW.properties) = 'object' THEN NEW.properties ELSE '{}'::jsonb END)),
            ''
        )), 'C');
    RETURN NEW;
END;
$$;

-- Restore original unconditional chunks TSV trigger
CREATE OR REPLACE FUNCTION kb.update_tsv() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    NEW.tsv := to_tsvector('simple', NEW.text);
    RETURN NEW;
END;
$$;

-- Recreate the redundant index (for rollback completeness)
CREATE INDEX IF NOT EXISTS "IDX_graph_objects_key"
    ON kb.graph_objects (project_id, type, key);

-- +goose StatementEnd
