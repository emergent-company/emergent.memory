-- +goose Up

-- Add ownership columns to graph_schemas for project/org scoping.
-- Schemas are project-scoped by default; visibility='organization' shares with the org.
ALTER TABLE kb.graph_schemas
    ADD COLUMN project_id uuid,
    ADD COLUMN org_id uuid,
    ADD COLUMN visibility text NOT NULL DEFAULT 'project';

-- Backfill: derive org_id and project_id from existing project_schemas assignments.
-- For schemas assigned to multiple projects, pick the earliest assignment.
UPDATE kb.graph_schemas gs
SET project_id = sub.project_id,
    org_id     = sub.organization_id
FROM (
    SELECT DISTINCT ON (ps.schema_id)
        ps.schema_id,
        ps.project_id,
        p.organization_id
    FROM kb.project_schemas ps
    JOIN kb.projects p ON p.id = ps.project_id
    ORDER BY ps.schema_id, ps.installed_at ASC
) sub
WHERE gs.id = sub.schema_id
  AND gs.project_id IS NULL;

-- Indexes for the new ownership columns.
CREATE INDEX idx_graph_schemas_project_id ON kb.graph_schemas (project_id) WHERE project_id IS NOT NULL;
CREATE INDEX idx_graph_schemas_org_id ON kb.graph_schemas (org_id) WHERE org_id IS NOT NULL;
CREATE INDEX idx_graph_schemas_visibility ON kb.graph_schemas (visibility);

-- +goose Down
DROP INDEX IF EXISTS kb.idx_graph_schemas_visibility;
DROP INDEX IF EXISTS kb.idx_graph_schemas_org_id;
DROP INDEX IF EXISTS kb.idx_graph_schemas_project_id;

ALTER TABLE kb.graph_schemas
    DROP COLUMN IF EXISTS visibility,
    DROP COLUMN IF EXISTS org_id,
    DROP COLUMN IF EXISTS project_id;
