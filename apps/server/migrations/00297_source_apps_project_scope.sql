-- +goose Up
-- +goose StatementBegin

-- Hybrid source App ownership: NULL = org-wide (org admin), set = project-owned
-- (project admin). The App is still one credential per GitHub App per org: a
-- project-owned row is a distinct GitHub App with its own webhook key, while an
-- org-wide row can be bound by every project in the org.
ALTER TABLE kb.source_apps
    ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES kb.projects (id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS idx_source_apps_project
    ON kb.source_apps (project_id);

-- Existing rows stay org-wide (project_id NULL); nothing to backfill.

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS kb.idx_source_apps_project;
ALTER TABLE kb.source_apps DROP COLUMN IF EXISTS project_id;
-- +goose StatementEnd
