-- +goose Up
-- +goose StatementBegin

-- Public keyed agent-share: an owner mints a share link carrying a reserved
-- `share:agent-chat` API token. The gateway presents that token as a Bearer
-- credential; the server resolves token -> link -> agent definition -> project.
-- No unauthenticated routes exist: the share key is an emt_* token.

-- 1. Binding table: one row per minted share link, bound to an agent
--    definition and the reserved-scope API token that carries it.
CREATE TABLE IF NOT EXISTS kb.agent_share_links (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES kb.projects(id) ON DELETE CASCADE,
    agent_definition_id UUID NOT NULL REFERENCES kb.agent_definitions(id) ON DELETE CASCADE,
    api_token_id UUID NOT NULL UNIQUE REFERENCES core.api_tokens(id) ON DELETE CASCADE,
    label VARCHAR(255) NOT NULL,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by_user_id UUID REFERENCES core.user_profiles(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ
);

-- One active link per (definition, case-insensitive label). Revoked links may
-- be re-created with the same label.
CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_share_links_def_label_active
    ON kb.agent_share_links (agent_definition_id, lower(label))
    WHERE revoked_at IS NULL;

-- Project listing of active links.
CREATE INDEX IF NOT EXISTS idx_agent_share_links_project_active
    ON kb.agent_share_links (project_id)
    WHERE revoked_at IS NULL;

-- 2. Session join table: links a share link to an ACP session, keyed by the
--    end-user's anonymous reference. Run linkage rides on kb.agent_runs
--    .acp_session_id (no new columns on kb.acp_sessions).
CREATE TABLE IF NOT EXISTS kb.agent_share_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    share_link_id UUID NOT NULL REFERENCES kb.agent_share_links(id) ON DELETE CASCADE,
    acp_session_id UUID NOT NULL REFERENCES kb.acp_sessions(id) ON DELETE CASCADE,
    end_user_ref VARCHAR(64) NOT NULL,
    title VARCHAR(255),
    last_activity_at TIMESTAMPTZ,
    is_archived BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_share_sessions_acp_link
    ON kb.agent_share_sessions (acp_session_id, share_link_id);

CREATE INDEX IF NOT EXISTS idx_agent_share_sessions_user
    ON kb.agent_share_sessions (share_link_id, end_user_ref, is_archived, last_activity_at DESC);

-- 3. End users: anonymous identity keyed by end_user_ref (never email). Email
--    is consent/contact metadata only; unverified email grants no access.
CREATE TABLE IF NOT EXISTS kb.agent_share_end_users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    share_link_id UUID NOT NULL REFERENCES kb.agent_share_links(id) ON DELETE CASCADE,
    end_user_ref VARCHAR(64) NOT NULL,
    email VARCHAR(255),
    email_normalized VARCHAR(255),
    verified BOOLEAN NOT NULL DEFAULT FALSE,
    consent_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_share_end_users_ref
    ON kb.agent_share_end_users (share_link_id, end_user_ref);

CREATE INDEX IF NOT EXISTS idx_agent_share_end_users_email
    ON kb.agent_share_end_users (share_link_id, email_normalized);

-- 4. Usage/budget counters: one row per (link, rolling period).
CREATE TABLE IF NOT EXISTS kb.agent_share_usage (
    link_id UUID NOT NULL REFERENCES kb.agent_share_links(id) ON DELETE CASCADE,
    period_start TIMESTAMPTZ NOT NULL,
    messages INT NOT NULL DEFAULT 0,
    tokens BIGINT NOT NULL DEFAULT 0,
    cost_usd NUMERIC NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (link_id, period_start)
);

-- 5. Audit: provenance on tool-approval rows for share runs.
ALTER TABLE kb.agent_tool_approvals
    ADD COLUMN IF NOT EXISTS share_link_id UUID REFERENCES kb.agent_share_links(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_agent_tool_approvals_share_link_id
    ON kb.agent_tool_approvals (share_link_id);

-- 6. Audit: hashed-IP share-access log. Only a SHA-256 hex digest of the client
--    IP is stored (never a raw IP); action is one of stream, session_create,
--    approve, deny.
CREATE TABLE IF NOT EXISTS kb.agent_share_access_log (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    share_link_id UUID NOT NULL REFERENCES kb.agent_share_links(id) ON DELETE CASCADE,
    end_user_ref VARCHAR(64) NOT NULL,
    ip_hash VARCHAR(64) NOT NULL,
    action VARCHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_agent_share_access_log_link
    ON kb.agent_share_access_log (share_link_id, created_at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_agent_share_access_log_link;
DROP TABLE IF EXISTS kb.agent_share_access_log;
DROP INDEX IF EXISTS idx_agent_tool_approvals_share_link_id;
ALTER TABLE kb.agent_tool_approvals DROP COLUMN IF EXISTS share_link_id;
DROP TABLE IF EXISTS kb.agent_share_usage;
DROP TABLE IF EXISTS kb.agent_share_end_users;
DROP TABLE IF EXISTS kb.agent_share_sessions;
DROP TABLE IF EXISTS kb.agent_share_links;
-- +goose StatementEnd
