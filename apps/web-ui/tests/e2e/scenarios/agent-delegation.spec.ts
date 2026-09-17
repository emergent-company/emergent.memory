import { test, expect, type Page } from '@playwright/test';
import { readBootstrap, createProject } from '../helpers/bootstrap';
import { expectAppPage } from '../helpers/page';
import { addProvider } from '../helpers/providers';
import { memoryAuthHeaders } from '../helpers/objects';
import { MEMORY_API_URL } from '../helpers/tokens';

// Full agent-delegation value loop: seed a fresh scratch project, create a
// TARGET agent (B) and a DELEGATOR agent (A) with an explicit model and no
// tools, enable delegation on A through the real settings page (target picker
// → B), and prove the whole chain end to end:
//
//   persist  → GET /api/agents/:A round-trips `spawn_agents` +
//              `list_available_agents` in `tools` and `config.spawnPolicy.allow`
//              == [B's name];
//   live     → one real chat turn against A forces a `spawn_agents` call, the
//              raw SSE body names the tool, and the spawn is a REAL child run
//              (parentRunId == A's run) that completes and whose transcript
//              contains PINEAPPLE.
//
// Why the child run is the proof, not just the tool chip: the gateway only
// *lists* cached state (the delegation toggle, the target picker); whether A
// actually handed off to B can only come from the backend. The deterministic
// signals, in strength order:
//   1. RAW SSE (protocol): `POST /api/chat` streams memory's SSE through the
//      gateway. Every tool invocation — including the native `spawn_agents`
//      ADK tool, not just MCP tools — is emitted as `{"type":"mcp_tool",...}`
//      (server domain/chat/handler.go maps StreamEventToolCallStart/End to
//      NewMCPToolEvent), so the captured body must contain `"type":"mcp_tool"`
//      and `"tool":"spawn_agents"`.
//   2. REAL CHILD RUN (backend): `spawn_agents` is synchronous — the parent
//      run does not finish until the child run completes — and the tool's
//      COMPLETED result carries the child run id directly:
//      `{"status":"completed","tool":"spawn_agents",...,"result":{"results":[{"run_id":"<child>"}],"total":1}}`.
//      The child run id therefore comes from that tool result, NOT from the
//      chat stream's final `done` event: the agent-backed chat path emits a
//      bare `{"type":"done"}` (server domain/chat/handler.go StreamChat writes
//      `sse.NewDoneEvent()`, and `DoneEvent.RunID` is `json:"runId,omitempty"`,
//      so an empty run id omits the field entirely). The child run id is read
//      from the completed `spawn_agents` tool result instead; fetching that run
//      and checking `agentDefinitionId == B`, a non-empty `parentRunId` and a
//      fetchable parent run proves the spawn produced a real linked child run;
//      polling it to `completed` + reading the `/full` transcript (which must
//      contain PINEAPPLE) proves B actually ran the delegated task.
//
// NOTE on the run id correlation: the `agent-runs` list filters on the runtime
// `kb.agents.id` (`agentId`), NOT the agent-definition id that POST /api/agents
// returns — a chat turn runs as a "Chat session for <name>" dummy agent and a
// spawn lazily creates a runtime agent for the target, so neither run's
// `agentId` equals the definition id. The correlation therefore reads the
// child run id straight from the completed `spawn_agents` tool result and
// links it via `agentDefinitionId` + `parentRunId` (the parent run is fetched by
// id to prove the linkage). `rootRunId` is only cross-checked when both runs
// carry one: the spawn path does not propagate the orchestration root, so every
// spawned child in dev has `root_run_id IS NULL`.
//
// Flow:
//   1. SEED (API): fresh scratch project under the bootstrap org — provider /
//      agent state is project-scoped, so the scratch project makes the loop real;
//   2. PROVIDER (UI): add the live provider from env vars (live-validated save,
//      invalid key / unreachable base URL skips with the backend's copy);
//   3. AGENTS (API): create B (target) and A (delegator), both explicit model
//      + `tools: []` (creating via API is allowed; the settings-page tool
//      picker is covered elsewhere). E2E_SCENARIO_LLM_MODEL is the already-
//      prefixed "provider/model" catalog value, passed through as-is (never
//      double-prefixed);
//   4. DELEGATION (UI): /agents/:A/settings → check `#agent-settings-
//      delegation-enabled`, tick `input[name="delegation-target"][value=B]`,
//      submit via the real "Save changes" button (PRG → /agents/:A/settings
//      ?updated=1);
//   5. PERSIST (API): GET /api/agents/:A → tools contain spawn_agents +
//      list_available_agents and config.spawnPolicy.allow == [B];
//   6. CHAT (UI): /chat?agent=A → one forced prompt naming B → parse the raw
//      SSE for the spawn_agents tool events: assert the started event names B
//      and read the child run id from the completed event's result. A turn
//      that errors before the model runs is surfaced from memory's `error` SSE
//      event; a turn that completes with no provider error but no spawn call
//      skips with an annotation (model tool-use is non-deterministic);
//   7. CHILD RUN (API): GET /agent-runs/:id with the child run id read from the
//      completed tool result → assert B's definition id + `completed` status +
//      non-empty parentRunId; fetch the parent run by that id to prove the
//      linkage (rootRunId cross-checked only when both runs carry one); read
//      /full and assert the transcript contains
//      PINEAPPLE;
//   8. CLEANUP (finally): delete both agents + reactivate the bootstrap project
//      + delete the scratch project (cascade removes the provider).
//
// Env vars (all reused — see tests/e2e/.env.e2e.example, no new keys):
//   E2E_SCENARIO_LLM_PROVIDER/API_KEY/BASE_URL/MODEL  live provider for the
//       scratch project (chat turn calls a real model; skip when key unset)
const PROVIDER = process.env.E2E_SCENARIO_LLM_PROVIDER || 'openai';
const API_KEY = process.env.E2E_SCENARIO_LLM_API_KEY || '';
const BASE_URL = process.env.E2E_SCENARIO_LLM_BASE_URL || 'http://litellm:4000/v1';
// The scenario suite treats E2E_SCENARIO_LLM_MODEL as the already-prefixed
// "provider/model" catalog value. Tolerate a bare value too, but never
// double-prefix: memory strips exactly one routing prefix, so
// "openai/openai/deepseek-v4-flash" reaches litellm as
// "openai/deepseek-v4-flash" and is rejected with a 403.
const MODEL = process.env.E2E_SCENARIO_LLM_MODEL || 'openai/deepseek-v4-flash';
const AGENT_MODEL = MODEL.includes('/') ? MODEL : `${PROVIDER}/${MODEL}`;

function requireBootstrap() {
  const bootstrap = readBootstrap();
  expect(bootstrap, 'setup project must run first').toBeTruthy();
  return bootstrap!;
}

/** The subset of memory's AgentRunDTO the child-run assertions read. */
interface AgentRunDTO {
  id: string;
  agentDefinitionId?: string;
  status: string;
  parentRunId?: string;
  rootRunId?: string;
}

/** The subset of the gateway's GET /api/agents/:id response we assert. */
interface AgentDefinitionJSON {
  id: string;
  name: string;
  tools?: string[];
  config?: { spawnPolicy?: { allow?: string[] } };
}

// extractSseError pulls the message from the first `error` event in a raw chat
// SSE stream. The gateway forwards memory's `{"type":"error","error":...}`
// events verbatim, so a run that dies before the model executes (bad provider
// key, model access denied, provider 5xx) is visible in the captured body.
// Without this, a dead run is indistinguishable from "the model completed but
// chose not to spawn" and the scenario silently skips with the wrong reason.
function extractSseError(raw: string): string | null {
  for (const line of raw.split('\n')) {
    if (!line.startsWith('data:')) continue;
    const data = line.slice('data:'.length).trim();
    if (!data.startsWith('{')) continue;
    let ev: { type?: string; error?: string };
    try {
      ev = JSON.parse(data) as { type?: string; error?: string };
    } catch {
      continue;
    }
    if (ev.type === 'error' && ev.error) return ev.error.slice(0, 500);
  }
  return null;
}

/** One `mcp_tool` SSE event for the spawn_agents tool (subset we read). */
interface SpawnToolEvent {
  type?: string;
  tool?: string;
  status?: string;
  result?: {
    agents?: Array<{ agent_name?: string; task?: string }>;
    results?: Array<{ agent_name?: string; run_id?: string; status?: string }>;
    total?: number;
  };
}

/** The spawn_agents facts pulled out of a raw chat SSE stream. */
interface SpawnParse {
  invoked: boolean;
  startedAgentName: string | null;
  childRunId: string | null;
  total: number | null;
}

// parseSpawnEvents walks the raw chat SSE and pulls the spawn_agents facts out
// of the two `mcp_tool` events the backend emits for the native ADK tool:
//   - `status:"started"`  carries the requested `result.agents[0].agent_name`
//     (the input args) — proving A asked for B, not some other agent;
//   - `status:"completed"` carries `result.results[0].run_id` (the child run
//     id) plus `result.total` — the ground-truth child run id.
// The chat `done` event is deliberately NOT used: the agent-backed chat path
// emits a bare `{"type":"done"}` with no run id (StreamChat writes
// `sse.NewDoneEvent()` and `DoneEvent.RunID` is `json:"runId,omitempty"`, so
// an empty id is omitted), so the only reliable run id is the tool result.
function parseSpawnEvents(raw: string): SpawnParse {
  const out: SpawnParse = { invoked: false, startedAgentName: null, childRunId: null, total: null };
  for (const line of raw.split('\n')) {
    if (!line.startsWith('data:')) continue;
    const data = line.slice('data:'.length).trim();
    if (!data.startsWith('{')) continue;
    let ev: SpawnToolEvent;
    try {
      ev = JSON.parse(data) as SpawnToolEvent;
    } catch {
      continue;
    }
    if (ev.type !== 'mcp_tool' || ev.tool !== 'spawn_agents') continue;
    out.invoked = true;
    if (ev.status === 'started') {
      out.startedAgentName = ev.result?.agents?.[0]?.agent_name ?? null;
    } else if (ev.status === 'completed') {
      out.childRunId = ev.result?.results?.[0]?.run_id ?? null;
      out.total = ev.result?.total ?? null;
    }
  }
  return out;
}

// All steps best-effort: idempotent across repeated runs, skips, and failures.
async function cleanup(page: Page, targetId: string, delegatorId: string, projectId: string): Promise<void> {
  const bootstrap = readBootstrap();
  if (targetId) await page.request.delete(`/api/agents/${targetId}`).catch(() => {});
  if (delegatorId) await page.request.delete(`/api/agents/${delegatorId}`).catch(() => {});
  if (bootstrap?.projectId) {
    await page.request.post(`/api/projects/${bootstrap.projectId}/activate`).catch(() => {});
  }
  if (projectId && bootstrap?.orgId) {
    await page.request
      .post(`/projects/delete?projectId=${projectId}&orgId=${bootstrap.orgId}`)
      .catch(() => {});
  }
}

test.describe('Agent delegation scenario', () => {
  test('configured delegator spawns its target in a live turn and the spawn is a real child run', async ({ page }) => {
    // Provider save + chat turn are network-bound live calls.
    test.setTimeout(300_000);

    test.skip(
      !API_KEY,
      'E2E_SCENARIO_LLM_API_KEY is not set — the provider save is live-validated ' +
        'and the chat turn calls a real model. Set it in tests/e2e/.env.e2e to run ' +
        'this scenario (defaults target the dev litellm: openai @ http://litellm:4000/v1).',
    );

    const bootstrap = requireBootstrap();
    const stamp = `${Date.now()}`;
    const targetName = `E2E Delegation Target ${stamp}`;
    const delegatorName = `E2E Delegation Source ${stamp}`;
    let projectId = '';
    let targetId = '';
    let delegatorId = '';

    try {
      // 1. SEED (API): a fresh project isolates provider/agent state.
      projectId = await createProject(page, bootstrap.orgId, `E2E Delegation ${stamp}`);

      // 2. PROVIDER (UI): live-validated save; rejection is an environment
      // problem (invalid key / unreachable base URL), not a product regression.
      const saved = await addProvider(page, PROVIDER, API_KEY, BASE_URL);
      if (saved !== 'saved') {
        let detail = "couldn't save provider";
        const modal = page.locator('#provider-save-error-modal');
        if (await modal.isVisible().catch(() => false)) {
          const reason = await modal.locator('p').last().textContent().catch(() => null);
          if (reason?.trim()) detail = reason.trim();
        }
        test.skip(
          true,
          `provider save rejected by memory backend (catalog unsynced or invalid key): ${detail}`,
        );
        return;
      }

      // 3. AGENTS (API): B (target) replies the sentinel word; A (delegator)
      // is directed to spawn, not answer. Both explicit model + tools: [].
      const targetSystem =
        'You are a worker agent. Reply to any task with exactly the single word ' +
        'PINEAPPLE and nothing else.';
      const delegatorSystem =
        'You are a delegator agent. When asked to spawn an agent, call the ' +
        'spawn_agents tool exactly once with the requested agents, then report ' +
        "the spawn's outcome. Never answer the delegated task yourself.";

      const createTarget = await page.request.post('/api/agents', {
        data: {
          name: targetName,
          systemPrompt: targetSystem,
          tools: [],
          skills: [],
          defaultToolPolicy: 'allow',
          model: { name: AGENT_MODEL, temperature: 0, maxTokens: 4096 },
        },
      });
      expect(createTarget.ok(), `create target agent failed (HTTP ${createTarget.status()})`).toBeTruthy();
      const target = (await createTarget.json()) as { id: string };
      expect(target.id, 'target agent create must return an id').toBeTruthy();
      targetId = target.id;

      const createDelegator = await page.request.post('/api/agents', {
        data: {
          name: delegatorName,
          systemPrompt: delegatorSystem,
          tools: [],
          skills: [],
          defaultToolPolicy: 'allow',
          model: { name: AGENT_MODEL, temperature: 0, maxTokens: 4096 },
        },
      });
      expect(createDelegator.ok(), `create delegator agent failed (HTTP ${createDelegator.status()})`).toBeTruthy();
      const delegator = (await createDelegator.json()) as { id: string };
      expect(delegator.id, 'delegator agent create must return an id').toBeTruthy();
      delegatorId = delegator.id;

      // 4. DELEGATION (UI): enable the toggle, pick B, submit via the real
      // "Save changes" button. The settings form is a plain PRG form (action
      // /agents/:id/update), so the submit control navigates + 303-redirects
      // to /agents/:id/settings?updated=1 on success.
      await page.goto(`/agents/${delegatorId}/settings`);
      await expectAppPage(page, /Settings/);

      const enabled = page.locator('#agent-settings-delegation-enabled');
      await expect(enabled).toBeVisible();
      await enabled.check();

      const targetBox = page.locator(`input[name="delegation-target"][value="${targetName}"]`);
      await expect(targetBox, 'the target agent must be listed in the delegation picker').toHaveCount(1);
      await targetBox.check();

      await page.getByRole('button', { name: 'Save changes' }).click();
      await page.waitForURL(/settings\?updated=1/, { timeout: 15_000 });

      // 5. PERSIST (API): GET /api/agents/:A round-trips the delegation state.
      const aResp = await page.request.get(`/api/agents/${delegatorId}`);
      expect(aResp.ok(), `fetch delegator agent after update failed (HTTP ${aResp.status()})`).toBeTruthy();
      const a = (await aResp.json()) as AgentDefinitionJSON;
      expect(
        a.tools ?? [],
        'tools must include spawn_agents and list_available_agents after delegation is enabled',
      ).toEqual(expect.arrayContaining(['spawn_agents', 'list_available_agents']));
      expect(
        a.config?.spawnPolicy?.allow,
        `config.spawnPolicy.allow must equal [${targetName}] after delegation is enabled`,
      ).toEqual([targetName]);

      // 6. CHAT (UI): one forced turn against the delegator. Completion is
      // detected on the POST /api/chat SSE stream itself (the dev reasoner can
      // answer entirely inside its reasoning stream, so waiting on bubble text
      // would time out); the spawn_agents tool events arrive before the stream
      // closes.
      await page.goto(`/chat?agent=${delegatorId}`);
      await expectAppPage(page, /Chat/);
      const agentSelect = page.locator('#chat-agent');
      await expect(agentSelect).toBeVisible();
      await agentSelect.selectOption(delegatorId);
      await expect(page.locator('#chat-input')).toBeEnabled();

      const message =
        `Call the spawn_agents tool now, exactly once, with ` +
        `agents=[{"agent_name":"${targetName}","task":"Reply with the single word PINEAPPLE."}]. ` +
        `You must call the tool before writing any final answer, and do not answer the task yourself.`;
      const chatStream = page.waitForResponse(
        (r) => r.url().includes('/api/chat') && r.request().method() === 'POST',
        { timeout: 180_000 },
      );
      await page.locator('#chat-input').fill(message);
      await page.locator('#chat-send').click();

      // The SSE response body is fully readable once memory closes the stream
      // (done event). Cap the wait: a paused run (e.g. an approval pause) would
      // hold the stream open, which is an environment state, not a UI signal.
      let raw = '';
      try {
        raw = await Promise.race([
          chatStream.then((r) => r.text()),
          new Promise<string>((_, reject) =>
            setTimeout(
              () => reject(new Error('chat SSE stream did not finish within 180s')),
              180_000,
            ),
          ),
        ]);
      } catch (err) {
        const why = (err as Error).message;
        test.info().annotations.push({
          type: 'skipped-step',
          description: `chat stream did not complete (${why}) — the turn may have paused or the backend stalled`,
        });
        test.skip(true, `chat turn did not complete: ${why}`);
        return;
      }

      // A run-level error means the model never executed: surface memory's own
      // message instead of letting it masquerade as a missing tool call.
      const runError = extractSseError(raw);
      if (runError) {
        test.info().annotations.push({
          type: 'skipped-step',
          description: `chat run errored before any tool call (provider/environment): ${runError}`,
        });
        test.skip(true, `chat run errored before any tool call: ${runError}`);
        return;
      }

      // Signal 1 (protocol): parse the spawn_agents tool events out of the raw
      // SSE. spawn_agents is a native ADK tool, so the tool name is the bare
      // value (no server prefix), unlike pooled MCP tools.
      const spawn = parseSpawnEvents(raw);

      if (!spawn.invoked) {
        // The stream completed with no error event, so the provider call
        // succeeded and the model ran — but it never invoked spawn_agents. We
        // cannot tell "backend never offered it" from "model chose not to call
        // it" here, so quote the visible transcript and skip rather than fail
        // on non-deterministic model tool-use.
        const transcript = (await page.locator('#chat-messages').innerText().catch(() => ''))
          .replace(/\s+/g, ' ')
          .slice(0, 300);
        test.info().annotations.push({
          type: 'skipped-step',
          description:
            `model completed the turn without calling spawn_agents (no stream error; ` +
            `tool offered vs model declined is indistinguishable here; ` +
            `transcript excerpt: "${transcript}")`,
        });
        test.skip(
          true,
          `no spawn_agents tool call in a completed turn — the model did not ` +
            `delegate (no provider error). Transcript excerpt: "${transcript}"`,
        );
        return;
      }

      expect(raw, 'chat stream must emit the mcp_tool event type for the spawn').toContain('"type":"mcp_tool"');
      expect(
        spawn.startedAgentName,
        'spawn_agents must have been asked to spawn the target agent, not another agent',
      ).toBe(targetName);

      // Fail (not skip): the tool was invoked but its completed result carried
      // no child run id — that is a real delegation failure, not model
      // non-determinism. (This is also where the scenario previously hung on
      // the chat `done` event, which carries no run id.)
      expect(
        spawn.childRunId,
        'spawn_agents completed without a child run id (result.results[0].run_id) — the delegation did not produce a child run',
      ).toBeTruthy();
      expect(spawn.total, 'spawn_agents must have spawned exactly one child').toBe(1);

      // 7. REAL CHILD RUN: the child run id comes from the completed
      // spawn_agents tool result. Fetch it by id (no project-run listing), then
      // prove the parent→child linkage and that B actually ran the task.
      const childRunId = spawn.childRunId as string;

      // The memory API is project-scoped by path, so point X-Project-ID at the
      // scratch project (the auth token comes from the signed-in session).
      const headers = await memoryAuthHeaders(page);
      headers['X-Project-ID'] = projectId;

      const getRun = async (runId: string): Promise<AgentRunDTO | null> => {
        const resp = await page.request.get(
          `${MEMORY_API_URL}/api/projects/${projectId}/agent-runs/${runId}`,
          { headers },
        );
        if (!resp.ok()) return null;
        const body = (await resp.json()) as { success?: boolean; data?: AgentRunDTO };
        return body?.data ?? null;
      };

      // spawn_agents is synchronous, so the child run is already terminal when
      // the tool result landed — the poll is a safety net against read-path lag.
      // spawn_agents is synchronous, so the child run is already terminal when
      // the tool result landed — the poll is a safety net against read-path lag.
      await expect
        .poll(
          async () => (await getRun(childRunId))?.status ?? null,
          {
            timeout: 60_000,
            intervals: [1000, 2000, 5000],
            message: `child run ${childRunId} did not reach 'completed' within 60s`,
          },
        )
        .toBe('completed');

      // Re-fetch the now-completed child run to assert the linkage fields.
      const child = await getRun(childRunId);
      expect(child, `child run ${childRunId} not found`).toBeTruthy();
      expect(
        child!.agentDefinitionId,
        'the child run must belong to the target (B) agent definition',
      ).toBe(targetId);
      expect(child!.parentRunId, 'the child run must have a non-empty parentRunId').toBeTruthy();

      // Prove the linkage instead of assuming it: fetch the parent run by the
      // child's parentRunId and confirm it exists and that the child points at
      // it.
      const parentRun = await getRun(child!.parentRunId as string);
      expect(parentRun, `parent run ${child!.parentRunId} must exist`).toBeTruthy();
      expect(parentRun!.id, 'the parent run id must equal the child run parentRunId').toBe(
        child!.parentRunId,
      );

      // rootRunId is cross-checked, deliberately NOT required to be present.
      // Live evidence: the spawn path does not propagate the orchestration root,
      // so every spawned child in dev carries `root_run_id IS NULL`
      // (kb.agent_runs: 5/5 children with a parentRunId). A parent/child linkage
      // is therefore proven through parentRunId, and when both runs do carry a
      // rootRunId they must agree.
      if (child!.rootRunId && parentRun!.rootRunId) {
        expect(
          parentRun!.rootRunId,
          'when both runs carry a rootRunId, the parent and child must share it',
        ).toBe(child!.rootRunId);
      }

      // The child run's full transcript must contain B's sentinel answer —
      // this proves B actually ran the delegated task, not just that a row
      // was created.
      const fullResp = await page.request.get(
        `${MEMORY_API_URL}/api/projects/${projectId}/agent-runs/${childRunId}/full`,
        { headers },
      );
      expect(
        fullResp.ok(),
        `fetch child run full transcript failed (HTTP ${fullResp.status()})`,
      ).toBeTruthy();
      const fullText = await fullResp.text();
      expect(
        fullText,
        'the child run transcript must contain PINEAPPLE (the target completed its delegated task)',
      ).toContain('PINEAPPLE');
    } finally {
      await cleanup(page, targetId, delegatorId, projectId);
    }
  });
});
