-- +goose Up
-- +goose StatementBegin

-- Improve full-text search recall for multi-word queries (fixes GitHub #103).
--
-- Problem: The 'simple' text search config uses AND semantics in
-- websearch_to_tsquery — every word in the query must appear in the document.
-- A query like "notification invitation reminder email delivery" returns 0
-- results because all 5 words must match. The 'simple' config also skips
-- stemming, so "running" won't match "run".
--
-- Fix: Switch both tsvector generation (triggers) and tsquery matching
-- (application SQL) to the 'english' config. This enables:
--   1. Stemming: "running" → "run", "notifications" → "notif"
--   2. Stop-word removal: common words like "the", "and", "of" are ignored
--      rather than becoming mandatory AND terms
--
-- Both sides (tsvector + tsquery) MUST use the same config or matches fail.
-- Application code is updated in the same commit.

-- ─── 1. Rebuild graph_objects FTS trigger with 'english' config ──────────────

CREATE OR REPLACE FUNCTION kb.update_graph_objects_fts() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    -- On UPDATE, skip the expensive recompute if the source fields are unchanged.
    IF TG_OP = 'UPDATE' AND
       NEW.key IS NOT DISTINCT FROM OLD.key AND
       NEW.type IS NOT DISTINCT FROM OLD.type AND
       NEW.properties IS NOT DISTINCT FROM OLD.properties THEN
        RETURN NEW;
    END IF;

    NEW.fts :=
        setweight(to_tsvector('english', coalesce(NEW.key, '')), 'A') ||
        setweight(to_tsvector('english', coalesce(NEW.type, '')), 'B') ||
        setweight(to_tsvector('english', coalesce(
            (SELECT string_agg(value::text, ' ')
             FROM jsonb_each_text(CASE WHEN jsonb_typeof(NEW.properties) = 'object' THEN NEW.properties ELSE '{}'::jsonb END)),
            ''
        )), 'C');
    RETURN NEW;
END;
$$;

-- ─── 2. Rebuild chunks TSV trigger with 'english' config ────────────────────

CREATE OR REPLACE FUNCTION kb.update_tsv() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND NEW.text IS NOT DISTINCT FROM OLD.text THEN
        RETURN NEW;
    END IF;

    NEW.tsv := to_tsvector('english', NEW.text);
    RETURN NEW;
END;
$$;

-- ─── 3. Backfill existing graph_objects FTS vectors ─────────────────────────
-- Touch key to force trigger refire on all HEAD objects.
-- This is safe: it sets key = key (no-op value change) but the trigger
-- sees NEW.key IS DISTINCT FROM OLD.key as false — however, we also set
-- properties = properties which also won't fire. So we must update fts directly.

UPDATE kb.graph_objects
SET fts =
    setweight(to_tsvector('english', coalesce(key, '')), 'A') ||
    setweight(to_tsvector('english', coalesce(type, '')), 'B') ||
    setweight(to_tsvector('english', coalesce(
        (SELECT string_agg(value::text, ' ')
         FROM jsonb_each_text(CASE WHEN jsonb_typeof(properties) = 'object' THEN properties ELSE '{}'::jsonb END)),
        ''
    )), 'C')
WHERE supersedes_id IS NULL
  AND deleted_at IS NULL;

-- ─── 4. Backfill existing chunks TSV vectors ────────────────────────────────

UPDATE kb.chunks
SET tsv = to_tsvector('english', text)
WHERE text IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Restore 'simple' config on graph_objects FTS trigger
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

-- Restore 'simple' config on chunks TSV trigger
CREATE OR REPLACE FUNCTION kb.update_tsv() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND NEW.text IS NOT DISTINCT FROM OLD.text THEN
        RETURN NEW;
    END IF;

    NEW.tsv := to_tsvector('simple', NEW.text);
    RETURN NEW;
END;
$$;

-- Backfill graph_objects back to 'simple'
UPDATE kb.graph_objects
SET fts =
    setweight(to_tsvector('simple', coalesce(key, '')), 'A') ||
    setweight(to_tsvector('simple', coalesce(type, '')), 'B') ||
    setweight(to_tsvector('simple', coalesce(
        (SELECT string_agg(value::text, ' ')
         FROM jsonb_each_text(CASE WHEN jsonb_typeof(properties) = 'object' THEN properties ELSE '{}'::jsonb END)),
        ''
    )), 'C')
WHERE supersedes_id IS NULL
  AND deleted_at IS NULL;

-- Backfill chunks back to 'simple'
UPDATE kb.chunks
SET tsv = to_tsvector('simple', text)
WHERE text IS NOT NULL;

-- +goose StatementEnd
