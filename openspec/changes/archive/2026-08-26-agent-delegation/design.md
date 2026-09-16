## Context

Alfred's brain is Emergent Memory (vision D7); the Go gateway is the single client-facing origin and mirrors memory's `AgentDefinition` for agent CRUD. Agent-to-agent (A2A) is currently deferred (D9). A source investigation (exp-1, against `/root/emergent.memory`) confirmed memory already ships a complete A2A surface — the work here is to surface a thin per-agent config on top of it, not to build new orchestration. See proposal.md for motivation; specs/agent-delegation/spec.md for requirements.

## Goals / Non-Goals

**Goals:**

- Add a per-agent `delegation` config (enable + explicit target allowlist) through the gateway, mapped onto memory's existing A2A tools — no memory-service code changes.
- Grant a delegation-enabled agent the ability to list its allowed targets and delegate (spawn) work to them.
- Document the memory-sharing semantics of delegation (the "memory possibilities" investigation) so behavior is understood, not accidental.

**Non-Goals:**

- No full A2A orchestration UI or cross-agent mesh; single hop per delegation.
- No cross-project delegation (single-owner, single project — D6).
- No cancel/suspend tool surfaced to agents (memory does not expose one to agents).
- No revival of the legacy Python `sub_agents` field (`agent/api/models.py`) — that stack is being retired.
- No queued/dispatch-mode changes in v1; sync delegation only (queued dispatch already works if the target sets `DispatchMode`, and needs no gateway work).

## Decisions

### D1 — Config shape: `delegation: { enabled, targets[] }` mapped to memory's `Tools` + `Config.spawnPolicy.allow`

The gateway agent model gains a `delegation` field:

```json
{ "enabled": false, "targets": [] }
```

- `enabled=false` → the gateway strips the gateway-managed A2A tools (`spawn_agents`, `list_available_agents`) from `Tools` and removes `Config.spawnPolicy` → cannot delegate. This is the default, and makes revocation clean (spec: revoke-immediately). The two tool names are gateway-managed, so disabling always removes them even if the client re-sends a stale `Tools` list.
- `enabled=true` → requires non-empty `targets`; the gateway adds `spawn_agents` and `list_available_agents` to the `AgentDefinition.Tools` whitelist and sets `Config.spawnPolicy.allow = targets`.

This reuses memory's existing gating (see investigation): coordination tools are injected **only when** `spawn_agents`/`list_available_agents` are listed in a non-empty `Tools` whitelist (executor.go:2639-2650), and `spawnPolicy.allow` both filters `list_available_agents` output and rejects non-listed spawns (coordination_tools.go:46-73, 179, 326).

*Alternatives considered:* (a) an unrestricted "delegate to any project agent" mode via empty `spawnPolicy.allow` (memory's "open policy") — rejected because an explicit allowlist is safer and matches the spec's "explicit list" requirement; (b) a separate memory-service feature — rejected, adds a dependency and code churn for zero new capability.

### D2 — Use the ADK coordination tools (`spawn_agents` + `list_available_agents`), not MCP `trigger_agent` / ACP

In-agent delegation should use the in-process coordination tools that memory injects into the LLM pipeline (`coordination_tools.go`): `spawn_agents` (params: `agents[]` of `{agent_name, task, timeout?, resume_run_id?}` + per-spawn `context`) and `list_available_agents` (no params). These are exactly what D9 means by "grant the tools."

`trigger_agent` (mcp_tools.go) is an MCP tool that is additionally ACP-opt-in and stripped from agents unless explicitly whitelisted or globbed (`applyACPRestrictions`, toolpool.go:513); it is the external-client/ACP entry point, not the natural in-agent path.

*Alternatives considered:* `trigger_agent` via MCP — rejected for the in-agent loop; it targets external callers and adds ACP-gating noise.

### D3 — Sync dispatch; result returned inline

`spawn_agents` runs sync by default and returns `SpawnResult.Summary` (the child's `final_response`) directly to the delegating agent. For a queued target (`DispatchMode=queued`), memory reenqueues the parent with an `AGENT_COMPLETE` message, so the result still reaches the supervisor without gateway code. v1 therefore needs no result-polling plumbing and no additional status tools (`agent-run-status`/`agent-run-get` are left out of the grant).

### D4 — Memory/context sharing is emergent; document, don't build

The investigation confirmed the memory-sharing model (this is the "memory possibilities" deliverable):

- **Same project scope** — supervisor and sub-agent share the same knowledge graph (project-filtered), so a sub-agent can recall facts the supervisor persisted.
- **Trigger message** — `spawn_agents`'s `task` becomes the child's user message; the child keeps its own system prompt.
- **Context passthrough** — parent metadata merges with the per-spawn `context` map and is injected as a `<context>…</context>` prefix on the child's first turn.
- **Write-back** — sync: `SpawnResult.Summary`; queued: `AGENT_COMPLETE` reenqueue; suspended: FunctionResponse. Parent/child linked via `parent_run_id`/`root_run_id`/`trace_id`.

No code is needed to obtain this; it is memory's existing behavior.

### D5 — Revocation is immediate

Memory reads the current `AgentDefinition` each run; removing `delegation` (dropping the tools + spawnPolicy) takes effect on the agent's next action. No rebuild/restart (consistent with the "delete = stop" and definition-driven model in 03-agent-model.md).

Persisting the removal was verified against live memory and depends on two implementation details: the gateway must **PATCH** (not PUT) the definition — memory's route is `PATCH /api/projects/:id/agent-definitions/:id` and PUT returns 404 — and it must send `tools`/`config` explicitly even when empty, because memory's update is partial (an absent/null field means "unchanged"). The gateway therefore drops `omitempty` on those two fields and, on disable, normalizes them to empty `[]`/`{}` so the strip actually reaches memory.

## Risks / Trade-offs

- [Recursive delegation blows up] → memory enforces `DefaultMaxDepth=6` and strips coordination tools at max depth (toolpool.go:474); single-owner project keeps the blast radius small.
- [Allowlist misconfiguration] → requiring non-empty `targets` when enabled avoids memory's "open policy" footgun (empty allowlist = delegate to any project agent); the UI must reject enabled-with-empty-targets.
- [Sub-agent sees the whole project memory] → by design (D4) a delegated agent reads/writes the same graph as the supervisor; acceptable within the single-owner boundary (D6), and the allowlist limits *who* can be delegated to, not *what* they can touch.
- [Alfred "disabled" ≠ memory "disabled"] → `list_available_agents` returns all project definitions with no enabled filter (memory has no per-agent enabled flag); an Alfred agent with `enabled=false` (no voice worker) still has a definition and could be spawned. The allowlist is the real gate; the "enabled" flag is voice-dispatch-only and deliberately orthogonal.
- [No cancel tool for a stuck sub-agent] → memory exposes cancel only via repo/HTTP/ACP, not to agents; v1 accepts this (sync spawns are bounded by the child's `MaxSteps`/`DefaultTimeout`).

## Migration Plan

Additive: new optional `delegation` field with a safe default (`enabled=false`). No data migration — existing agents simply omit the field and behave exactly as today. Rollback is a config revert (set `enabled=false`), no schema change. The gateway's `AgentDefinition` needs a `Config` field added to carry `spawnPolicy.allow` (it currently omits it).

## Open Questions

None that would change the specs, approach, or task breakdown.
