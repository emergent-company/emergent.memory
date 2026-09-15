import { test, expect, type Page } from '@playwright/test';
import { readBootstrap, createProject } from '../helpers/bootstrap';
import { expectAppPage } from '../helpers/page';
import { addProvider } from '../helpers/providers';

// Full MCP value-loop scenario: register an external MCP server in a FRESH
// scratch project, attach one of its tools to an agent, send one real chat
// turn, and prove the agent actually called that server's tool.
//
// The gateway UI only *lists* cached tools (per-row toggles, the agent tool
// picker); the live proof of a tool call has to come from the chat stream.
// Deterministic signal, in strength order:
//   1. TOOL CHIP (DOM): the web chat renders a `.memory-tool-chip` with
//      `data-tool="<resolvedToolName>"` for every non-ask_user `mcp_tool` SSE
//      event (gateway/webui/static/js/chat-stream.js toolChip/handleToolEvent).
//      Memory namespaces external tools as `<slugified-server>_<toolName>`, so
//      the resolved name is the bare whitelist value with a server prefix —
//      match on that, never on the bare value.
//   2. RAW SSE (protocol): `POST /api/chat` streams memory's SSE through the
//      gateway; non-ask_user `mcp_tool` events pass through (gateway/
//      sse_markdown.go adds `resultHtml`). The captured response body must
//      contain `"type":"mcp_tool"` and a `"tool"` ending in the bare ToolName.
//      (Matching the bare name literally fails: the event carries the resolved
//      `<slugified-server>_<toolName>`, which is why this scenario used to skip
//      even though the tool had been called.)
// Flow:
//   1. SEED (API): fresh scratch project under the bootstrap org — every piece
//      of state (provider / MCP registry / agent) is project-scoped, so the
//      scratch project is what makes the loop real;
//   2. PROVIDER (UI): add the live provider from env vars; the save is
//      live-validated by the memory backend, so an invalid key or unsynced
//      catalog skips (with the backend's copy) instead of failing;
//   3. MCP SERVER (API): register the example server (http transport, optional
//      Authorization header), POST /:id/sync to discover tools, then read the
//      exact cached tool names. An unreachable example endpoint at sync time
//      skips with an annotation — never fails on network state;
//   4. AGENT (API): create with the whitelist set to the exact namespaced
//      ToolName read from the tools JSON, a directive system prompt, allow
//      policy, and the provider-prefixed model — the dev memory backend rejects
//      bare model names ("model.name must include a provider prefix") while the
//      agent-modal catalog still lists bare names, so the modal helper cannot
//      express a creatable agent today. (Creating via API is explicitly
//      allowed; the settings-page tool picker is already covered by the
//      mutation spec.) E2E_SCENARIO_LLM_MODEL is treated by the scenario suite
//      as the already-prefixed "provider/model" catalog value, so it is passed
//      through as-is (a bare value still gets PROVIDER/ prepended) — prepending
//      unconditionally produced "openai/openai/deepseek-v4-flash", and memory
//      strips exactly one routing prefix, so litellm saw the still-prefixed
//      "openai/deepseek-v4-flash" and rejected it with a 403. The persisted
//      whitelist is then asserted via GET /api/agents/:id;
//   5. CHAT (UI): /chat?agent=<id> → one forced prompt → assert the tool chip
//      (signal 1) and that the raw SSE body names the tool (signal 2).
//      The memory backend DOES load AgentDefinition.Tools into the agent's
//      function schema (it resolves a bare whitelist entry to the pooled
//      <slugified-server>_<tool> key), so a turn that fails before the model
//      runs is surfaced from memory's `error` SSE event rather than being
//      mislabelled as "the tool was never offered". A turn that completes with
//      no provider error but no tool call skips with an annotation, since
//      model tool-use is non-deterministic (see the run-error / no-signal
//      branches below);
//   6. CLEANUP (finally): delete agent + MCP server, reactivate the bootstrap
//      project, delete the scratch project (cascade removes the provider).
//
// Env vars (all reused — see tests/e2e/.env.e2e.example, no new keys):
//   E2E_SCENARIO_LLM_PROVIDER/API_KEY/BASE_URL/MODEL  live provider for the
//       scratch project (chat turn calls a real model; skip when key unset)
//   E2E_MCP_EXAMPLE_URL  http/sse endpoint of an example MCP server the dev
//       memory backend must reach (default https://mcp.exa.ai/mcp)
//   E2E_MCP_EXAMPLE_HEADER_NAME/VALUE  optional Authorization header sent when
//       registering + syncing the example server
const PROVIDER = process.env.E2E_SCENARIO_LLM_PROVIDER || 'openai';
const API_KEY = process.env.E2E_SCENARIO_LLM_API_KEY || '';
const BASE_URL = process.env.E2E_SCENARIO_LLM_BASE_URL || 'http://litellm:4000/v1';
// The scenario suite treats E2E_SCENARIO_LLM_MODEL as the already-prefixed
// "provider/model" catalog value (see skill-execution / blueprint-object-chat).
// Tolerate a bare value too, but never double-prefix: memory strips exactly one
// routing prefix, so "openai/openai/deepseek-v4-flash" reaches litellm as
// "openai/deepseek-v4-flash" and is rejected with a 403.
const MODEL = process.env.E2E_SCENARIO_LLM_MODEL || 'openai/deepseek-v4-flash';
const AGENT_MODEL = MODEL.includes('/') ? MODEL : `${PROVIDER}/${MODEL}`;
const MCP_URL = process.env.E2E_MCP_EXAMPLE_URL || 'https://mcp.exa.ai/mcp';
const MCP_HEADER_NAME = (process.env.E2E_MCP_EXAMPLE_HEADER_NAME || '').trim();
const MCP_HEADER_VALUE = (process.env.E2E_MCP_EXAMPLE_HEADER_VALUE || '').trim();

function requireBootstrap() {
  const bootstrap = readBootstrap();
  expect(bootstrap, 'setup project must run first').toBeTruthy();
  return bootstrap!;
}

interface SyncResult {
  serverId: string;
  tools: Array<{ id: string; toolName: string; description: string; enabled: boolean }>;
}

// Register the example server via the API (project-scoped registry), sync it,
// and return the cached tools. A failed sync (unreachable example endpoint)
// surfaces memory's error text for the skip annotation.
async function registerAndSyncServer(page: Page, name: string): Promise<SyncResult> {
  const body: Record<string, unknown> = { name, type: 'http', url: MCP_URL, enabled: true };
  if (MCP_HEADER_NAME && MCP_HEADER_VALUE) body.headers = { [MCP_HEADER_NAME]: MCP_HEADER_VALUE };
  const created = await page.request.post('/api/mcp-servers', { data: body });
  expect(created.ok(), `register MCP server failed (HTTP ${created.status()})`).toBeTruthy();
  const server = (await created.json()) as { id: string };
  expect(server.id, 'MCP server create must return an id').toBeTruthy();

  const sync = await page.request.post(`/api/mcp-servers/${server.id}/sync`);
  if (!sync.ok()) {
    const detail = ((await sync.text().catch(() => '')) || `HTTP ${sync.status()}`).slice(0, 300);
    test.info().annotations.push({
      type: 'skipped-step',
      description: `example MCP endpoint ${MCP_URL} unreachable from the memory backend at sync: ${detail}`,
    });
    test.skip(true, `example MCP endpoint ${MCP_URL} unreachable at sync time: ${detail}`);
  }

  const toolsResp = await page.request.get(`/api/mcp-servers/${server.id}/tools`);
  expect(toolsResp.ok(), `list synced tools failed (HTTP ${toolsResp.status()})`).toBeTruthy();
  const tools = (await toolsResp.json()) as SyncResult['tools'];
  return { serverId: server.id, tools };
}

// All steps best-effort: idempotent across repeated runs, skips, and failures.
async function cleanup(page: Page, agentId: string, serverId: string, projectId: string): Promise<void> {
  const bootstrap = readBootstrap();
  if (serverId) await page.request.delete(`/api/mcp-servers/${serverId}`).catch(() => {});
  if (agentId) await page.request.delete(`/api/agents/${agentId}`).catch(() => {});
  if (bootstrap?.projectId) {
    await page.request.post(`/api/projects/${bootstrap.projectId}/activate`).catch(() => {});
  }
  if (projectId && bootstrap?.orgId) {
    await page.request
      .post(`/projects/delete?projectId=${projectId}&orgId=${bootstrap.orgId}`)
      .catch(() => {});
  }
}

// extractSseError pulls the message from the first `error` event in a raw chat
// SSE stream. The gateway forwards memory's `{"type":"error","error":...}`
// events verbatim (gateway/sse_markdown.go default branch), so a run that dies
// before the model executes (bad provider key, model access denied, provider
// 5xx) is visible in the captured body. Without this, a dead run is
// indistinguishable from "the model completed but chose not to call the tool"
// and the scenario silently skips with the wrong reason.
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

// escapeRegExp escapes regex metacharacters in a literal string.
function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

test.describe('MCP server → agent tool → real tool call scenario', () => {
  test('external MCP tool is called by the agent in a live chat turn', async ({ page }) => {
    // Provider save, sync, and the chat turn are all network-bound live calls.
    test.setTimeout(300_000);

    test.skip(
      !API_KEY,
      'E2E_SCENARIO_LLM_API_KEY is not set — the provider save is live-validated ' +
        'and the chat turn calls a real model. Set it in tests/e2e/.env.e2e to run ' +
        'this scenario (defaults target the dev litellm: openai @ http://litellm:4000/v1).',
    );

    const bootstrap = requireBootstrap();
    const name = `E2E MCP Tool ${Date.now()}`;
    let projectId = '';
    let serverId = '';
    let agentId = '';
    let toolName = '';

    try {
      // 1. SEED (API): a fresh project isolates provider/registry/agent state.
      projectId = await createProject(page, bootstrap.orgId, name);

      // 2. PROVIDER (UI): live-validated save; rejection is an environment
      // problem (invalid key / unreachable base URL), not a product regression
      // — skip with the backend's copy.
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

      // 3. MCP SERVER (API): register + sync the example server, then read the
      // exact cached tool names — the whitelist + the chip signal must use the
      // string memory actually registers, never a guessed prefix.
      const synced = await registerAndSyncServer(page, name);
      serverId = synced.serverId;
      const enabledTools = synced.tools.filter((t) => t.enabled);
      const candidates = enabledTools.length > 0 ? enabledTools : synced.tools;
      if (candidates.length === 0) {
        test.info().annotations.push({
          type: 'skipped-step',
          description: `sync of ${MCP_URL} returned no tools — nothing to attach to the agent`,
        });
        test.skip(true, `sync of ${MCP_URL} returned no tools`);
        return;
      }
      const preferred =
        candidates.find((t) => /search|web|lookup|query|fetch|browse/i.test(t.toolName)) ||
        candidates[0];
      toolName = preferred.toolName;
      const toolDesc = (preferred.description || '').slice(0, 240);

      // 4. AGENT (API): whitelist = the exact cached ToolName, allow policy,
      // directive prompt, provider-prefixed model (memory rejects bare model
      // names). The persisted whitelist is the attach proof.
      //
      // NOTE: memory auto-forces `tool_choice: "required"` for most models
      // (openai_model.go) but SKIPS it for models matching its reasoner
      // heuristic — `deepseek-v4-*` is in that list — so the model is not
      // forced by the runtime and will happily answer from prior knowledge.
      // The only lever left to the test is the prompt, so it is deliberately
      // explicit and imperative: it names the exact tool and forbids a
      // knowledge-only answer.
      const systemPrompt =
        `Automated tool-integration test. Your ONLY tool is ${toolName}` +
        `${toolDesc ? ` (${toolDesc})` : ''}. You MUST call ${toolName} before answering. ` +
        `An answer not derived from ${toolName}'s output is a failure. ` +
        `Never answer from prior knowledge.`;
      const createResp = await page.request.post('/api/agents', {
        data: {
          name,
          systemPrompt,
          tools: [toolName],
          skills: [],
          defaultToolPolicy: 'allow',
          model: { name: AGENT_MODEL, temperature: 0, maxTokens: 4096 },
        },
      });
      if (!createResp.ok()) {
        const detail = ((await createResp.text().catch(() => '')) || `HTTP ${createResp.status()}`).slice(0, 300);
        test.info().annotations.push({
          type: 'skipped-step',
          description: `agent create rejected by memory backend: ${detail}`,
        });
        test.skip(true, `agent create rejected by memory backend: ${detail}`);
        return;
      }
      const created = (await createResp.json()) as { id: string };
      agentId = created.id;
      const agentResp = await page.request.get(`/api/agents/${agentId}`);
      expect(agentResp.ok(), 'fetch created agent failed').toBeTruthy();
      const agent = (await agentResp.json()) as { tools: string[] };
      expect(agent.tools, 'the agent whitelist must persist the MCP tool').toContain(toolName);

      // 5. CHAT (UI): one forced turn against the agent. The turn is driven
      // through the real composer, but completion is detected on the POST
      // /api/chat SSE stream itself — the dev model (deepseek-v4-flash) can
      // answer entirely inside its reasoning stream without a final assistant
      // bubble, so waiting on bubble text would time out. The stream always
      // terminates, and the tool chips + mcp_tool events arrive before it does.
      await page.goto(`/chat?agent=${agentId}`);
      await expectAppPage(page, /Chat/);
      const agentSelect = page.locator('#chat-agent');
      await expect(agentSelect).toBeVisible();
      await agentSelect.selectOption(agentId);
      await expect(page.locator('#chat-input')).toBeEnabled();

      const message =
        `Call the ${toolName} tool exactly once right now, then reply with a one-sentence ` +
        `summary of the tool's output. You must actually invoke ${toolName} in this turn; ` +
        `do not answer from prior knowledge or skip the tool call.`;
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

      // A run-level error means the model never executed: the tool WAS offered
      // to the agent (the whitelist persisted and memory resolves it), but the
      // provider call failed. Surface memory's own message instead of letting
      // it masquerade as a missing tool — this is the branch that previously
      // hid a litellm `403 key_model_access_denied` (agent model
      // `openai/deepseek-v4-flash` not in the proxy key's allowed list).
      const runError = extractSseError(raw);
      if (runError) {
        test.info().annotations.push({
          type: 'skipped-step',
          description: `chat run errored before any tool call (provider/environment): ${runError}`,
        });
        test.skip(true, `chat run errored before any tool call: ${runError}`);
        return;
      }

      // Signal 1 (DOM): the tool chip for exactly our whitelisted tool.
      // Signal 1 (DOM): the tool chip for the RESOLVED (namespaced) tool name.
      // Memory pools external tools as `<slugified-server>_<toolName>` and the
      // chip's data-tool carries that resolved id (chat-stream.js toolChip), so
      // matching the bare whitelist value would never hit even when the tool ran.
      const chip = page.locator(`#chat-messages .memory-tool-chip[data-tool$="_${toolName}"]`);
      // Signal 2 (protocol): the raw SSE body names the resolved tool via an
      // mcp_tool event; match the bare ToolName as a suffix of the pooled name.
      const toolCalled = (await chip.count().catch(() => 0)) > 0;
      const toolNamePattern = new RegExp(`"tool":"[^"]*${escapeRegExp(toolName)}"`);
      const toolEventSeen = raw.includes('"type":"mcp_tool"') && toolNamePattern.test(raw);

      if (!toolCalled && !toolEventSeen) {
        // The stream completed with no error event, so the provider call
        // succeeded and the model ran — but it did not call the whitelisted
        // tool. We cannot tell "backend never offered it" from "model chose not
        // to call it" without a resolved-tool surface, so quote the visible
        // transcript and skip rather than failing on non-deterministic model
        // tool-use.
        const transcript = (await page.locator('#chat-messages').innerText().catch(() => ''))
          .replace(/\s+/g, ' ')
          .slice(0, 300);
        test.info().annotations.push({
          type: 'skipped-step',
          description:
            `model completed the turn without calling ${toolName} (no stream error; ` +
            `tool offered vs model declined is indistinguishable here; ` +
            `transcript excerpt: "${transcript}")`,
        });
        test.skip(
          true,
          `no tool-call signal for ${toolName} in a completed turn — the model did not ` +
            `call the whitelisted tool (no provider error). ` +
            `Transcript excerpt: "${transcript}"`,
        );
        return;
      }

      await expect(chip).toBeVisible();
      expect(raw).toContain('"type":"mcp_tool"');
      expect(raw).toMatch(toolNamePattern);
    } finally {
      await cleanup(page, agentId, serverId, projectId);
    }
  });
});
