-- +goose Up
-- +goose StatementBegin

-- Issue #7: FTS search returns empty results
--
-- The fts tsvector column on kb.graph_objects was never populated because:
-- 1. No trigger function existed (kb.update_tsv() only handles chunks)
-- 2. No GIN index existed for efficient full-text search queries
--
-- This migration adds:
-- 1. A trigger function that populates fts from key (title), type, and properties
-- 2. A GIN index for efficient ts_rank / @@ queries
-- 3. A trigger on INSERT/UPDATE to keep fts current
-- 4. A backfill of all existing rows

-- Create the trigger function for graph_objects FTS
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

-- Create GIN index for FTS queries
CREATE INDEX IF NOT EXISTS idx_graph_objects_fts ON kb.graph_objects USING gin (fts);

-- Bind trigger to graph_objects INSERT/UPDATE
CREATE TRIGGER trg_graph_objects_fts
    BEFORE INSERT OR UPDATE ON kb.graph_objects
    FOR EACH ROW
    EXECUTE FUNCTION kb.update_graph_objects_fts();

-- Backfill existing rows
UPDATE kb.graph_objects
SET fts =
    setweight(to_tsvector('simple', coalesce(key, '')), 'A') ||
    setweight(to_tsvector('simple', coalesce(type, '')), 'B') ||
    setweight(to_tsvector('simple', coalesce(
        (SELECT string_agg(value::text, ' ')
         FROM jsonb_each_text(CASE WHEN jsonb_typeof(properties) = 'object' THEN properties ELSE '{}'::jsonb END)),
        ''
    )), 'C');

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS trg_graph_objects_fts ON kb.graph_objects;
DROP FUNCTION IF EXISTS kb.update_graph_objects_fts();
DROP INDEX IF EXISTS kb.idx_graph_objects_fts;
UPDATE kb.graph_objects SET fts = NULL;
-- +goose StatementEnd
