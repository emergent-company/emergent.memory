-- +goose Up

-- Email jobs were written without the project they belong to, so a delivery
-- failure could only be inspected in the deployment-wide superadmin view. Every
-- producer in this codebase is project-scoped (project-invitation and
-- mcp-invite), so the column is nullable only for the historical rows and for
-- any future account-scoped mail.
ALTER TABLE kb.email_jobs
    ADD COLUMN IF NOT EXISTS project_id uuid;

-- Backfill invitations from their source row (source_type = 'invite').
UPDATE kb.email_jobs ej
SET project_id = i.project_id
FROM kb.invites i
WHERE ej.source_type = 'invite'
  AND ej.source_id = i.id
  AND ej.project_id IS NULL;

-- Backfill MCP share invites. They carry the project in template_data but
-- never set source_type/source_id, so this is the only mapping available.
-- CASE, not a bare cast: the regex guard and the cast are not guaranteed to be
-- evaluated in source order, and one malformed historical value must not abort
-- the migration.
UPDATE kb.email_jobs
SET project_id = CASE
    WHEN template_data->>'projectId' ~ '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$'
        THEN (template_data->>'projectId')::uuid
    ELSE NULL
END
WHERE template_name = 'mcp-invite'
  AND project_id IS NULL
  AND template_data ? 'projectId';

CREATE INDEX IF NOT EXISTS idx_email_jobs_project_status
    ON kb.email_jobs (project_id, status)
    WHERE project_id IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS kb.idx_email_jobs_project_status;
ALTER TABLE kb.email_jobs DROP COLUMN IF EXISTS project_id;
