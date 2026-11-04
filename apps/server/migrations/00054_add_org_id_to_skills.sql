-- +goose Up
-- +goose StatementBegin

-- Add org_id to kb.skills to support org-scoped skills.
-- Hierarchy: project (project_id set) > org (org_id set, project_id NULL) > global (both NULL, built-in only).
ALTER TABLE kb.skills ADD COLUMN org_id UUID REFERENCES kb.orgs(id) ON DELETE CASCADE;

-- Org-scoped skill name uniqueness: only one skill per name within an org (project_id must be NULL).
CREATE UNIQUE INDEX idx_skills_name_org
    ON kb.skills (name, org_id)
    WHERE project_id IS NULL AND org_id IS NOT NULL;

-- Enforce: project_id and org_id cannot both be set on the same row.
ALTER TABLE kb.skills ADD CONSTRAINT chk_skills_scope
    CHECK (NOT (project_id IS NOT NULL AND org_id IS NOT NULL));

-- Standard index to support org-scoped queries efficiently.
CREATE INDEX idx_skills_org_id
    ON kb.skills (org_id);

COMMENT ON COLUMN kb.skills.org_id IS 'NULL for global and project-scoped skills; set for org-wide skills. Mutually exclusive with project_id.';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE kb.skills DROP CONSTRAINT IF EXISTS chk_skills_scope;
DROP INDEX IF EXISTS idx_skills_name_org;
DROP INDEX IF EXISTS idx_skills_org_id;
ALTER TABLE kb.skills DROP COLUMN IF EXISTS org_id;

-- +goose StatementEnd
