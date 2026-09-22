-- +goose NO TRANSACTION
-- +goose Up
--
-- Issue #706: kb.graph_objects.fts cannot represent this platform's identifiers
-- and silently kills phrase search. Two independent defects, both verified:
--
--   1. Composite keys are indexed atomically. The default parser treats
--      "x/y-z" as a file/host token, so:
--        to_tsvector('simple', 'lov/1997-06-13-44')
--          = 'lov/1997-06-13-44':1        (one opaque lexeme)
--      The law identifier is therefore unreachable in component form:
--      no query containing `lov 1997 06 13 44` can ever match it.
--
--   2. Positions saturate at MAXENTRYPOS (16383). kb.update_graph_objects_fts
--      (migration 00016) concatenated every value of the `properties` jsonb
--      unbounded, so large objects exhausted the position budget and every
--      later lexeme collapsed onto position 16383. Because
--      websearch_to_tsquery emits `<->` for any hyphenated run, `a <-> b`
--      could never be satisfied on such rows — phrase search was structurally
--      dead, and the resulting tsvectors bloated the table (790MB observed).
--
-- This migration:
--   * indexes a separator-normalised key so component tokens exist alongside
--     the raw form (`/`, `#` -> space, plus a signed variant where `-` -> " -"
--     so websearch_to_tsquery's `<->` phrase over `1997-06-13-44` is
--     satisfiable);
--   * bounds what enters `fts` to key, type and a whitelist of prose fields
--     (properties->>'title' | 'name' | 'description', each capped) instead of
--     the whole `properties` blob;
--   * uses the `norwegian` text search configuration for the prose fields
--     while keeping `simple` semantics for the identifier fields, concatenated
--     as two weighted tsvectors (A/B identifiers, C prose);
--   * backfills existing rows and rebuilds the GIN index CONCURRENTLY.
--
-- Query side (domain/graph/repository.go) matches either configuration:
--   (fts @@ websearch_to_tsquery('simple', q)
--    OR fts @@ websearch_to_tsquery('norwegian', q))
--
-- LOCK IMPACT
--   * Adding/replacing a function: no table lock.
--   * `DROP INDEX CONCURRENTLY` / `CREATE INDEX CONCURRENTLY`: no session-
--     blocking lock; they only briefly take a SHARE UPDATE EXCLUSIVE lock and
--     wait for concurrent transactions to drain.
--   * The backfill is a single UPDATE of the whole table (RowExclusiveLock,
--     concurrent reads and writes to unrelated rows proceed). It is run after
--     the GIN index is dropped so it does not pay index-maintenance cost per
--     row. For a much larger table the UPDATE can be re-run in ctid batches by
--     hand; the statement is idempotent (WHERE fts IS DISTINCT FROM ...).
--
-- ROLLBACK
--   Down restores the exact 00032 trigger function and expression (including
--   the source-field guard that skips recomputation when key/type/properties are
--   unchanged), backfills with it, and rebuilds the index. The helper function
--   is dropped last.

-- Immutable tsvector builder shared by the trigger and the backfill so the two
-- can never drift. Bound: each prose field is capped at 4000 chars, which is at
-- most ~2000 positions per field (~6000 total) — safely below MAXENTRYPOS.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION kb.graph_object_fts(
    p_key text,
    p_type text,
    p_properties jsonb
) RETURNS tsvector
    LANGUAGE sql
    IMMUTABLE
    PARALLEL SAFE
    AS $$
    SELECT
        -- Raw key (exact identifier lookups, e.g. "forskrift/...#kapittel-1-paragraf-2")
        setweight(to_tsvector('simple', coalesce(p_key, '')), 'A')
        -- Separator-normalised key: `/`, `-`, `#` -> space, so component tokens
        -- exist (lov, 1997, 06, 13, 44). The signed variant keeps the minus on
        -- numeric components (" -06"), which is the lexeme websearch_to_tsquery
        -- emits for a hyphenated run, making `'1997' <-> '-06' <-> ...` satisfiable.
        || setweight(to_tsvector('simple',
               translate(coalesce(p_key, ''), '/-#', '   ')
               || ' '
               || replace(replace(replace(coalesce(p_key, ''), '/', ' '), '#', ' '), '-', ' -')
           ), 'A')
        || setweight(to_tsvector('simple', coalesce(p_type, '')), 'B')
        -- Prose: norwegian stemmer over a bounded whitelist. `aksjeselskap` and
        -- `aksjeselskaper` become one lexeme; identifier fields above stay simple.
        || setweight(to_tsvector('norwegian',
               left(coalesce(p_properties ->> 'title', ''), 4000)
               || ' ' || left(coalesce(p_properties ->> 'name', ''), 4000)
               || ' ' || left(coalesce(p_properties ->> 'description', ''), 4000)
           ), 'C');
$$;
-- +goose StatementEnd

-- Trigger body delegates to the shared builder, but keeps the migration 00032
-- source-field guard: on UPDATE, skip the recompute when key/type/properties are
-- unchanged, so status/label/embedding updates don't reparse the JSON and rebuild
-- the tsvector. On INSERT (TG_OP = 'INSERT') OLD is undefined — always compute.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION kb.update_graph_objects_fts() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND
       NEW.key IS NOT DISTINCT FROM OLD.key AND
       NEW.type IS NOT DISTINCT FROM OLD.type AND
       NEW.properties IS NOT DISTINCT FROM OLD.properties THEN
        RETURN NEW;
    END IF;

    NEW.fts := kb.graph_object_fts(NEW.key, NEW.type, NEW.properties);
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- Drop the index before the backfill so the rewrite does not maintain GIN
-- entries row by row; it is rebuilt CONCURRENTLY afterwards.
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_graph_objects_fts;

-- Backfill. The BEFORE UPDATE trigger recomputes fts from the same builder; the
-- explicit SET keeps the intent readable and the WHERE makes re-runs cheap.
-- +goose StatementBegin
UPDATE kb.graph_objects
   SET fts = kb.graph_object_fts(key, type, properties)
 WHERE fts IS DISTINCT FROM kb.graph_object_fts(key, type, properties);
-- +goose StatementEnd

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_graph_objects_fts
    ON kb.graph_objects USING gin (fts);

-- +goose Down
-- Restore the migration 00032 definition verbatim (including the source-field
-- guard), re-backfill, rebuild.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION kb.update_graph_objects_fts() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
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
-- +goose StatementEnd

DROP INDEX CONCURRENTLY IF EXISTS kb.idx_graph_objects_fts;

-- +goose StatementBegin
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

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_graph_objects_fts
    ON kb.graph_objects USING gin (fts);

DROP FUNCTION IF EXISTS kb.graph_object_fts(text, text, jsonb);
