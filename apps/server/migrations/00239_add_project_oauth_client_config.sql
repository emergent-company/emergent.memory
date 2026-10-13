-- +goose Up
-- +goose StatementBegin
-- Project-scoped OAuth client configs (add-datasource-oauth-app-setup).
--
-- Extends kb.oauth_client_configs so a project can bring its own OAuth2 client
-- in addition to the existing org-wide config. Existing rows stay org-wide:
-- project_id is NULL for them, and the org-wide partial unique index keeps them
-- unique on (organization_id, provider). A project override sets project_id and
-- is unique on (project_id, provider); it still carries its owning
-- organization_id (the column stays NOT NULL) so org membership is preserved.
--
-- Resolution order when starting a connect is now: project config → org config
-- → deployment env default → actionable error.
ALTER TABLE kb.oauth_client_configs
    ADD COLUMN IF NOT EXISTS project_id uuid REFERENCES kb.projects(id) ON DELETE CASCADE;

-- The org-wide uniqueness now applies only to org-wide rows; project overrides
-- get their own uniqueness on (project_id, provider).
DROP INDEX IF EXISTS idx_oauth_client_configs_org_provider;

CREATE UNIQUE INDEX IF NOT EXISTS idx_oauth_client_configs_org_provider
    ON kb.oauth_client_configs (organization_id, provider)
    WHERE project_id IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_oauth_client_configs_project_provider
    ON kb.oauth_client_configs (project_id, provider)
    WHERE project_id IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_oauth_client_configs_project_provider;
DROP INDEX IF EXISTS idx_oauth_client_configs_org_provider;

-- Project-scoped rows have no meaning once the column is gone; drop them so the
-- restored org-wide unique index cannot collide on a leftover override.
DELETE FROM kb.oauth_client_configs WHERE project_id IS NOT NULL;

ALTER TABLE kb.oauth_client_configs DROP COLUMN IF EXISTS project_id;

CREATE UNIQUE INDEX IF NOT EXISTS idx_oauth_client_configs_org_provider
    ON kb.oauth_client_configs (organization_id, provider);
-- +goose StatementEnd
