## 1. Shared kill-switch guard

- [x] 1.1 Add `ErrExecutionDisabled` sentinel in `apps/server/domain/agents/executor.go`
- [x] 1.2 Add `ensureExecutionEnabled(req)` helper (logs block, returns sentinel)
- [x] 1.3 Route `Execute` through the helper (replace inline check)
- [x] 1.4 Route `ExecuteWithRun` through the helper, after `dbCtx` and before the trust-marker write
- [x] 1.5 Route `Resume` through the helper, after `dbCtx` and before any mutation

## 2. Tests — gated entry points

- [x] 2.1 `TestExecute_ExecutionDisabled` asserts `errors.Is(err, ErrExecutionDisabled)`
- [x] 2.2 `TestExecuteWithRun_ExecutionDisabled` asserts `errors.Is(err, ErrExecutionDisabled)`
- [x] 2.3 `TestResume_ExecutionDisabled` asserts `errors.Is(err, ErrExecutionDisabled)`

## 3. Clean error surfacing (mcp query_knowledge)

- [x] 3.1 Detect the kill-switch message in the SSE `error` branch and avoid the `agent error:` double-wrap
- [x] 3.2 `TestExecuteQueryKnowledge_KillSwitchErrorNotDoubleWrapped` asserts the exact message

## 4. Configurable internal base URL

- [x] 4.1 Add `INTERNAL_BASE_URL` to `apps/server/internal/config/config.go`
- [x] 4.2 Add `internalBaseURL` field + `baseURL()` helper to `mcp.Service` (keep `serverPort`)
- [x] 4.3 Replace the query/remember/forget hardcoded `localhost` URLs with `s.baseURL()`
- [x] 4.4 Document `INTERNAL_BASE_URL` in `.env.example`
- [x] 4.5 `TestServiceBaseURL_PrefersConfiguredAndFallsBack` covers override + fallback
- [x] 4.6 `TestExecuteQueryKnowledge_UsesInternalBaseURLWhenSet` covers the dialled URL

## 5. Verification

- [x] 5.1 `go build ./...` from `apps/server`
- [x] 5.2 `go vet ./...` from `apps/server`
- [x] 5.3 `go test ./domain/agents/... ./domain/mcp/... ./internal/config/...`
- [x] 5.4 `golangci-lint run ./domain/agents/... ./domain/mcp/... ./internal/config/...`
- [x] 5.5 `openspec validate gate-kill-switch-all-agent-entry-points --strict`
- [x] 5.6 Reference stale `fix-agent-queue-explosion` tasks 9.4 / 20.10 (covered by this change; left for that change's own archive decision)

## 6. Delivery

- [x] 6.1 Commit, push branch `fix/gate-kill-switch-all-agent-entry-points`
- [x] 6.2 Open one PR (spec + impl + tests) linking #1646; do not merge

## 7. Adversarial review (R2) follow-ups

- [x] 7.1 Extract the canonical sentinel/message to neutral `pkg/agentexec` (breaks the agents↔mcp import cycle); alias it from `agents.ErrExecutionDisabled`
- [x] 7.2 Add exported `AgentExecutor.EnsureExecutionEnabled()` for caller-side pre-checks
- [x] 7.3 Add `failRunIfExecutionDisabled` helper and mark pre-created resume runs terminal on disabled
- [x] 7.4 Refuse HTTP trigger before creating the run (explicit error, no false `success:true`)
- [x] 7.5 Refuse A2A send before creating the run
- [x] 7.6 MCP respond tool checks executability before `AnswerQuestion` (question stays pending)
- [x] 7.7 A2A resume checks executability before claiming the question
- [x] 7.8 mcp `query_knowledge` sources the kill-switch message from `pkg/agentexec`
- [x] 7.9 Normalise trailing slash in mcp `baseURL()`
- [x] 7.10 Rewrite delta spec to the enforceable guarantee (pre-created run terminal; question not consumed) and move `agent-execution` to New Capabilities in the proposal
- [x] 7.11 Tests: disabled path does not mutate the run row; non-kill-switch SSE error keeps the generic wrapper; pre-created run ends terminal; question not consumed when disabled; trailing-slash normalisation
- [x] 7.12 Re-verify build/vet/tests/lint/openspec and push to the same branch (PR #1647)

## 8. Round 3 — A2A SSE surface (`a2a_stream.go`)

- [x] 8.1 `streamNewTask`: refuse before `CreateRunWithOptions` (SSE new-task)
- [x] 8.2 `streamResumeTask`: refuse before claiming the pending question (SSE resume)
- [x] 8.3 `streamRun`: fail the run terminal on a late `ErrExecutionDisabled` and emit `TASK_STATE_FAILED`
- [x] 8.4 `a2a_message.go` sync branch: fail the run terminal on a late `ErrExecutionDisabled`
- [x] 8.5 Delta spec: A2A requirements/scenarios cover BOTH `/message:send` (JSON) and `/message:stream` (SSE)
- [x] 8.6 Tests: SSE new-task no run insert; SSE resume question not consumed; streamRun terminal + SSE failure (new + resume); TriggerAgent / RespondToQuestion 503 contract
- [x] 8.7 Confirm `pkg/agentexec` is the single source of the message; trailing-slash + generic-wrapper tests present
- [x] 8.8 Re-verify build/vet/tests/lint/openspec and push to PR #1647
