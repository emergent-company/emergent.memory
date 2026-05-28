-- +goose Up
-- Remove org-scoped schema visibility. Schemas are now strictly project-scoped.
-- The 'organization' visibility tier allowed schemas to be shared across all projects
-- in an org — this feature is removed. 'global' visibility (builtins) is unaffected
-- since it uses a separate query path and the column is being dropped entirely.
--
-- Also consolidates the duplicate tenant_id column on discovery_jobs into organization_id
-- (they were always set to the same value).

ALTER TABLE kb.graph_schemas DROP COLUMN IF EXISTS org_id;
ALTER TABLE kb.graph_schemas DROP COLUMN IF EXISTS visibility;
DROP INDEX IF EXISTS idx_graph_schemas_org_id;
DROP INDEX IF EXISTS idx_graph_schemas_visibility;

ALTER TABLE kb.discovery_jobs DROP COLUMN IF EXISTS tenant_id;

-- +goose Down
ALTER TABLE kb.graph_schemas ADD COLUMN org_id uuid;
ALTER TABLE kb.graph_schemas ADD COLUMN visibility text NOT NULL DEFAULT 'project';
CREATE INDEX idx_graph_schemas_org_id ON kb.graph_schemas (org_id) WHERE org_id IS NOT NULL;
CREATE INDEX idx_graph_schemas_visibility ON kb.graph_schemas (visibility);
ALTER TABLE kb.discovery_jobs ADD COLUMN tenant_id uuid NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000';
