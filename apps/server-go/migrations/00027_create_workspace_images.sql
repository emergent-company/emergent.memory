-- +goose Up

-- Workspace Image Catalog: tracks available workspace images (built-in rootfs variants and custom Docker images).
-- Each project gets its own catalog entries, but Docker won't re-download images already present locally.
CREATE TABLE IF NOT EXISTS kb.workspace_images (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        VARCHAR(100)  NOT NULL,                -- User-facing alias (e.g. "coder", "py-ml")
    type        VARCHAR(20)   NOT NULL DEFAULT 'custom', -- 'built_in' or 'custom'
    docker_ref  TEXT,                                   -- Docker image reference (e.g. "python:3.12-slim"); NULL for built-in rootfs
    provider    VARCHAR(20)   NOT NULL DEFAULT 'firecracker', -- Which provider handles this: "firecracker" or "gvisor"
    status      VARCHAR(20)   NOT NULL DEFAULT 'pending',     -- 'pending', 'pulling', 'ready', 'error'
    error_msg   TEXT,                                   -- Error details if status='error'
    project_id  UUID          NOT NULL,                 -- Scoped per project
    created_at  TIMESTAMPTZ   NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMPTZ   NOT NULL DEFAULT CURRENT_TIMESTAMP
);

COMMENT ON TABLE kb.workspace_images IS 'Catalog of available workspace images per project';
COMMENT ON COLUMN kb.workspace_images.name IS 'User-facing alias used in agent definitions (e.g. "coder", "py-ml")';
COMMENT ON COLUMN kb.workspace_images.type IS 'Image type: built_in (pre-built rootfs) or custom (Docker pull)';
COMMENT ON COLUMN kb.workspace_images.docker_ref IS 'Docker image reference for custom images; NULL for built-in Firecracker rootfs';
COMMENT ON COLUMN kb.workspace_images.provider IS 'Provider that handles this image: firecracker or gvisor';
COMMENT ON COLUMN kb.workspace_images.status IS 'Image readiness: pending, pulling, ready, or error';

-- Unique constraint: one name per project
CREATE UNIQUE INDEX IF NOT EXISTS idx_workspace_images_project_name
    ON kb.workspace_images (project_id, name);

-- Fast lookup by project
CREATE INDEX IF NOT EXISTS idx_workspace_images_project_id
    ON kb.workspace_images (project_id);

-- +goose Down
DROP TABLE IF EXISTS kb.workspace_images;
