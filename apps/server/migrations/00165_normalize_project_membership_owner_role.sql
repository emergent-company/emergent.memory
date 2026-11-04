-- +goose Up
-- +goose StatementBegin

-- Normalise legacy project membership roles.
--
-- The canonical kb.project_memberships.role values are project_admin,
-- project_user, and project_viewer (see migration 00068). Standalone bootstrap
-- incorrectly wrote role = 'owner' for the bootstrapped project membership,
-- which no role check accepts (e.g. project_embedding_handler.go requires
-- project_admin) and produced a latent 403. 'owner' remains valid for
-- kb.organization_memberships and is left untouched there.
--
-- Idempotent: re-running is a no-op once normalised.
UPDATE kb.project_memberships
SET role = 'project_admin'
WHERE role = 'owner';

COMMENT ON COLUMN kb.project_memberships.role IS 'Member role: project_admin | project_user | project_viewer';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- No automatic reversal: 'owner' is not a valid project role and cannot be
-- distinguished from an intentional project_admin. Leaving normalised rows is
-- the safe behaviour.
-- +goose StatementEnd
