## Context

`AgentExecutor` (`apps/server/domain/agents/executor.go`) has three ways a run can
enter execution:

- `Execute` — synchronous, from-scratch execution (also the path nested
  `search-knowledge` reaches through chat `/query`).
- `ExecuteWithRun` — a pre-created run is handed in; used by the HTTP trigger
  endpoint, async A2A, and the worker pool.
- `Resume` — a paused run is re-woken from an MCP response or the session UI.

The kill switch (`AgentSafeguardsConfig.ExecutionEnabled`, env
`AGENT_EXECUTION_ENABLED`) was only checked at the top of `Execute`. The other two
reach `runPipeline` without the check, producing the inconsistency in #1646.

## Goals / Non-Goals

**Goals**

- One guard, checked by every entry point, before any run-row mutation.
- A stable sentinel (`ErrExecutionDisabled`) so callers can programmatically detect
  a system-wide disable, without changing the operator-facing message.
- Consistent failure for trusted and external paths.

**Non-Goals**

- Do not add a `TrustedInternal` exemption. Trust distinguishes reachability, not
  executability; both the paths that were correctly blocked and those that were
  bypassing are `TrustedInternal: true`, so keying on it would not have fixed the bug.
- Do not change budget or queue safeguards.

## Decisions

### One shared guard

`ensureExecutionEnabled(req)` logs the block (same fields as before) and returns
`ErrExecutionDisabled`. `Execute` calls it where the inline check used to be;
`ExecuteWithRun` and `Resume` call it immediately after `dbCtx :=
context.Background()` and **before** their first run-row write
(`UpdateRunTrustedInternal`), so a disabled executor never mutates rows.

### Placement in Resume

The guard runs before the `priorRun.Status != RunStatusPaused` validation. A
system-wide disable is the more fundamental refusal, and this keeps the guard in the
same position (right after `dbCtx`) in all three entry points.

### Clean MCP error surfacing

When the SSE stream carries `{"type":"error","error":"agent execution is disabled
system-wide"}`, `query_knowledge` returns
`query_knowledge: agent execution is disabled system-wide` rather than the generic
`query_knowledge: agent error: …` wrapper. Detection is a substring match against
the canonical message. The message is defined once in the neutral leaf package
`pkg/agentexec` (`agentexec.ErrExecutionDisabled`), which both `domain/agents` and
`domain/mcp` import. `domain/mcp` cannot import `domain/agents` directly:
`domain/agents/module.go` already imports `domain/mcp`, so the reverse import would
be a cycle. `agents.ErrExecutionDisabled` aliases the `agentexec` value, so
`errors.Is` identity is preserved everywhere.

### Configurable internal base URL

`mcp.Service` gains `internalBaseURL` (from `config.Config.InternalBaseURL`, env
`INTERNAL_BASE_URL`). `baseURL()` returns it (trailing slashes trimmed) when
non-empty, otherwise `http://localhost:<serverPort>`. The existing `serverPort`
field is retained so existing tests and the loopback fallback keep working. The
three internal URLs (query/remember/forget) switch to `s.baseURL()`.

## Adversarial review (R2) follow-ups

The in-executor guard alone was insufficient because callers create run rows and
consume pending questions *before* calling the executor. Four caller-side gaps are
closed:

### Pre-created runs must not sit `running`

Callers create the run row first. On `ErrExecutionDisabled` the executor returns
before touching it, so the row stayed `running` until the 30-minute
`StaleRunReaper` produced a misleading `"run exceeded idle timeout (likely
abandoned by client)"`. Two measures:

- **Refuse before creating** where the run is created synchronously in the request
  path (HTTP trigger, A2A send). This is a response-contract change for the HTTP
  trigger: with the switch off it now returns an explicit error instead of
  `200 {"success": true}`.
- **Fail the pre-created run** where a row already exists (`resumeQuestionRun`
  pre-creates the resume run). The shared helper
  `failRunIfExecutionDisabled(ctx, repo, run, err, log)` marks it failed with the
  disabled reason, and is a no-op for nil runs / other errors.

### Questions must not be consumed while disabled

`AnswerQuestion` is an atomic claim; consuming it and then having `Resume` refuse
leaves the run `paused` with no pending question — permanently unresumable after
re-enable. The exported `AgentExecutor.EnsureExecutionEnabled()` is called **before**
the claim in the MCP respond tool and the A2A resume path, so the question stays
`pending`. The in-executor guard remains as defence in depth (e.g. a switch toggled
between the pre-check and the resume).

### Base URL trailing slash

`baseURL()` trims trailing slashes from `INTERNAL_BASE_URL` so
`https://host/` + `/api/...` cannot produce `//api/...`.

## Risks / Trade-offs

- The mcp error detection is string-based rather than `errors.Is` (the error crosses
  an SSE boundary, so the sentinel cannot be transported). Both sides source the
  message from `pkg/agentexec`, and both are covered by tests.
- Refusing the HTTP trigger before run creation changes its response contract when
  the switch is off (documented above). This is the intended behaviour: the previous
  `success: true` was false.
- `INTERNAL_BASE_URL` defaults to empty, preserving current loopback behaviour for
  single-host deployments.
