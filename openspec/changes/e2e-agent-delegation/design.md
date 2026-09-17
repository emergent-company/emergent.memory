# Design — e2e-agent-delegation

## Context

Delegation spans four layers that unit tests can only pin down individually:

1. the gateway Settings form (`uiAgentSettings` / `uiAgentUpdate`) reads
   `delegationEnabled` + repeated `delegation-target` values and calls
   `applyDelegation`, which appends `spawn_agents`, `list_available_agents` to
   `AgentDefinition.Tools` and writes `Config["spawnPolicy"]["allow"]`;
2. memory stores that definition and `extractSpawnPolicy` reads it back;
3. the executor builds coordination tools for a top-level agent, exposes
   `spawn_agents`, and enforces the allow-list;
4. the spawned agent executes as a child run carrying `parentRunId` / `rootRunId`.

Only a live turn can prove all four line up. The existing
`apps/web-ui/tests/e2e/scenarios/mcp-servers-tool-call.spec.ts` already proves
the analogous loop for an external MCP tool (create agent with a whitelist →
live chat turn → assert the tool really ran), and is the template for this spec.

There is no gateway proxy for the agent-run listing endpoints: the gateway only
proxies `GET …/agent-runs/:runId/full` (via `GetRunFull`) and a run-history
route. Reading "B's runs and their parent" therefore goes straight to memory
with `helpers/objects.memoryAuthHeaders(page)`.

## Decisions

### D1 — Live-LLM scenario spec, env-gated

The spawn is model-mediated: it only happens if the model chooses
`spawn_agents`, so this cannot be a deterministic DOM spec. It lives in
`scenarios/` (already matched by the `scenarios` Playwright project) and copies
the MCP scenario's gating: `test.skip` when the scenario LLM credential is
unset, and turn-level provider/model errors (`extractSseError`-style detection)
become an annotated skip, not a failure. A flaky provider must not fail CI for a
reason unrelated to delegation.

### D2 — Configure delegation through the UI, assert through the API

The point of the test is the real loop, so the toggle is driven on
`/agents/:id/settings` (`#agent-settings-delegation-enabled` plus the target
checkbox `input[name="delegation-target"][value="<B name>"]`) and the form is
submitted normally. Post-condition is then asserted through
`GET /api/agents/:id`: `tools` must contain both delegation tools and the stored
definition must carry `config.spawnPolicy.allow == ["<B name>"]`. This separates
"the analyst could do it" from "the backend recorded it", and gives a
diagnosable failure when only one half regresses.

The agents themselves are created through `POST /api/agents` with an explicit
model (as the MCP scenario does), because the modal helper's tool field cannot
express the delegation-managed tool set and the source agent must start without
them so the toggle is what adds them.

### D3 — The strongest assertion is the child run, not the tool chip

A rendered tool chip only proves the model emitted a call. The spec therefore
also reads memory's run list for the target agent
(`GET /api/projects/:projectId/agent-runs?agentId=…`), asserts a run exists
whose `parentRunId` matches the source agent's run and whose `rootRunId` is
shared with it, polls that run to a terminal `completed` status, and reads the
child's `full` bundle (`/agent-runs/:childId/full`) to confirm the transcript
carries the delegated task's result. Each assertion names what it proves so a
broken link (no child run, wrong parent, never terminal) is immediately
distinguishable.

### D4 — Isolated project, always cleaned up

The spec seeds a fresh, uniquely named project and both agents in it (mirroring
the MCP scenario), so it never races other specs on the shared bootstrap tenant.
`finally` deletes both agents and the project via `page.request`, each
`.catch(() => {})`, and reactivates the bootstrap project if the template does.

### D5 — Forced prompt, bounded waits

The chat turn uses an imperative prompt that requires the tool call before any
final answer, mirroring how the MCP scenario forces its tool use. All waiting is
`waitForResponse` / `expect.poll` / bounded poll loops with explicit timeouts and
explicit timeout messages; no `waitForTimeout` is used as a correctness
mechanism.

## Risks

- **Model non-compliance**: the model may answer instead of calling the tool. The
  forced prompt plus the same skip-on-error handling used by the MCP scenario
  keeps this from being recorded as a product regression.
- **Run visibility lag**: child runs may not be listed the instant the turn ends,
  which is why the child-run assertions poll with a bounded timeout and fail with
  the last observed status.
