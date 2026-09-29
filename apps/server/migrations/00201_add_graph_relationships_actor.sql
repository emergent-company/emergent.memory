-- +goose Up
-- Add actor provenance columns to kb.graph_relationships, mirroring the existing
-- kb.graph_objects.actor_type / actor_id model (issue #1193).
--
-- actor_type is polymorphic: 'user' (human / HTTP), 'agent' (agent tool writes),
-- 'system' (extraction / background). actor_id holds the user UUID for 'user',
-- the kb.agents UUID for 'agent', and NULL for 'system'. The columns are
-- nullable so pre-existing rows (written before provenance tracking) remain
-- NULL rather than being attributed to a guessed actor.
ALTER TABLE kb.graph_relationships
    ADD COLUMN IF NOT EXISTS actor_type TEXT,
    ADD COLUMN IF NOT EXISTS actor_id UUID;

-- Mirror the kb.graph_objects actor_type CHECK (00001_baseline.sql): restrict the
-- polymorphic value set. NULL passes the CHECK (NULL = ANY(...) is NULL), so
-- pre-existing rows are unaffected.
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'chk_graph_relationships_actor_type'
    ) THEN
        ALTER TABLE kb.graph_relationships
        ADD CONSTRAINT chk_graph_relationships_actor_type
        CHECK ((actor_type = ANY (ARRAY['user'::text, 'agent'::text, 'system'::text])));
    END IF;
END $$;
-- +goose StatementEnd

-- Partial index mirroring idx_graph_objects_actor (00001_baseline.sql) so the
-- object provenance filter's (actor_type, actor_id) scans are index-driven. The
-- partial predicate matches only rows with a non-NULL actor_type, which is empty
-- at migration time (all pre-existing rows are NULL), so the build is instant.
CREATE INDEX IF NOT EXISTS idx_graph_relationships_actor
    ON kb.graph_relationships USING btree (actor_type, actor_id)
    WHERE (actor_type IS NOT NULL);

-- +goose Down
DROP INDEX IF EXISTS kb.idx_graph_relationships_actor;
ALTER TABLE kb.graph_relationships DROP CONSTRAINT IF EXISTS chk_graph_relationships_actor_type;
ALTER TABLE kb.graph_relationships
    DROP COLUMN IF EXISTS actor_id,
    DROP COLUMN IF EXISTS actor_type;
