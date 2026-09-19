## Context

The in-house ACP implementation (`domain/agents/acp_handler.go`, `acp_dto.go`, `acp_routes.go`) targets IBM/BeeAI Agent Communication Protocol v0.2.0 — archived 2025-08-27, merged into A2A on 2025-08-25 — and diverges from even that dead spec. The platform needs to expose agents to external clients over a live standard.

Key existing infrastructure this design builds on:

- `AgentDefinition.Visibility` (`external`/`project`/`internal`) already gates discoverability, and `AgentDefinition.ACPConfig` (JSONB) already carries display description, capabilities, input/output modes.
- A spawnable runtime `Agent` and an `AgentRun` engine with `AgentExecutor.ExecuteWithRun()` / `Resume()` and a `StreamCallback` for real-time deltas.
- `AgentQuestion` implements human-in-the-loop pause/resume, surfaced today as `await_request` + `POST …/resume`.
- Persistence already exists: `kb.acp_sessions` (thin run grouping) and `kb.acp_run_events` (persisted event log), with `kb.agent_runs.acp_session_id` linking a run to a session.
- Auth: `Bearer emt_*` project API token, scopes `agents:read` / `agents:write`, resolved to a project via `auth.GetUser(c).ProjectID`.
- Precedent for a stateless protocol facade over the run engine: `domain/agentcompat` serves an OpenAI-style `/v1/chat/completions` surface outside `/api/`.

Target protocol: **A2A Protocol v1.0** (`a2a.proto`, package `lf.a2a.v1`; JSON is camelCase; enums serialize as `SCREAMING_SNAKE_CASE`; timestamps ISO-8601 UTC).

## Goals / Non-Goals

**Goals:**

- Expose A2A v1.0 discovery (global AgentCard + authenticated extended card) and the core HTTP+JSON message flow.
- Map the existing run engine onto A2A `Task` semantics through a stateless facade — no internal model refactor.
- Support streaming (SSE) and human-in-the-loop (`TASK_STATE_INPUT_REQUIRED` + `taskId` resume).
- Negotiate protocol version and emit spec-shaped errors.
- Deprecate, migrate off, and later delete the ACP implementation.
- Add conformance smoke tests and wire the official TCK as a non-blocking gate.

**Non-Goals:**

- Push notifications / webhooks (`pushNotificationConfigs`) — deferred; lowest value, highest infra cost.
- gRPC and JSON-RPC bindings — deferred until a concrete interop partner requires them.
- `/{tenant}/` path routing and per-tenant public cards — rejected as non-standard for the global well-known path.
- AgentCard `signatures` (JWS) — deferred.
- Adopting `github.com/a2aproject/a2a-go/v2` — deferred (see Decision 2).
- Refactoring `AgentRun` / `AgentRunStatus` to A2A-native vocabulary.

## Decisions

### 1. Discovery is two-tier: static global card + authenticated extended card

`GET /.well-known/agent-card.json` is IANA-registered at the origin root and **unauthenticated**, so it can never be per-tenant. Keep it tenant-blind and config-driven:

- Contains platform identity only: `name`, `description`, `version`, `capabilities` (`streaming: true`, `extendedAgentCard: true`), `defaultInputModes`, `defaultOutputModes`, `supportedInterfaces[]` (one entry, `protocolBinding: "HTTP+JSON"`, `protocolVersion: "1.0"`, no `tenant`), and a single generic `skills[]` entry or an empty array.
- It MUST NOT read `agent_definitions`, projects, orgs, or any tenant row. This is a security invariant, enforced by a unit test asserting the rendered card contains no project/agent identifiers.

Per-project enumeration happens at `GET /extendedAgentCard` (authenticated; requires `capabilities.extendedAgentCard: true`):

- Bearer `emt_*` → project resolved from the token exactly as `acpProjectID` does today.
- `skills[]` = one `AgentSkill` per `AgentDefinition` with `visibility = 'external'`; `skill.id` = the existing RFC 1123 slug (`pkg/acpslug`), populated from `ACPConfig` with `name`/`description` fallbacks.
- `securityRequirements` declares the bearer scheme (Decision 7).

**Why not per-`{tenant}` cards:** clients are already credential-scoped to exactly one project, so `tenant` routing adds nothing and risks advertising project UUIDs publicly. Omit `tenant` from `supportedInterfaces[]`.

*Alternative considered:* single platform card with one skill per agent across all tenants — rejected, would leak cross-tenant agent inventories publicly.

### 2. Hand-roll the HTTP+JSON binding; do not adopt `a2a-go/v2` yet

Write camelCase A2A DTOs and Echo handlers directly. Rationale:

- The SDK's `a2asrv` ships its own generic bearer/OAuth middleware and has no native "token-bound project → extended card vs global card" model; our auth and tenancy would be force-fit around it.
- We must map internal messages/events to A2A types regardless, so the SDK buys wire types only.
- We write new DTOs either way (our ACP DTOs are snake_case; A2A is camelCase).
- The official TCK is currently 0.3-wire-focused, so SDK adoption would not buy conformance certainty today.

Revisit the SDK only when a JSON-RPC or gRPC binding is demanded; then adopt it for that binding only.

*Alternative considered:* adopt `a2a-go/v2` for everything — rejected as YAGNI plus supply-chain/auth friction.

### 3. Stateless facade over the existing run engine

Do not make A2A the internal vocabulary; that is the exact trap that produced the current divergent protocol. The engine keeps its own status strings; the facade translates.

| A2A | Internal |
|---|---|
| `Task.id` | `AgentRun.ID` (stable across a resume chain — present the original task id, not `resume_run_id`) |
| `Task.contextId` | `ACPSession.ID` (created lazily when the caller omits `contextId`) |
| `Task.status` | mapped `AgentRun.Status` (table below) |
| `Task.artifacts[]` | final assistant text, as one artifact with a text part |
| `Task.history[]` | reconstructed `Message[]` (ROLE_USER / ROLE_AGENT) from run messages |
| `Message.taskId` present | resume of an existing run |

**Status map** (internal → A2A `TaskState`):

| Internal | A2A | Note |
|---|---|---|
| `submitted` | `TASK_STATE_SUBMITTED` | queued, not yet claimed |
| `working` | `TASK_STATE_WORKING` | running |
| `completed` | `TASK_STATE_COMPLETED` | terminal |
| `failed` | `TASK_STATE_FAILED` | `status.message` carries the error |
| `input-required` | `TASK_STATE_INPUT_REQUIRED` | paused on a question or tool approval |
| `cancelled` | `TASK_STATE_CANCELED` | single-L in A2A |
| `cancelling` | `TASK_STATE_WORKING` + message "cancellation requested" | transient; never emit `cancelling` on the wire |
| `skipped` | `TASK_STATE_COMPLETED` + `status.message` = skip reason | A2A has no SKIPPED |

Never emitted: `TASK_STATE_UNSPECIFIED`, `TASK_STATE_REJECTED` (reserved for future policy rejection), `TASK_STATE_AUTH_REQUIRED` (reserved for send-time auth failure).

**Event map** (internal stream source → A2A SSE `StreamResponse` member):

| Internal | A2A member | Detail |
|---|---|---|
| run created | `task` | `TASK_STATE_SUBMITTED` |
| working transition | `statusUpdate` | `TASK_STATE_WORKING` |
| text delta | `artifactUpdate` (streaming) → final `message` | growing text artifact with stable `artifactId`; emit immutable ROLE_AGENT message at turn end |
| thinking delta | fold into final `message` metadata | A2A has no thinking concept |
| tool call start/end | `artifactUpdate` with a `data` part | A2A has no tool event; render as JSON data |
| tool approval gate | `statusUpdate` (`INPUT_REQUIRED`) | second pause source beside `ask_user` |
| error | `statusUpdate` (`FAILED`) | |
| completed | `task` + final `message` + `artifactUpdate` | |
| awaiting | `statusUpdate` (`INPUT_REQUIRED`) | |
| cancelled | `statusUpdate`/`task` (`CANCELED`) | |
| `session.ready` | — | dropped; `contextId` conveys continuity |

### 4. HITL: `INPUT_REQUIRED` + resume via `message.send` with `taskId`

- `message:send` with **no** `taskId` → new task: create a run, link to `contextId`, execute.
- `message:send` with **`taskId`** → resume: require the task be `INPUT_REQUIRED` (else 409/400), atomically claim the question, call `executor.Resume`. The same `Task.id` is returned; internally a new run is chained, but the wire never exposes `resume_run_id`.
- A2A has no `/resume` route; `POST …/runs/:runId/resume` disappears.

**Streaming across the interruption.** The spec says `INPUT_REQUIRED` does not close the stream. Two options:

- **M1 (shipped first, documented deviation):** the `message:stream` response closes at `INPUT_REQUIRED`; the client resumes with a new `message:stream`/`message:send` carrying `taskId`.
- **M2 (later):** hold the stream open and bridge the resume signal through the existing `events.Service` SSE bus (already fanned out per project+run).

Rationale: the engine executes synchronously on the request goroutine, so holding a connection across an asynchronous human turn is the only genuinely new concurrency primitive required. Ship the deviation first, document it, and build the bridge when justified.

### 5. Reuse the ACP persistence; no new migration for milestone 1

`kb.acp_sessions` already models a `contextId` (an opaque grouping of runs) and `kb.acp_run_events` is a generic persisted event log keyed by run. Map onto them directly:

- `contextId` → `kb.acp_sessions.id`; `Task.id` → `kb.agent_runs.id`; history rebuild → `kb.acp_run_events` ordered by `created_at`.
- Rename the tables in a later migration once ACP is deleted; do not create parallel A2A tables.

*Alternative considered:* new `kb.a2a_*` tables — rejected as duplicate storage with no behavioural gain.

### 6. ACP disposition: deprecate, migrate, delete

- Phase D1: add `Deprecation` and `Sunset` response headers to `/acp/v1/` and `/agent-chat/v1/`. ACP routes keep working.
- Phase D2: migrate first-party consumers — CLI `memory acp` → `memory a2a`; SDK `pkg/sdk/acp` → `pkg/sdk/a2a`; MCP `acp-*` tools repointed to `a2a-*` or retired (they are thin wrappers over `repo`/`executor` that duplicate `agent-list-available` + `trigger_agent`). The web-ui does not reference `/agent-chat/v1` (verified — the only literal is `acp_routes.go`), so no gateway change is required.
- Phase D3 (later release): delete `acp_handler.go`, `acp_dto.go`, `acp_routes.go`, `pkg/sdk/acp`, `apps/cli/internal/cmd/acp.go`; optionally rename the two `kb.acp_*` tables.

### 7. Auth declaration

Declare a bearer scheme on the card rather than claiming OAuth2 compliance we do not have:

```json
{
  "securitySchemes": {
    "memoryApiToken": { "httpAuthSecurityScheme": { "scheme": "Bearer", "bearerFormat": "emt" } }
  },
  "securityRequirements": [{ "schemes": { "memoryApiToken": { "list": [] } } }]
}
```

Clients send `Authorization: Bearer emt_…`. Scope requirements (`agents:read` / `agents:write`) are documented; project addressing is credential-scoped, never path-scoped. The scheme is declared on the extended card (authenticated), not the public one.

### 8. Version negotiation and errors

- Honor `A2A-Version: Major.Minor` as a header or query parameter; support `1.0`; unsupported → `VersionNotSupportedError` (-32009 / HTTP 400).
- Errors use the `google.rpc.Status` JSON envelope with `details[].reason` in UPPER_SNAKE and `domain: "a2a-protocol.org"`, mapped to HTTP: -32001→404, -32002→400, -32003→400, -32004→400, -32005→400, -32006→500, -32009→400.

### 9. Transport surface (HTTP+JSON)

| Function | Route |
|---|---|
| Send message | `POST /message:send` |
| Streaming message | `POST /message:stream` (SSE) |
| Get task | `GET /tasks/{id}?historyLength=` |
| List tasks | `GET /tasks?contextId=&status=&pageSize=&pageToken=` |
| Cancel task | `POST /tasks/{id}:cancel` |
| Subscribe to task | `POST /tasks/{id}:subscribe` |
| Extended card | `GET /extendedAgentCard` |
| Global card | `GET /.well-known/agent-card.json` |

Content type `application/a2a+json` (accept `application/json`); streaming `text/event-stream` with `data: {StreamResponse}` lines.

*Open spec ambiguity:* the spec text says `POST /tasks/{id}:subscribe` while the proto annotation says `GET`. Implement `POST` (per §5.3/§11.3.2) and note the discrepancy.

### 10. Skill routing via message metadata

A2A's `SendMessageRequest` carries no skill selector, while a Memory project exposes many agent definitions. The first-party client (and any client) MAY select the target agent by setting `message.metadata["skillId"]` to an agent-definition slug.

- New-task path only: if `skillId` is present, resolve the definition by slug (external visibility first, then any visibility, mirroring the ACP resolver). Unknown slug → HTTP 400 `SKILL_NOT_FOUND`, with no fallback.
- If absent, the run uses the project CLI-assistant fallback (previous behaviour).
- Resume never consults `skillId` (the task already knows its agent).
- `skill_id` is tolerated as an alias; `skillId` wins.

This uses the A2A open-ended `metadata` map and requires no protocol extension. It is a first-party convention, documented for external clients.

### 11. MCP `acp-*` tool disposition

The `acp-list-agents`, `acp-trigger-run`, `acp-get-run-status`, and `acp-get-run-events` MCP tools are **deprecated in place** for this milestone: their descriptions are prefixed `[Deprecated: …]` pointing at `agent-list-available`, `trigger_agent`, and the A2A HTTP surface, but behaviour is unchanged. Removal is deferred to the ACP-deletion milestone alongside the ACP HTTP handlers, because removal is a breaking change for agent prompts that still reference the old tool names.

## Risks / Trade-offs

- **Multi-tenant well-known path (highest).** An unauthenticated global path cannot be tenant-aware. Mitigation: the global card is static/config-only with a unit test asserting no tenant identifiers leak; all per-project data stays behind `/extendedAgentCard`.
- **TCK 0.3-vs-1.0 gap.** Passing the official suite today does not prove v1.0 conformance. Mitigation: treat TCK as a smoke gate, add golden-file tests for v1.0 JSON shapes (camelCase, single-L `CANCELED`, `TASK_STATE_*` strings), and validate against the proto.
- **SDK maturity / supply chain.** Mitigated by hand-rolling (Decision 2); re-evaluate per binding later.
- **SSE across HITL.** Holding a stream open across an async human turn needs new coordination. Mitigation: ship the closing-stream deviation first (Decision 4).
- **Single-server streaming.** The current in-process channel design only streams from the server running the run. Acceptable while Memory is single-server; revisit with the `events.Service` bus.
- **Status/event fidelity.** Tool trajectories and thinking have no A2A equivalent; forcing them risks a half-conformant surface. Mitigation: relegate to `data` parts + metadata, keep core members strictly conformant, and unit-test that every emitted `TaskState` is a valid A2A value.
- **ACP deletion touches first-party consumers.** Mitigation: phased deprecation (Decision 6); no web-ui impact.

## Migration Plan

1. Land the A2A facade (discovery + message flow + streaming + HITL) alongside the live ACP routes; add deprecation headers to ACP.
2. Migrate CLI (`memory a2a`), SDK (`pkg/sdk/a2a`), and MCP tools.
3. After a release window, delete the ACP implementation and optionally rename the `kb.acp_*` tables.
4. Rollback: A2A routes are additive; removing them restores the pre-change surface. No data migration is involved in milestone 1.

## Open Questions

1. Should `GET /tasks` be project-scoped only, or also filterable by `skill`/agent id? (A2A has no skill filter; propose project-scoped with `contextId` + `status` filters.)
2. Retention/TTL for the reused `kb.acp_run_events` log once it backs A2A history.
3. ~~Whether to migrate the `acp-*` MCP tools to `a2a-*` or retire them.~~ **Resolved (Decision 11):** deprecate in place now, remove at the ACP-deletion milestone.
