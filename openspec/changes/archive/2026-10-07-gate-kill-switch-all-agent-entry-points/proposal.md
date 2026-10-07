## Why

The `AGENT_EXECUTION_ENABLED` emergency kill switch is enforced in only one of the
three agent execution entry points. `AgentExecutor.Execute` checks it, but
`ExecuteWithRun` (HTTP agent triggers, async A2A, worker-pool jobs) and `Resume`
(MCP respond / session-UI wake) bypass it. The result is inconsistent enforcement:
a nested `search-knowledge` call, which routes through `Execute` via chat `/query`,
is refused while HTTP triggers, A2A, and the worker pool keep executing. During the
`fix-agent-queue-explosion` work the switch was added to `Execute` only, and the two
verification tasks for it (9.4 test, 20.10 verify) were never completed — that gap is
this issue (#1646).

Separately, the internal same-server HTTP calls made by the `mcp` domain
(`query_knowledge`, `remember`, `forget`) hardcode `http://localhost:<port>`. That
address is wrong in containerised / non-loopback deployments, where the server cannot
reach itself on `localhost`.

## What Changes

- Gate **all three** execution entry points (`Execute`, `ExecuteWithRun`, `Resume`)
  through one shared `ensureExecutionEnabled` guard.
- Add a stable sentinel, `ErrExecutionDisabled`, returned by every blocked path while
  preserving the existing message string `"agent execution is disabled system-wide"`.
- **No** `TrustedInternal` exemption: trust is a reachability marker, not an
  executability one, and both blocked and bypassing paths carry `TrustedInternal: true`.
- Surface the kill-switch condition cleanly from `mcp` `query_knowledge` — no
  `agent error: Agent execution failed: …` double-wrap — sourcing the message from a
  single shared definition.
- Add a configurable `INTERNAL_BASE_URL` (with `http://localhost:<SERVER_PORT>`
  fallback, trailing slashes normalised) for the `mcp` internal same-server HTTP calls.
- **Close the caller-side gaps** (adversarial review R2): callers that pre-create a run
  (HTTP trigger, A2A send) refuse before creating it instead of returning a false
  `success: true`; any pre-created resume run is marked failed so it does not sit
  `running`; and question-consuming responders (MCP respond tool, A2A resume) check the
  switch **before** claiming the question so a disabled system cannot strand a run
  paused with its question already consumed.
- Tests covering all three gated entry points, the disabled-path non-mutation property,
  the pre-created-run terminal transition, question non-consumption, the clean error
  surfacing, and the base URL override + trailing-slash + fallback.

## Capabilities

### New Capabilities

- `agent-execution`: the emergency kill switch gates every execution entry point
  uniformly; callers must not strand a pre-created run or consume a pending question
  while disabled; and internal same-server HTTP calls use a configurable base URL.
  (Declared as a **new** capability because no `agent-execution` spec exists in
  `openspec/specs/` yet — the source change `fix-agent-queue-explosion` is
  unarchived — so the delta uses `## ADDED Requirements`.)

## Impact

### Code

- `apps/server/domain/agents/executor.go` — sentinel, shared guard, gate the three
  entry points.
- `apps/server/domain/agents/executor_cleanup_test.go` — disabled-path tests.
- `apps/server/domain/mcp/query_tools.go` — clean kill-switch error + `baseURL()`.
- `apps/server/domain/mcp/service.go` — `internalBaseURL` field + `baseURL()` helper.
- `apps/server/internal/config/config.go` — `INTERNAL_BASE_URL`.
- `.env.example` — document `INTERNAL_BASE_URL`.

### Verification

- Server build + vet + targeted unit tests; `golangci-lint` on the touched packages;
  `openspec validate --strict`.

### Operational follow-through

The dev/infra environment must set `AGENT_EXECUTION_ENABLED=true`; the flag being
`false` there is what exposed the inconsistency (some paths blocked, others not).
