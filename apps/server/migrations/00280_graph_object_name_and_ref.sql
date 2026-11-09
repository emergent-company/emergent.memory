-- +goose Up
-- +goose StatementBegin

-- First-class object title. `key` stays the internal dedup/upsert identity; it
-- is no longer the user-facing title. `name` carries the human title and is the
-- primary full-text signal (weight A), while the display ref rides with the type.
ALTER TABLE kb.graph_objects ADD COLUMN IF NOT EXISTS name text;

-- Backfill from the properties convention, then a readable legacy key. Machine
-- keys of the form "<type>-<12 hex>" (the old generateEntityKey output) are not
-- titles and stay out of `name`.
UPDATE kb.graph_objects
SET name = COALESCE(
    NULLIF(properties->>'name', ''),
    NULLIF(properties->>'title', ''),
    CASE WHEN key ~ '^[a-z0-9_]+-[0-9a-f]{12}$' THEN NULL ELSE NULLIF(key, '') END
)
WHERE name IS NULL;

-- Name lookup for filters/sorts (case-insensitive per type).
CREATE INDEX IF NOT EXISTS idx_graph_objects_project_type_name_lower
    ON kb.graph_objects (project_id, type, lower(name));

-- Display ref lookup: last 12 hex of the canonical UUID (dashes stripped). The
-- ref is display-only; this expression index backs exact-ref search.
CREATE INDEX IF NOT EXISTS idx_graph_objects_ref
    ON kb.graph_objects ((right(replace(canonical_id::text, '-', ''), 12)));

-- +goose StatementEnd

-- Rebuild the shared FTS builder (migration 00174) to promote `name` to the
-- title signal and index the display ref alongside the type. Key identifiers
-- remain indexed for exact component lookups; prose stays bounded and stemmed.
-- +goose StatementBegin
DROP FUNCTION IF EXISTS kb.graph_object_fts(text, text, jsonb);

CREATE OR REPLACE FUNCTION kb.graph_object_fts(
    p_key text,
    p_name text,
    p_type text,
    p_canonical_id uuid,
    p_properties jsonb
) RETURNS tsvector
    LANGUAGE sql
    IMMUTABLE
    PARALLEL SAFE
    AS $$
    SELECT
        -- Human title. Norwegian stemmer so morphological variants match.
        setweight(to_tsvector('norwegian', left(coalesce(p_name, ''), 4000)), 'A')
        -- Raw key (exact identifier lookups, e.g. "forskrift/...#kapittel-1-paragraf-2")
        || setweight(to_tsvector('simple', coalesce(p_key, '')), 'A')
        -- Separator-normalised key: `/`, `-`, `#` -> space, so component tokens
        -- exist (lov, 1997, 06, 13, 44). The signed variant keeps the minus on
        -- numeric components (" -06"), which is the lexeme websearch_to_tsquery
        -- emits for a hyphenated run, making `'1997' <-> '-06' <-> ...` satisfiable.
        || setweight(to_tsvector('simple',
               translate(coalesce(p_key, ''), '/-#', '   ')
               || ' '
               || replace(replace(replace(coalesce(p_key, ''), '/', ' '), '#', ' '), '-', ' -')
           ), 'A')
        -- Type + display ref (# + last 12 hex of the canonical UUID).
        || setweight(to_tsvector('simple',
               coalesce(p_type, '') || ' #' ||
               coalesce(right(replace(p_canonical_id::text, '-', ''), 12), '')
           ), 'B')
        -- Prose: norwegian stemmer over a bounded whitelist. Identifier fields
        -- above stay simple.
        || setweight(to_tsvector('norwegian',
               left(coalesce(p_properties ->> 'title', ''), 4000)
               || ' ' || left(coalesce(p_properties ->> 'name', ''), 4000)
               || ' ' || left(coalesce(p_properties ->> 'description', ''), 4000)
           ), 'C');
$$;
-- +goose StatementEnd

-- Trigger keeps the 00032/00174 source-field guard, extended to `name` (and the
-- immutable canonical id) so property-only edits still skip the rebuild.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION kb.update_graph_objects_fts() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND
       NEW.key IS NOT DISTINCT FROM OLD.key AND
       NEW.name IS NOT DISTINCT FROM OLD.name AND
       NEW.type IS NOT DISTINCT FROM OLD.type AND
       NEW.canonical_id IS NOT DISTINCT FROM OLD.canonical_id AND
       NEW.properties IS NOT DISTINCT FROM OLD.properties THEN
        RETURN NEW;
    END IF;

    NEW.fts := kb.graph_object_fts(NEW.key, NEW.name, NEW.type, NEW.canonical_id, NEW.properties);
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- Backfill through the trigger so the builder is used exactly once per row.
-- +goose StatementBegin
UPDATE kb.graph_objects
   SET fts = kb.graph_object_fts(key, name, type, canonical_id, properties)
 WHERE fts IS DISTINCT FROM kb.graph_object_fts(key, name, type, canonical_id, properties);
-- +goose StatementEnd

-- +goose Down
-- Restore the migration 00174 builder/trigger verbatim, rebuild, drop the new
-- indexes and column.
-- +goose StatementBegin
DROP FUNCTION IF EXISTS kb.graph_object_fts(text, text, text, uuid, jsonb);

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
        setweight(to_tsvector('simple', coalesce(p_key, '')), 'A')
        || setweight(to_tsvector('simple',
               translate(coalesce(p_key, ''), '/-#', '   ')
               || ' '
               || replace(replace(replace(coalesce(p_key, ''), '/', ' '), '#', ' '), '-', ' -')
           ), 'A')
        || setweight(to_tsvector('simple', coalesce(p_type, '')), 'B')
        || setweight(to_tsvector('norwegian',
               left(coalesce(p_properties ->> 'title', ''), 4000)
               || ' ' || left(coalesce(p_properties ->> 'name', ''), 4000)
               || ' ' || left(coalesce(p_properties ->> 'description', ''), 4000)
           ), 'C');
$$;

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

UPDATE kb.graph_objects
   SET fts = kb.graph_object_fts(key, type, properties)
 WHERE fts IS DISTINCT FROM kb.graph_object_fts(key, type, properties);

DROP INDEX IF EXISTS kb.idx_graph_objects_ref;
DROP INDEX IF EXISTS kb.idx_graph_objects_project_type_name_lower;
ALTER TABLE kb.graph_objects DROP COLUMN IF EXISTS name;
-- +goose StatementEnd
