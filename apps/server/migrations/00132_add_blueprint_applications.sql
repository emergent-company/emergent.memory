-- +goose Up
-- Blueprint applications: records which blueprint versions are applied to
-- which project, with the applied manifest checksum. (project_id, blueprint_id)
-- is unique — a re-apply updates the row — so the table reflects "currently
-- applied" state rather than an append-only history. This is the provenance
-- foundation for future drift detection and unapply.
CREATE TABLE kb.blueprint_applications (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    blueprint_id UUID NOT NULL REFERENCES kb.blueprints(id),
    project_id UUID NOT NULL,
    version TEXT NOT NULL,
    checksum TEXT NOT NULL DEFAULT '',
    applied_by UUID,
    status TEXT NOT NULL DEFAULT 'applied',
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT blueprint_applications_project_blueprint_key UNIQUE (project_id, blueprint_id)
);
CREATE INDEX idx_blueprint_applications_project ON kb.blueprint_applications(project_id);

-- +goose Down
DROP TABLE IF EXISTS kb.blueprint_applications;
