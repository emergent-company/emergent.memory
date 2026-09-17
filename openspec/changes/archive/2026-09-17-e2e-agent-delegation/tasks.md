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
- [x] 1.7 Tool-invocation assertion on the SSE body: the started `spawn_agents`
      event must name the target, and the `"type":"mcp_tool"` event type is
      asserted for the spawn (memory emits native ADK tool calls through the
      same `mcp_tool` event as pooled MCP tools).
- [x] 1.8 Child-run assertion: the child run id comes from the completed
      `spawn_agents` tool result (`result.results[0].run_id`); the run is fetched
      by id and must belong to B's definition, be `completed`, carry a non-empty
      `parentRunId`, and the parent run fetched by that id must exist and be the
      run the child points at. `rootRunId` is cross-checked only when both runs
      carry one (see F2). The child's `/full` transcript must contain the
      delegated sentinel result.
- [x] 1.9 `finally` cleanup: delete all three agents (target, delegator, and
      reject), reactivate the bootstrap project, delete the scratch project
      (`.catch(() => {})` on each).
- [x] 1.10 No-target rejection scenario: a third agent C (`tools: []`) enables
      the toggle with no target selected, the save redirects to `?err=<message>`,
      the decoded error names the missing target, and `GET /api/agents/:C`
      confirms neither delegation tool nor a `spawnPolicy` was written.
- [x] 1.11 Disable-removes scenario: unchecking the toggle on A strips both
      delegation tools and the `spawnPolicy`; re-enabling restores them and the
      persisted `tools`/`spawnPolicy.allow` are re-asserted so the live turn
      still runs delegated.
- [x] 1.12 Child-run correlation from the tool result + parent-run guard: the
      child run id is read from the completed `spawn_agents` result
      (`result.results[0].run_id`); a completed turn that never invoked the tool
      first asserts the delegator run's resolved `tools` contains `spawn_agents`
      (fail when unoffered) before skipping as "model declined"; PINEAPPLE is
      asserted in a non-user, non-tool message of the `/full` bundle.

## 2. Verification

- [x] 2.1 Typecheck passes for the new spec: 0 errors in this file (the repo
      pins no local typescript; the only errors reported are pre-existing and in
      `specs/shell/css-go-daisy-scan.spec.ts`).
- [x] 2.2 `npx playwright test --list scenarios/agent-delegation.spec.ts` lists
      the spec in the `scenarios` project without executing it.
- [x] 2.3 `openspec validate e2e-agent-delegation --strict` passes.
- [x] 2.4 Live run against dev with real credentials: **2 passed in 37.0s**
      (setup + scenario; scenario 22.3s). Command:
      `npx playwright test scenarios/agent-delegation.spec.ts --project=scenarios`
      against the gateway at `alfred-dev.tail0358fa.ts.net:8095` and memory at
      `https://api.dev.emergent-company.ai`, with credentials from the gitignored
      `tests/e2e/.env.e2e`. The child run id was read from the completed
      `spawn_agents` tool result, the child run verified against B's definition
      and its `parentRunId` linkage, and B's output asserted in a non-user,
      non-tool message.
- [x] 2.5 Confirm no product code, gateway, or Playwright config file changed:
      `git status` in the worktree shows only the new spec and this change
      directory.

## 3. Ship

- [x] 3.1 Commit the change directory and the spec together on the feature
      branch and open one PR against `main` (spec + implementation are one unit):
      PR #545.
- [ ] 3.2 After merge, `openspec archive e2e-agent-delegation` and sync the delta
      spec into `openspec/specs/e2e-agent-delegation/`.

## 4. Findings and follow-ups

- [x] 4.1 **F1 (recorded in `design.md`)** — the chat `done` event carries no run
      id: `DoneEvent.RunID` is `json:"runId,omitempty"` and the chat path writes
      it empty, so the field is omitted. The spec anchors on the child run id
      from the completed tool result instead.
- [x] 4.2 **F3 (recorded in `design.md`)** — a dev `memory-server` restart
      produced a 502 during `setup`; not a delegation regression.
- [ ] 4.3 **F2 follow-up (out of scope for this test-only change)** — `root_run_id`
      is not persisted when the run's OTel span context is invalid (tracing off).
      Read-only dev evidence:
      `SELECT count(*) FILTER (WHERE root_run_id IS NULL), count(*) FILTER (WHERE root_run_id IS NOT NULL) FROM kb.agent_runs;`
      → 204 null, 0 set, 204 total — every run, parents included. The
      parent/child link survives via `parentRunId`, but the `agent-run-preview`
      change groups the delegation tree by root run, so runs without a root
      cannot be attached to that tree. Needs its own change on the
      agents/executor path; not fixed here.
