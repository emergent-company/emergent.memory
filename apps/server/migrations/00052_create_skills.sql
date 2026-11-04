-- +goose Up
-- +goose StatementBegin

-- Skills table: reusable Markdown workflow instructions for agents.
-- Global skills (project_id IS NULL) are available to all agents in all projects.
-- Project-scoped skills (project_id IS NOT NULL) are available only within that project,
-- and override global skills of the same name.
CREATE TABLE kb.skills (
    id                      UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name                    TEXT        NOT NULL CHECK (name ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND char_length(name) BETWEEN 1 AND 64),
    description             TEXT        NOT NULL DEFAULT '',
    content                 TEXT        NOT NULL DEFAULT '',
    metadata                JSONB       NOT NULL DEFAULT '{}',
    description_embedding   vector(768),
    project_id              UUID        REFERENCES kb.projects(id) ON DELETE CASCADE,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Global skill name uniqueness: only one global (project_id IS NULL) skill per name.
CREATE UNIQUE INDEX idx_skills_name_global
    ON kb.skills (name)
    WHERE project_id IS NULL;

-- Per-project skill name uniqueness: only one skill per name within a project.
CREATE UNIQUE INDEX idx_skills_name_project
    ON kb.skills (name, project_id)
    WHERE project_id IS NOT NULL;

-- IVFFlat index for cosine similarity search on description embeddings.
-- 100 lists is the standard choice for tables up to ~1M rows.
CREATE INDEX idx_skills_embedding_ivfflat
    ON kb.skills USING ivfflat (description_embedding vector_cosine_ops)
    WITH (lists = 100);

-- Standard index to support project-scoped queries efficiently.
CREATE INDEX idx_skills_project_id
    ON kb.skills (project_id);

COMMENT ON TABLE kb.skills IS 'Reusable Markdown workflow instructions (skills) for agents. Global when project_id IS NULL; project-scoped otherwise.';
COMMENT ON COLUMN kb.skills.name IS 'Slug identifier: lowercase alphanumeric with hyphens, 1–64 chars';
COMMENT ON COLUMN kb.skills.description_embedding IS '768-dim embedding of description text for semantic retrieval (gemini-embedding-001 with MRL); NULL when embedding generation failed or is disabled';
COMMENT ON COLUMN kb.skills.project_id IS 'NULL for global skills; set for project-scoped skills. Project-scoped wins on name collision.';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS kb.skills;

-- +goose StatementEnd
