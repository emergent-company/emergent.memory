-- +goose Up
-- +goose StatementBegin

-- Typed source objects: a durable mapping between an external source item and
-- the graph objects created from it. The mapping is the idempotency key and the
-- deletion-reconciliation ledger; graph objects themselves stay source-agnostic
-- (namespace `src:<sourceID>` prevents cross-account key collisions).
CREATE TABLE IF NOT EXISTS kb.source_objects (
    id uuid PRIMARY KEY DEFAULT public.uuid_generate_v4(),
    project_id uuid NOT NULL,
    source_id uuid NOT NULL REFERENCES kb.sources(id) ON DELETE CASCADE,
    external_id text NOT NULL,
    object_type text NOT NULL,
    object_key text NOT NULL,
    object_id uuid NOT NULL,
    object_canonical_id uuid NOT NULL,
    document_id uuid NULL,
    revision_id text NULL,
    status text NOT NULL DEFAULT 'active',
    synced_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_source_objects_identity
    ON kb.source_objects (source_id, external_id, object_type, object_key);

CREATE INDEX IF NOT EXISTS idx_source_objects_project_source
    ON kb.source_objects (project_id, source_id);

CREATE INDEX IF NOT EXISTS idx_source_objects_project_source_status
    ON kb.source_objects (project_id, source_id, status);

ALTER TABLE kb.source_objects ENABLE ROW LEVEL SECURITY;

CREATE POLICY source_objects_select ON kb.source_objects FOR SELECT USING (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
);

CREATE POLICY source_objects_insert ON kb.source_objects FOR INSERT WITH CHECK (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
);

CREATE POLICY source_objects_update ON kb.source_objects FOR UPDATE USING (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
) WITH CHECK (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
);

CREATE POLICY source_objects_delete ON kb.source_objects FOR DELETE USING (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
);

-- Stable, immutable MCP alias for a source. Used as the MCP server name and the
-- tool-name segment for source-backed tools (e.g. gmail-alice-message-search).
-- Unique per project so tool names cannot collide.
ALTER TABLE kb.sources ADD COLUMN IF NOT EXISTS mcp_slug text NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_sources_project_mcp_slug
    ON kb.sources (project_id, mcp_slug)
    WHERE mcp_slug IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_sources_project_mcp_slug;
ALTER TABLE kb.sources DROP COLUMN IF EXISTS mcp_slug;

DROP TABLE IF EXISTS kb.source_objects;

-- +goose StatementEnd
