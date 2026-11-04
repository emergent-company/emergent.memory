-- +goose Up
-- Blueprint registry: versioned, immutable-on-publish blueprint definitions.
-- Blueprints are global (no project scoping). (name, version) is unique and
-- enforced atomically by the database to prevent TOCTOU duplicate inserts.
CREATE TABLE kb.blueprints (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name TEXT NOT NULL,
    version TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    author TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'draft',
    manifest JSONB NOT NULL DEFAULT '{}'::jsonb,
    checksum TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT blueprints_name_version_key UNIQUE (name, version)
);
CREATE INDEX idx_blueprints_name ON kb.blueprints(name);

-- +goose Down
DROP TABLE IF EXISTS kb.blueprints;
