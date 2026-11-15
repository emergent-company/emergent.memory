-- +goose Up
-- +goose StatementBegin

-- A source workflow turns verified source events into board work items instead
-- of directly enqueuing agent runs. Config carries the reviewer lanes:
-- {"reviewers":[{"lane":"security","agent":"security-review"}]}.
CREATE TABLE IF NOT EXISTS kb.source_workflows (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  UUID NOT NULL REFERENCES kb.projects (id) ON DELETE CASCADE,
    source_id   UUID NOT NULL,
    config      JSONB NOT NULL DEFAULT '{}'::jsonb,
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT fk_source_workflows_source_project
        FOREIGN KEY (source_id, project_id) REFERENCES kb.sources (id, project_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_source_workflows_source ON kb.source_workflows (source_id);
CREATE INDEX IF NOT EXISTS idx_source_workflows_project ON kb.source_workflows (project_id);

ALTER TABLE kb.source_workflows ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS source_workflows_all ON kb.source_workflows;
CREATE POLICY source_workflows_all ON kb.source_workflows FOR ALL USING (
    project_id = NULLIF(current_setting('app.current_project_id'::text, true), '')::uuid
) WITH CHECK (
    project_id = NULLIF(current_setting('app.current_project_id'::text, true), '')::uuid
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS kb.source_workflows;
-- +goose StatementEnd
