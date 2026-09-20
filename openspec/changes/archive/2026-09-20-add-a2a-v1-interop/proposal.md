## Why

The server exposes an in-house "ACP" interface (`/acp/v1/`, `/agent-chat/v1/`) that claims to implement IBM/BeeAI **Agent Communication Protocol v0.2.0**. That claim no longer holds in two independent ways:

1. **The spec is dead.** IBM/BeeAI ACP was merged into A2A on 2025-08-25 and its repository was archived on 2025-08-27. BeeAI — its only real consumer — migrated to A2A. There is no active peer set, no certification, and no conformance suite.
2. **We do not conform to it anyway.** Our routes (`POST /agents/:name/runs`, `DELETE .../runs/:runId`, `/resume`), run-status vocabulary (`submitted`/`working`/`input-required`), extra events (`tool_call`, `tool_result`, `session.ready`), and manifest fields are variously A2A v0.3 names or inventions — not ACP v0.2.0.

The result is a private protocol that external clients can only use through bespoke integration docs. The market standard for "external clients discover and invoke agents" is now **A2A Protocol v1.0** (Google-originated, donated to the Linux Foundation; TSC includes AWS, Cisco, Google, IBM Research, Microsoft, Salesforce, SAP, ServiceNow; native support in Google Cloud, AWS Bedrock AgentCore, and Azure AI Foundry; SDKs in six languages). Our existing manifest is already accidentally close to an A2A `AgentCard`, so the migration is mostly a wire-contract change, not an engine rewrite.

## What Changes

- Add an **A2A Protocol v1.0 HTTP+JSON interop surface** implemented as a stateless facade over the existing agent run engine:
  - **Discovery**: unauthenticated global `GET /.well-known/agent-card.json` (tenant-blind) plus authenticated `GET /extendedAgentCard` returning per-project `skills[]` derived from `visibility = 'external'` agent definitions.
  - **Message flow**: `POST /message:send`, `POST /message:stream` (SSE), `GET /tasks/{id}`, `GET /tasks`, `POST /tasks/{id}:cancel`, `POST /tasks/{id}:subscribe`.
  - **Version negotiation**: `A2A-Version` header/query parameter, defaulting to `1.0` for A2A requests.
  - **Errors**: A2A v1.0 `google.rpc.Status` JSON envelope with `details[].reason` / `domain: a2a-protocol.org` and the -32001…-32009 code mapping.
- Adopt A2A wire vocabulary (camelCase JSON, unified `Part` oneof, `TASK_STATE_*` states, `StreamResponse` discriminated union) **without** refactoring the internal `AgentRun` model. The facade owns the mapping; the engine stays protocol-neutral.
- Reuse existing persistence: `kb.acp_sessions` backs A2A `contextId`, `kb.acp_run_events` backs task event/history reconstruction. **No new migration is required for v1.0-http-json.**
- Hand-write the A2A DTOs and Echo handlers. Do **not** adopt `github.com/a2aproject/a2a-go/v2` for this milestone (our project-bound bearer auth and single-project tenancy would be force-fit around its auth/multi-tenancy model, while the wire types still need mapping). Revisit the SDK only when a JSON-RPC or gRPC binding is actually demanded.
- **Deprecate** `/acp/v1/` and `/agent-chat/v1/` with `Deprecation` / `Sunset` headers, migrate all first-party consumers to A2A, then delete the ACP implementation in a later release.
- Add a first-party `pkg/sdk/a2a` client and a `memory a2a` CLI command group; repoint or retire the `acp-*` MCP tools.
- Add A2A conformance shape-assertion tests (camelCase keys, `TaskState`/`Role` enum spelling, oneof serialization, no-tenant-leak). Wiring the official `a2a-tck` suite into CI and checking in golden fixtures are deferred until the TCK supports the 1.0 wire format, with the 0.3-vs-1.0 coverage gap documented.

## Capabilities

### New Capabilities

- `a2a-discovery`: Unauthenticated global AgentCard at `/.well-known/agent-card.json` and authenticated per-project `GET /extendedAgentCard`, including `supportedInterfaces[]`, `capabilities`, `skills[]`, and `securitySchemes`/`securityRequirements`. Enforces the hard invariant that public discovery leaks no tenant data.
- `a2a-message-flow`: `POST /message:send`, `POST /message:stream`, `GET /tasks/{id}`, `GET /tasks`, `POST /tasks/{id}:cancel`; the `Task`/`Message`/`Part`/`Artifact` data model; mapping to `AgentRun`; `contextId` ↔ session mapping; status and streaming-event mapping.
- `a2a-hitl`: Human-in-the-loop over A2A — `TASK_STATE_INPUT_REQUIRED` with a prompt in `status.message`, resume by sending a new `Message` carrying `taskId`, and the documented streaming behavior across the interruption.
- `a2a-conformance`: `A2A-Version` negotiation, `google.rpc.Status` error envelope, and conformance shape-assertion tests; the official TCK smoke gate is deferred.
- `a2a-acp-migration`: Deprecation signaling on `/acp/v1/` + `/agent-chat/v1/`, migration of CLI/SDK/MCP consumers, and the eventual removal of the ACP implementation.

### Modified Capabilities

(none)

## Impact

- **New files**: `apps/server/domain/agents/a2a_dto.go`, `a2a_handler.go`, `a2a_routes.go`, `a2a_mapping.go`; `apps/server/pkg/sdk/a2a/client.go`; `apps/cli/internal/cmd/a2a.go`.
- **Modified files**: `apps/server/domain/agents/module.go` (wire the handler + routes), `acp_routes.go` (deprecation headers), `mcp_tools.go` (repoint/retire `acp-*` tools), `apps/cli/internal/cmd/root.go` (register `a2a`, mark `acp` deprecated).
- **Auth**: reuses the existing `Bearer emt_*` project token and `agents:read` / `agents:write` scopes. The A2A AgentCard *declares* a bearer `securityScheme`; no new auth mechanism is introduced. Project resolution stays credential-scoped (token-bound), not path-scoped.
- **DB**: no new migration for the first milestone — `kb.acp_sessions` (→ `contextId`) and `kb.acp_run_events` (→ event/history log) are reused. A later optional migration may rename them once ACP is deleted.
- **No breaking changes**: existing `/api/…` routes, CLI commands, and MCP tools are untouched; ACP routes keep working behind deprecation headers during migration.
- **Not in scope (deferred)**: push notifications/webhooks, JSON-RPC and gRPC bindings, `/{tenant}/` path routing, per-tenant public cards, extended-card signatures (`signatures`).
