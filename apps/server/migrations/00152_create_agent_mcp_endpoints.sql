-- +goose Up
-- +goose StatementBegin
-- Agent-scoped MCP endpoints. An endpoint is agent-owned (one active endpoint
-- per agent, enforced by a partial unique index) and lifetime follows the agent
-- via ON DELETE CASCADE, since kb.agents hard-deletes.
CREATE TABLE core.agent_mcp_endpoints (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  UUID NOT NULL,
    agent_id    UUID NOT NULL REFERENCES kb.agents(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at  TIMESTAMPTZ
);

-- One active endpoint per agent; revoked endpoints release the agent for reuse.
CREATE UNIQUE INDEX uq_agent_mcp_endpoints_agent
    ON core.agent_mcp_endpoints (agent_id)
    WHERE revoked_at IS NULL;

-- Many labeled keys per endpoint. Each key binds one core.api_tokens credential
-- to the endpoint; token_id is UNIQUE so a credential maps to exactly one
-- endpoint/agent (the guarantee previously held by uq_agent_mcp_shares_active_token).
CREATE TABLE core.agent_mcp_keys (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    endpoint_id UUID NOT NULL REFERENCES core.agent_mcp_endpoints(id) ON DELETE CASCADE,
    token_id    UUID NOT NULL UNIQUE REFERENCES core.api_tokens(id) ON DELETE CASCADE,
    label       TEXT NOT NULL,
    created_by  UUID,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at  TIMESTAMPTZ
);

CREATE INDEX idx_agent_mcp_keys_endpoint ON core.agent_mcp_keys (endpoint_id);

-- One active label per endpoint; revoked keys release their label for reuse.
CREATE UNIQUE INDEX uq_agent_mcp_keys_endpoint_label
    ON core.agent_mcp_keys (endpoint_id, lower(label))
    WHERE revoked_at IS NULL;

-- Session ownership/metadata only. History stays in the ADK session store under
-- session:<projectID>:<sessionRef>; sessions are key-scoped, never endpoint-scoped.
CREATE TABLE core.agent_mcp_sessions (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    endpoint_id    UUID NOT NULL REFERENCES core.agent_mcp_endpoints(id) ON DELETE CASCADE,
    key_id         UUID NOT NULL REFERENCES core.agent_mcp_keys(id) ON DELETE CASCADE,
    session_ref    TEXT NOT NULL,
    status         TEXT NOT NULL DEFAULT 'active',
    turn_count     INT  NOT NULL DEFAULT 0,
    total_steps    INT  NOT NULL DEFAULT 0,
    last_run_id    UUID,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_active_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at     TIMESTAMPTZ
);

CREATE UNIQUE INDEX uq_agent_mcp_sessions_ref ON core.agent_mcp_sessions (session_ref);
CREATE INDEX idx_agent_mcp_sessions_key ON core.agent_mcp_sessions (key_id);
-- +goose StatementEnd

-- +goose StatementBegin
-- Backfill: carry existing per-agent shares forward so live credentials keep
-- working through the cutover. Tokens and scopes are not touched.
--
-- Endpoints: one per distinct (project_id, agent_id) in core.agent_mcp_shares,
-- including revoked shares so the mapping is deterministic. The join to
-- kb.agents skips orphaned shares whose agent was hard-deleted before this
-- migration added the FK (agent_mcp_shares had no FK and kb.agents hard-deletes);
-- those shares cannot have an endpoint and are dropped rather than failing the
-- migration. Every endpoint is inserted active (revoked_at NULL) because the
-- partial unique index allows only one active endpoint per agent. DISTINCT keys
-- by agent_id, so the index holds.
--
-- Keys: one per share row, copying token_id/created_by/timestamps. Revoked
-- shares carry over as revoked keys, so a revoked credential is not silently
-- reactivated and a revoked label stays free. Shares with no endpoint (orphaned
-- agent) are skipped by the join. Both INSERT ... SELECT statements are no-ops
-- when core.agent_mcp_shares is empty (no source rows).
--
-- core.agent_mcp_shares and core.mcp_share_instances.allowed_agents are left in
-- place; dropping them is deferred to a later change.
INSERT INTO core.agent_mcp_endpoints (project_id, agent_id)
SELECT DISTINCT s.project_id, s.agent_id
FROM core.agent_mcp_shares s
JOIN kb.agents a ON a.id = s.agent_id
WHERE NOT EXISTS (
    SELECT 1 FROM core.agent_mcp_endpoints e WHERE e.agent_id = s.agent_id
);

INSERT INTO core.agent_mcp_keys (endpoint_id, token_id, label, created_by, created_at, updated_at, revoked_at)
SELECT e.id, s.token_id, s.name, s.created_by, s.created_at, s.updated_at, s.revoked_at
FROM core.agent_mcp_shares s
JOIN core.agent_mcp_endpoints e ON e.agent_id = s.agent_id
WHERE NOT EXISTS (
    SELECT 1 FROM core.agent_mcp_keys k WHERE k.token_id = s.token_id
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Reverse dependency order: sessions -> keys -> endpoints.
DROP TABLE IF EXISTS core.agent_mcp_sessions;
DROP TABLE IF EXISTS core.agent_mcp_keys;
DROP TABLE IF EXISTS core.agent_mcp_endpoints;
-- +goose StatementEnd
