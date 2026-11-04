-- +goose Up
-- +goose StatementBegin
-- OAuth 2.1 authorization-server core (emergent.feedback is the first consumer).
--
-- The Memory API acts as a narrow, project-scoped authorization server for
-- third-party apps. Zitadel stays the login provider; a user who is already
-- authenticated in the Memory gateway consents to a specific project and scope
-- set, and the app exchanges an authorization code (PKCE S256 only) for a
-- project-scoped `emt_` API token.
--
-- Nothing here stores a client secret: every registered client is public
-- (token_endpoint_auth_method = 'none'). Authorization codes are single-use and
-- stored only as a SHA-256 hash. Grants link a client to the API token minted
-- for it so the token can be revoked from the grants surface.

CREATE TABLE IF NOT EXISTS core.oauth_clients (
    id                        uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id                 text        UNIQUE NOT NULL,
    client_name               text        NOT NULL,
    redirect_uris             jsonb       NOT NULL,
    grant_types               jsonb       NOT NULL DEFAULT '["authorization_code"]'::jsonb,
    token_endpoint_auth_method text       NOT NULL DEFAULT 'none',
    scope                     text        NOT NULL DEFAULT '',
    created_at                timestamptz NOT NULL DEFAULT now(),
    last_used_at              timestamptz,
    revoked_at                timestamptz
);

COMMENT ON TABLE core.oauth_clients IS 'Dynamically registered OAuth 2.1 public clients (RFC 7591); no client secrets are stored';

CREATE TABLE IF NOT EXISTS core.oauth_authorization_codes (
    id                    uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    code_hash             text        UNIQUE NOT NULL,
    client_id             text        NOT NULL,
    user_id               uuid        NOT NULL,
    project_id            uuid        NOT NULL,
    scopes                jsonb       NOT NULL,
    redirect_uri          text        NOT NULL,
    code_challenge        text        NOT NULL,
    code_challenge_method text        NOT NULL DEFAULT 'S256',
    expires_at            timestamptz NOT NULL,
    consumed_at           timestamptz,
    created_at            timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE core.oauth_authorization_codes IS 'Single-use OAuth authorization codes stored only as a SHA-256 hash; 5 minute TTL';

CREATE TABLE IF NOT EXISTS core.oauth_grants (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id    text        NOT NULL,
    user_id      uuid        NOT NULL,
    project_id   uuid        NOT NULL,
    scopes       jsonb       NOT NULL,
    token_id     uuid        NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    revoked_at   timestamptz
);

COMMENT ON TABLE core.oauth_grants IS 'Links a registered OAuth client to the project-scoped API token minted for it';

-- oauth_clients.client_id and oauth_authorization_codes.code_hash are already
-- indexed by their UNIQUE constraints; the grant lookups need explicit indexes.
CREATE INDEX IF NOT EXISTS idx_oauth_grants_user_id ON core.oauth_grants (user_id);
CREATE INDEX IF NOT EXISTS idx_oauth_grants_client_id ON core.oauth_grants (client_id);
CREATE INDEX IF NOT EXISTS idx_oauth_grants_token_id ON core.oauth_grants (token_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS core.oauth_grants;
DROP TABLE IF EXISTS core.oauth_authorization_codes;
DROP TABLE IF EXISTS core.oauth_clients;
-- +goose StatementEnd
