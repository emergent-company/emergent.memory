-- +goose Up
-- +goose StatementBegin
-- Org-scoped OAuth client configs (add-provider-oauth-connect §3, design D3).
--
-- Each row holds the OAuth2 client material for one provider for one
-- organization, so an org can bring its own app (BYO) instead of sharing the
-- deployment-default client. The client id and secret are stored together as a
-- single encrypted blob (AES-256-GCM, pkg/crypto, the same self-contained format
-- as source credentials); the secret is write-only and is never returned by the
-- API.
--
-- Resolution order when starting a connect is: org config → deployment env
-- default → actionable error.
--
-- No RLS policy: there is no org-scoped RLS context variable (RLS is keyed on
-- app.current_project_id only), so isolation is enforced in the application
-- layer by the shared org-administration check (pkg/auth.CanAdministerOrg) and
-- by org-keyed queries — matching kb.org_tool_settings.

CREATE TABLE IF NOT EXISTS kb.oauth_client_configs (
    id uuid PRIMARY KEY DEFAULT public.uuid_generate_v4(),
    organization_id uuid NOT NULL REFERENCES kb.orgs(id) ON DELETE CASCADE,
    provider text NOT NULL,
    encrypted_client bytea NOT NULL,
    scopes jsonb NOT NULL DEFAULT '[]'::jsonb,
    strategy text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_oauth_client_configs_org_provider
    ON kb.oauth_client_configs (organization_id, provider);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS kb.oauth_client_configs;
-- +goose StatementEnd
