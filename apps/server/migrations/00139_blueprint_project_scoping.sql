-- +goose Up
-- Blueprints become project-scoped: project_id IS NULL = global (shared
-- built-ins), project_id = <project> = private (a caller's own drafts).
-- The old global UNIQUE (name, version) is replaced by two partial unique
-- indexes so a private name/version can coexist with a global one, and each
-- project's private names are unique only within that project.
ALTER TABLE kb.blueprints ADD COLUMN project_id UUID;

ALTER TABLE kb.blueprints DROP CONSTRAINT IF EXISTS blueprints_name_version_key;

CREATE UNIQUE INDEX blueprints_name_version_global_key
    ON kb.blueprints (name, version) WHERE project_id IS NULL;

CREATE UNIQUE INDEX blueprints_name_version_project_key
    ON kb.blueprints (name, version, project_id) WHERE project_id IS NOT NULL;

CREATE INDEX blueprints_project_id_idx ON kb.blueprints (project_id);

-- +goose Down
DROP INDEX IF EXISTS blueprints_name_version_global_key;
DROP INDEX IF EXISTS blueprints_name_version_project_key;
DROP INDEX IF EXISTS blueprints_project_id_idx;

ALTER TABLE kb.blueprints DROP COLUMN project_id;

ALTER TABLE kb.blueprints ADD CONSTRAINT blueprints_name_version_key UNIQUE (name, version);
