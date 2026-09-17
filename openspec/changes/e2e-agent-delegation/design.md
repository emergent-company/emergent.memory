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

A rendered tool chip only proves the model emitted a call. The correlation
anchor is the child run id that the completed `spawn_agents` tool result already
carries (`result.results[0].run_id`), taken straight from the captured SSE. The
spec asserts the started event named the correct target, then fetches that run by
id and asserts it belongs to the target's definition, has a non-empty
`parentRunId`, is `completed`, and that the parent run — fetched by that
`parentRunId` — exists and is the same run the child points at. Finally it reads
the child's `full` bundle (`/agent-runs/:childId/full`) and asserts the
transcript carries the delegated task's result. Each assertion names what it
proves, so a broken link (no child id in the tool result, wrong definition,
missing parent, never terminal) is immediately distinguishable.

`rootRunId` is deliberately only cross-checked, never required: see the finding
below.

## Live-run findings

These were established by running the spec against the dev environment, and they
shape the assertions above.

### F1 — The chat `done` event carries no run id

The first live run failed on the assumption that the `done` event could anchor
the parent run. The captured stream ends in a bare `data: {"type":"done"}`:
`DoneEvent.RunID` is `json:"runId,omitempty"` (`apps/server/pkg/sse/events.go`)
and the chat path writes the done event with an empty id
(`apps/server/domain/chat/handler.go`), so the field is omitted entirely. The
child run id from the completed `spawn_agents` result replaces it as the
correlation anchor — a strictly better signal, because it is the id the backend
actually created for the spawn.

### F2 — Coordination spawns do not propagate `root_run_id` (out of scope)

The second live run showed the child run's `rootRunId` is absent. Read-only
confirmation against dev confirms this is systematic, not a race:

```sql
SELECT count(*) AS child_runs, count(root_run_id) AS with_root
FROM kb.agent_runs WHERE parent_run_id IS NOT NULL;
-- child_runs = 5, with_root = 0
```

So the parent/child linkage is proven through `parentRunId`, and `rootRunId` is
only cross-checked when both runs carry one. This is a product gap, not a test
gap: the `agent-run-preview` change groups the delegation tree by root run, so a
child with no root cannot be attached to that tree. It is deliberately **not**
fixed here — this change is test-only, and widening it to a server-side fix would
mix an unplanned behavioral change into a test PR. It is recorded in
`tasks.md` as a follow-up.

### F3 — The dev backend can restart mid-run (environmental)

One run failed in `setup` with `session not established after login (HTTP 502)`
because the dev `memory-server` container was being recreated at that moment
(health probe over the host confirmed it). The spec and the gateway were
unaffected; the run passed once the backend was healthy. Nothing to change in
the spec — recorded so a 502 in `setup` is not mistaken for a delegation
regression.

### D4 — Isolated project, always cleaned up

The spec seeds a fresh, uniquely named project and all three agents in it
(mirroring the MCP scenario), so it never races other specs on the shared
bootstrap tenant. `finally` deletes all three agents and the project via
`page.request`, each `.catch(() => {})`, and reactivates the bootstrap project
if the template does.

### D5 — Forced prompt, bounded waits

The chat turn uses an imperative prompt that requires the tool call before any
final answer, mirroring how the MCP scenario forces its tool use. All waiting is
`waitForResponse` / `expect.poll` / bounded poll loops with explicit timeouts and
explicit timeout messages; no `waitForTimeout` is used as a correctness
mechanism.

### D6 — Validate the rejection and disable paths too

The happy-path loop does not exercise the delegation surface's two
failure/teardown paths, so the spec covers both through the same UI + API
pattern:

- **No-target rejection**: a third agent C enables the toggle with no target
  selected and submits; the PRG redirect carries `?err=<message>`, the decoded
  error must name the missing target, and `GET /api/agents/:C` must show neither
  delegation tool nor a `spawnPolicy`.
- **Disable removes the surface**: unchecking the toggle on A strips both
  delegation tools and the `spawnPolicy` from the stored definition; the spec
  then re-enables A and re-asserts the persisted tools/allow so the live chat
  turn still runs delegated.

## Risks

- **Model non-compliance**: the model may answer instead of calling the tool. The
  forced prompt plus the same skip-on-error handling used by the MCP scenario
  keeps this from being recorded as a product regression.
- **Unoffered tool masquerading as model non-compliance**: a completed turn that
  never calls `spawn_agents` could be either "the model declined" or "the tool
  was never offered". The skip branch therefore first asserts the delegator run's
  resolved `tools` contains `spawn_agents` — an unoffered tool is a delegation
  regression and fails instead of skipping; only a genuinely offered but declined
  tool skips.
- **Run visibility lag**: child runs may not be listed the instant the turn ends,
  which is why the child-run assertions poll with a bounded timeout and fail with
  the last observed status.
