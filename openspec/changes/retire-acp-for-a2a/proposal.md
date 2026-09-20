## Why

Memory shipped an in-house "ACP" interface (`/acp/v1/`, `/agent-chat/v1/`) that claims IBM/BeeAI Agent Communication Protocol v0.2.0 but never conformed to it, and that spec is dead (merged into A2A on 2025-08-25; repo archived 2025-08-27). The misleading "ACP" name has leaked through the server, SDK, CLI, MCP tool catalog, and DB schema. A2A v1.0 is now the canonical agent-interop surface and MCP is the real Model Context Protocol. This change retires ACP entirely so the two real protocols stand clean.

## What Changes

- **BREAKING** — Delete the ACP server implementation: `domain/agents/acp_handler.go`, `acp_routes.go`, `acp_dto.go` (routes `/acp/v1/` and `/agent-chat/v1/` removed).
- **BREAKING** — Delete `pkg/sdk/acp` and the `memory acp` CLI command group (`apps/cli/internal/cmd/acp.go`).
- **BREAKING** — Retire the `acp-*` MCP tools (`acp-list-agents`, `acp-trigger-run`, `acp-get-run-status`, `acp-get-run-events`); the `agent-*` MCP tools already cover listing and triggering.
- Migrate ACP-named identifiers to protocol-neutral or A2A naming: `AgentDefinition.ACPConfig`, `pkg/acpslug`, and the persistence schema (`kb.acp_sessions`, `kb.acp_run_events`, and the `acp_session_id` columns on `kb.agent_runs`, `kb.chat_conversations`, `kb.session_todos`, `kb.agent_share_links`).
- A2A v1.0 remains the sole agent-interop surface. MCP remains the real Model Context Protocol surface (knowledge/memory tools plus the per-agent endpoint). No ACP-named artifact remains.

## Capabilities

### New Capabilities

<!-- none -->

### Modified Capabilities

- `a2a-acp-migration`: ACP moves from deprecated to fully removed; the `acp-*` MCP tools are retired; the MCP surface is the sole real Model-Context-Protocol surface with no ACP-named artifacts.

## Impact

- **Server** — `domain/agents/` (`acp_*` files deleted; `mcp_tools.go` `acp-*` tools removed; `module.go` route wiring; `entity.go` + `a2a_discovery.go` `ACPConfig` rename), `pkg/acpslug` rename, migration(s) for the `kb.acp_*` schema.
- **SDK** — `pkg/sdk/acp` deleted; any first-party consumers repointed to `pkg/sdk/a2a`.
- **CLI** — `apps/cli/internal/cmd/acp.go` deleted; root command wiring updated.
- **Consumers** — gateway (alfred/web-ui) and SDK users of `ACPConfig` or `acp-*` tools repointed.
- **Tests** — `acp_*_test.go` removed; A2A and MCP suites preserved; new coverage for the naming migration.
