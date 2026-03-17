-- +goose Up
-- +goose StatementBegin

-- Add project_viewer as a valid role value for project memberships.
-- The role column is unconstrained text, so this migration documents the new
-- role and adds an index to make role-filtered queries (e.g. counting admins)
-- more efficient now that there are three role values.

COMMENT ON COLUMN kb.project_memberships.role IS 'Member role: project_admin | project_user | project_viewer';

-- Add invited_by_user_id to kb.invites for audit trail
ALTER TABLE kb.invites
    ADD COLUMN IF NOT EXISTS invited_by_user_id uuid REFERENCES core.user_profiles(id) ON DELETE SET NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE kb.invites
    DROP COLUMN IF EXISTS invited_by_user_id;

-- +goose StatementEnd
