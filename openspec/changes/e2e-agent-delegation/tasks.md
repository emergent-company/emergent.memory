## 0. Recon

- [x] 0.1 Confirm no existing e2e coverage of delegation: no `spawn_agents` /
      sub-agent / delegation match anywhere under `apps/web-ui/tests/` or
      `e2e/tests-api/`; gateway coverage is unit-only
      (`apps/web-ui/gateway/delegation_test.go`).
- [x] 0.2 Confirm the delegation UI contract: Settings form fields
      `delegationEnabled` + repeated `delegation-target`
      (`apps/web-ui/gateway/agent.go`), templ ids
      `#agent-settings-delegation-enabled` and the target picker
      (`apps/web-ui/gateway/agent.templ`), and `applyDelegation` injecting
      `spawn_agents` + `list_available_agents` plus `config.spawnPolicy.allow`.
- [x] 0.3 Confirm the verification surface: memory exposes
      `GET /api/projects/:projectId/agent-runs` and
      `…/agent-runs/:runId/full`; the gateway has no agent-runs list proxy, so
      the spec reads memory directly with `memoryAuthHeaders(page)`.
- [x] 0.4 Confirm the spec belongs to the existing `scenarios` Playwright
      project (`scenarios/.*\.spec\.ts`, `playwright.config.ts`) — no config
      change and no test registry to update.

## 1. Test implementation

- [x] 1.1 `apps/web-ui/tests/e2e/scenarios/agent-delegation.spec.ts` — single
      spec, header comment documenting the journey and skip conditions, modelled
      on `scenarios/mcp-servers-tool-call.spec.ts`.
- [x] 1.2 Env gating and skip semantics: skip when `E2E_SCENARIO_LLM_API_KEY` is
      unset (reused env vars, no new keys); annotate-skip when the provider save
      is rejected or a run-level `error` SSE event precedes any tool call; a
      completed turn with no spawn call also skips with the transcript excerpt
      quoted, since model tool-use is non-deterministic.
- [x] 1.3 Seed: unique project + activate, target agent B (`tools: []`),
      delegator agent A (`tools: []`), both with an explicit model; capture ids.
- [x] 1.4 Configure delegation on A through `/agents/:id/settings`: enable the
      toggle, tick B in the target picker, submit via the real `Save changes`
      control (PRG → `?updated=1`).
- [x] 1.5 Persistence assertion via `GET /api/agents/:id`: `tools` contains
      `spawn_agents` and `list_available_agents`; `config.spawnPolicy.allow`
      equals `["<B name>"]`. Failure messages state which half is missing.
- [x] 1.6 Live chat turn: `/chat?agent=A`, select A, send the forced
      `spawn_agents` prompt, capture the POST `/api/chat` SSE body.
- [x] 1.7 Tool-invocation assertion on the SSE body: `"type":"mcp_tool"` plus
      `"tool":"spawn_agents"` (memory emits native ADK tool calls through the
      same `mcp_tool` event as pooled MCP tools — `domain/chat/handler.go` maps
      `StreamEventToolCallStart/End` to `NewMCPToolEvent`).
- [x] 1.8 Child-run assertion: a run exists whose `parentRunId` equals A's run id
      (from the `done` event) and whose `rootRunId` is the same, its
      `agentDefinitionId` is B's definition, status is polled to terminal
      `completed` (bounded, explicit timeout message), and B's `/full` bundle
      transcript contains the delegated sentinel result. Correlation uses the run
      id from the `done` event, not the definition id, because the run list
      filters on the runtime agent id.
- [x] 1.9 `finally` cleanup: delete both agents, reactivate the bootstrap
      project, delete the scratch project (`.catch(() => {})` on each).

## 2. Verification

- [x] 2.1 Typecheck passes for the new spec: `npx -p typescript@5.6.3 tsc
      --noEmit` reports 0 errors in this file (the repo pins no local
      typescript; the only 2 reported errors are pre-existing and in
      `specs/shell/css-go-daisy-scan.spec.ts`).
- [x] 2.2 `npx playwright test --list scenarios/agent-delegation.spec.ts` lists
      the spec in the `scenarios` project without executing it.
- [x] 2.3 `openspec validate e2e-agent-delegation --strict` passes.
- [ ] 2.4 Run the spec against a live environment with credentials and record
      the outcome. Not runnable from this checkout: `tests/e2e/.env.e2e` is
      absent and no `E2E_SCENARIO_LLM_*` variables are set, so the spec reports
      its documented skip. The child-run assertions (1.8) are the ones that
      would catch a broken parent/child linkage on a credentialed run.
- [x] 2.5 Confirm no product code, gateway, or Playwright config file changed:
      `git status` in the worktree shows only the new spec and this change
      directory.

## 3. Ship

- [ ] 3.1 Commit the change directory and the spec together on the feature
      branch and open one PR against `main` (spec + implementation are one unit).
- [ ] 3.2 After merge, `openspec archive e2e-agent-delegation` and sync the delta
      spec into `openspec/specs/e2e-agent-delegation/`.
