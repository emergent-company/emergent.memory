-- +goose Up
-- Per-project, per-blueprint model overrides. Keyed by (project_id,
-- blueprint_name, agent_name) — NOT by blueprint id — so a user's model mapping
-- survives a version upgrade (a new version is a new kb.blueprints row but the
-- same name). An empty model value is a valid override (project default).
CREATE TABLE IF NOT EXISTS kb.blueprint_model_overrides (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    project_id UUID NOT NULL,
    blueprint_name TEXT NOT NULL,
    agent_name TEXT NOT NULL,
    model TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT blueprint_model_overrides_project_name_agent_key
        UNIQUE (project_id, blueprint_name, agent_name)
);
CREATE INDEX IF NOT EXISTS idx_blueprint_model_overrides_project_name
    ON kb.blueprint_model_overrides (project_id, blueprint_name);

-- +goose Down
DROP TABLE IF EXISTS kb.blueprint_model_overrides;
