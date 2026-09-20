## Context

The A2A v1.0 facade (`domain/agents/a2a_*`) already supersedes the in-house "ACP" surface and reuses its persistence (`kb.acp_sessions` → A2A `contextId`, `kb.acp_run_events` → task history). The ACP routes/SDK/CLI/tools now exist only behind `Deprecation`/`Sunset` headers. Motivation is in proposal.md.

Blast radius confirmed by inspection:
- ACP impl: `acp_handler.go`, `acp_routes.go`, `acp_dto.go` (+ tests), `pkg/sdk/acp/client.go`, `apps/cli/internal/cmd/acp.go`.
- `acp-*` MCP tools already carry `[Deprecated: use trigger_agent or the A2A POST /message:send surface]` descriptions.
- `AgentDefinition.ACPConfig` (`entity.go:368`, JSON `acpConfig`, bun `acp_config,type:jsonb`) is consumed by both the ACP manifest (`acp_dto.go`) and the A2A skill mapping (`a2a_discovery.go:AgentDefinitionToSkill`), and is serialized in `AgentDefinitionDTO` (`dto.go:341/401/426`).
- `pkg/acpslug` (`FromName`, RFC 1123 slug) is imported by the agents domain and the MCP domain (shared to avoid an import cycle).

## Goals / Non-Goals

**Goals:**
- Delete every ACP implementation artifact (routes, handler, DTO, SDK, CLI).
- Retire the `acp-*` MCP tools with zero capability loss (each maps to an existing `agent-*` tool).
- Rename the misleading `ACPConfig`/`acpslug` identifiers to A2A/protocol-neutral names.
- Keep A2A and MCP surfaces behaviorally unchanged.

**Non-Goals:**
- No change to the `agent-*` MCP tools, the A2A wire contract, or the per-agent MCP endpoint.
- No new protocol surface; no stdio Agent Client Protocol server (out of scope for this change).
- Renaming the `kb.acp_sessions` / `kb.acp_run_events` tables (deferred, see Decisions).

## Decisions

### D1 — Delete ACP server, SDK, and CLI in one change
Delete `acp_handler.go`, `acp_routes.go`, `acp_dto.go` (+ their tests), `pkg/sdk/acp`, and `apps/cli/internal/cmd/acp.go`. Unregister `RegisterACPRoutes` from `domain/agents/module.go`.

*Rationale:* the deprecation window has passed; keeping the dead code only preserves the misleading name. *Alternative:* keep routes behind headers longer — rejected, the spec (IBM/BeeAI ACP) is archived and no conforming client exists.

### D2 — Retire `acp-*` MCP tools, mapping each to an `agent-*` equivalent
Remove the four `acp-*` tool definitions and their handlers from `domain/agents/mcp_tools.go`:
- `acp-list-agents` → `agent-list`
- `acp-trigger-run` → `trigger_agent`
- `acp-get-run-status` → `agent-run-status`
- `acp-get-run-events` → `agent-run-messages` / `agent-run-tool-calls`

*Rationale:* the `agent-*` family is already the canonical MCP agent surface and already wins the tool-description catalog injection in `trigger_agent` (`mcp_tools.go:1411`). *Alternative:* repoint `acp-*` to A2A semantics — rejected, they are pure duplicates.

### D3 — Rename `ACPConfig` → `A2AConfig` (type, field, wire, storage)
Rename the Go type `ACPConfig` → `A2AConfig`, the `AgentDefinition.ACPConfig` field → `A2AConfig`, the JSON tag `acpConfig` → `a2aConfig`, and the bun tag `acp_config` → `a2a_config`. Update `AgentDefinitionDTO` (`dto.go`) and the gateway/SDK consumers (alfred/web-ui) in lockstep.

*Rationale:* the struct only feeds A2A now (the ACP consumer is deleted), so the ACP name is pure misdirection. *Alternative:* rename the Go type but keep the JSON/DB field `acpConfig` for wire compatibility — rejected, that leaves the misleading name in the contract. This is **BREAKING** for the gateway/web-ui agent-definition serialization, so it must land together with those repos.

### D4 — Rename `pkg/acpslug` → `pkg/agentslug`
Rename the package and its `FromName` doc references. It is a shared helper (agents + MCP domains); the rename is mechanical and keeps the same function signature.

*Rationale:* the helper is a generic slug, not an ACP concept. *Alternative:* inline it into the two consumers — rejected, it exists precisely to avoid an import cycle.

### D5 — Defer the persistence table rename
Keep `kb.acp_sessions`, `kb.acp_run_events`, and the `acp_session_id` columns unchanged for now. The spec scenario permits "renamed or retained".

*Rationale:* those tables are referenced by `kb.session_todos`, `kb.chat_conversations`, `kb.agent_share_links`, `kb.agent_runs` and six migrations (00082, 00095, 00101, 00102, 00115, 00116, 00160). Renaming them is a broad FK/index migration with no user-visible payoff (the name is internal). *Alternative:* rename in this change — rejected to bound risk; can be a dedicated follow-up.

## Risks / Trade-offs

- **[BREAKING wire + DB rename of `ACPConfig`]** gateway/web-ui deserialize `acpConfig`; renaming to `a2aConfig` breaks them → land server + gateway/web-ui + SDK together; add a temporary read-compat shim if repos can't ship atomically.
- **[Orphaned ACP references]** any leftover import of `pkg/sdk/acp` or `acpslug` breaks the build → grep-driven task list; CI `go build ./...` + `go vet` gate each step.
- **[Capability loss in MCP]** an agent relying on `acp-*` loses the tool → mitigated by D2 mapping; the `agent-*` equivalents cover list/trigger/status/messages.
- **[A2A regression]** deleting ACP must not disturb the reused persistence → keep `a2a_*` tests green; the A2A suite is the regression net.
- **[Table-rename scope creep]** tempting to "fully retire" naming → bounded by D5; record the rename as a follow-up change rather than expanding this one.

## Migration Plan

1. Land deletion + retirement + renames in one server/CLI/SDK change; run `go build ./...`, `go test ./...` (unit), `task lint`.
2. Coordinate the `a2aConfig`/`a2a_config` rename with the gateway (alfred/web-ui) and any SDK consumer so serialization stays consistent.
3. Run the e2e agent suites (web-ui Playwright + server integration) to confirm A2A discovery/message-flow and MCP agent tools are unaffected.
4. Rollback: the ACP routes/SDK/CLI are dead — if a legacy client is discovered mid-deploy, re-add the deprecated routes from git history rather than blocking the cleanup.

## Open Questions

- Whether the `kb.acp_sessions`/`kb.acp_run_events` table rename should be folded into this change or tracked as its own follow-up change (recommended: follow-up).
