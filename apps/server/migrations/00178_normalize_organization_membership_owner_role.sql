-- +goose Up
-- +goose StatementBegin

-- Normalise legacy organization membership roles.
--
-- The canonical kb.organization_memberships.role value is org_admin
-- (see domain/orgs/repository.go). Standalone bootstrap previously wrote
-- role = 'owner' for the bootstrapped organization membership, which no
-- org-scoped decision point accepts (e.g. apitoken.CanGrantAdminAll checks
-- org_admin, and the project-transfer check requires org_admin). 'owner' is
-- not a valid organization membership role and is written by no server code
-- path. This mirrors migration 00165 for kb.project_memberships.
--
-- Idempotent: re-running is a no-op once normalised.
UPDATE kb.organization_memberships
SET role = 'org_admin'
WHERE role = 'owner';

COMMENT ON COLUMN kb.organization_memberships.role IS 'Member role: org_admin';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- No automatic reversal: 'owner' is not a valid organization membership role
-- and cannot be distinguished from an intentional org_admin. Leaving
-- normalised rows is the safe behaviour.
-- +goose StatementEnd
