## Why

`agent-scoped-mcp-endpoint` replaced the per-agent MCP share credential with
agent-owned endpoints and many labeled keys
(`core.agent_mcp_endpoints` / `core.agent_mcp_keys` /
`core.agent_mcp_sessions`). Migration
`00152_create_agent_mcp_endpoints.sql` already backfilled the new tables from
`core.agent_mcp_shares`, and the endpoint/key/session surface is live. Two
pieces of legacy schema are now dead:

1. `core.agent_mcp_shares` — superseded by the backfill; the legacy
   `AgentMCPShare` model/store was retained only as `// Deprecated:` for the
   backfill.
2. `core.mcp_share_instances.allowed_agents` — the project share-instance agent
   allowlist column. Project share instances are tools-only now; the column is
   unmapped by any Go code (a `// Deprecated:` note remains in
   `share_instance.go`).

Leaving them in place keeps dead schema and dead Go code on the books and
blocks the archive of the superseded capability. This change removes both.

## What Changes

- **Migration** `apps/server/migrations/00153_drop_legacy_mcp_share_schema.sql`:
  - Up: `DROP TABLE IF EXISTS core.agent_mcp_shares;` and
    `ALTER TABLE core.mcp_share_instances DROP COLUMN IF EXISTS allowed_agents;`
  - Down: faithfully recreates `core.agent_mcp_shares` exactly as `00148`
    defines it (columns, the partial unique indexes, the `core.api_tokens` FK)
    and re-adds `allowed_agents UUID[]` with its original `COMMENT ON COLUMN`
    text from `00150`/`00144`. The Down is fully reversible.
- **Dead Go code removal**: delete the deprecated `AgentMCPShare` struct and
  `agentMCPShareStore` (Bun store) from
  `apps/server/domain/mcp/agent_mcp_share.go`, its store test, and the unused
  `Service.agentShares` field/wiring. `CallAgentOnce`, `agentRunErrorResult`,
  and the agent/definition resolution helpers in the same file stay — they
  back the live agent endpoint.
- **Reference cleanup**: drop the deprecated `allowed_agents` note in
  `share_instance.go` and the `agent_mcp_shares` table block plus the
  `allowed_agents` column line from `apps/server/internal/testutil/schema.sql`.
- The legacy **read-only share** flow is intentionally untouched:
  `core.mcp_share_instances.is_legacy`, `share.go`
  (`HandleShareMCPAccess`, `recordLegacyShareInstance`) and their
  dependencies keep working. Only the dead agent allowlist column goes.

## Capabilities

### Modified Capabilities

- `agent-mcp-shares`: the main spec still documents the legacy
  `core.agent_mcp_shares` table and the "drop it in a later migration" clause.
  The delta records that the backfill has happened and the legacy table (plus
  the dead `allowed_agents` column) has now been dropped, while the endpoint /
  key / session tables and project share instances (including legacy rows)
  remain.

No `allowed_agents` main spec exists — the `mcp-share-instances` spec already
describes project instances as tools-only, so no delta is added there.

## Impact

- New migration `apps/server/migrations/00153_drop_legacy_mcp_share_schema.sql`
  (additive only; existing migrations are never edited).
- `apps/server/domain/mcp/agent_mcp_share.go`: legacy model + store removed.
- `apps/server/domain/mcp/agent_mcp_share_store_test.go`: deleted.
- `apps/server/domain/mcp/service.go`: `agentShares` field + wiring removed.
- `apps/server/domain/mcp/share_instance.go`: stale deprecated comment removed.
- `apps/server/internal/testutil/schema.sql`: legacy DDL blocks removed.
- No CLI, SDK, gateway/UI, API, or five-tool-surface changes.
