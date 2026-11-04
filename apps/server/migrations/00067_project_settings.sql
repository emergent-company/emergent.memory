-- +goose Up
-- +goose StatementBegin
-- General-purpose project settings key/value store.
-- Each row stores a JSONB value keyed by (project_id, category, key).
-- Initial use: category='agent_override', key=<agent-name> for per-project
-- agent configuration overrides (model, prompt, tools, etc.).
CREATE TABLE IF NOT EXISTS kb.project_settings (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  UUID NOT NULL REFERENCES kb.projects(id) ON DELETE CASCADE,
    category    TEXT NOT NULL,
    key         TEXT NOT NULL,
    value       JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (project_id, category, key)
);

CREATE INDEX IF NOT EXISTS idx_project_settings_project_category
    ON kb.project_settings (project_id, category);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS kb.project_settings;
-- +goose StatementEnd
