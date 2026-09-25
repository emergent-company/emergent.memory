## Context

The agent-scoped MCP endpoint feature is shipped and live. Its migration
`00152_create_agent_mcp_endpoints.sql` created
`core.agent_mcp_endpoints` / `core.agent_mcp_keys` /
`core.agent_mcp_sessions` and backfilled the first two from
`core.agent_mcp_shares`. That migration deliberately left the legacy table and
the unused `core.mcp_share_instances.allowed_agents` column in place, to be
dropped later once no reader remained. No Go code reads either object today:
the only remaining reader was the deprecated `agentMCPShareStore`, which is no
longer called from any live path.

## Decisions

- **Drop in one migration, after the backfill has shipped.** `00153` is additive
  and strictly ordered after `00152`, so the backfill always runs before the
  drop on a fresh chain. Dropping in `00152` itself would have destroyed the
  source rows before the backfill could read them.
- **The Down is a faithful inverse.** `agent_mcp_shares` is recreated exactly as
  `00148` defines it — columns, `uq_agent_mcp_shares_project_name` (partial,
  `(project_id, lower(name)) WHERE revoked_at IS NULL`),
  `uq_agent_mcp_shares_active_token` (partial, `token_id WHERE revoked_at IS
  NULL`), `idx_agent_mcp_shares_project_agent`, and the
  `token_id → core.api_tokens(id) ON DELETE CASCADE` FK — and
  `allowed_agents UUID[]` is re-added with its original comment. Nothing is
  lost on rollback.
- **`allowed_agents` is not worth preserving in code.** It has been unmapped by
  the `MCPShareInstance` Bun model since the agent-scoping change; only a stale
  `// Deprecated:` comment remained. The column itself is dropped; the comment
  goes with it.
- **Do not touch the legacy share flow.** `is_legacy`, `HandleShareMCPAccess`,
  and `recordLegacyShareInstance` are the read-only `/mcp/share` path and stay
  byte-for-byte intact — only the agent allowlist column is removed.
- **Keep the shared helpers.** `CallAgentOnce`, `agentRunErrorResult`,
  `resolveProjectAgent`, and `resolveAgentShareTarget` live in
  `agent_mcp_share.go` but serve the live agent endpoint; they are retained.

## Risks / mitigations

- **A reader was missed.** Grep the whole repo for `agent_mcp_shares`,
  `AgentMCPShare`, and `allowed_agents` after removal; the only expected hits
  are historical migrations (`00148`, `00150`, and the `00152` backfill
  comments), the new `00153` migration, and archive docs.
- **Fresh-chain breakage.** Apply the full migration chain from scratch against
  an empty Postgres and confirm `00152`'s backfill runs before `00153` drops the
  source table.
- **Down/Up asymmetry.** Run Down once and assert both objects are restored,
  then re-apply Up.
