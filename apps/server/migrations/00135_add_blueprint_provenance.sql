-- +goose Up
-- Blueprint unapply provenance: tracks which blueprint created/owns an agent
-- definition or pack assignment, so unapply can reverse only what it created
-- and never destroy pre-existing or shared resources.

ALTER TABLE kb.agent_definitions
    ADD COLUMN IF NOT EXISTS source_blueprint_id UUID;
CREATE INDEX IF NOT EXISTS idx_agent_definitions_source_blueprint
    ON kb.agent_definitions (project_id, source_blueprint_id);

ALTER TABLE kb.project_schemas
    ADD COLUMN IF NOT EXISTS source_blueprint_id UUID;
CREATE INDEX IF NOT EXISTS idx_project_schemas_source_blueprint
    ON kb.project_schemas (project_id, source_blueprint_id);

-- Pack dependency claims: one row per (blueprint, project, pack). A claim table
-- (rather than an integer counter) is idempotent under INSERT ON CONFLICT DO
-- NOTHING + DELETE, so retries after a mid-unapply crash converge safely.
CREATE TABLE IF NOT EXISTS kb.blueprint_pack_claims (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    blueprint_id UUID NOT NULL REFERENCES kb.blueprints(id),
    project_id UUID NOT NULL,
    schema_id UUID NOT NULL REFERENCES kb.graph_schemas(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT blueprint_pack_claims_project_blueprint_schema_key
        UNIQUE (project_id, blueprint_id, schema_id)
);
CREATE INDEX IF NOT EXISTS idx_blueprint_pack_claims_project_schema
    ON kb.blueprint_pack_claims (project_id, schema_id);

-- +goose Down
DROP TABLE IF EXISTS kb.blueprint_pack_claims;
ALTER TABLE kb.project_schemas DROP COLUMN IF EXISTS source_blueprint_id;
ALTER TABLE kb.agent_definitions DROP COLUMN IF EXISTS source_blueprint_id;
