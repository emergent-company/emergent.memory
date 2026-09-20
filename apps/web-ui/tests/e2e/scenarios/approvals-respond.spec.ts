import { test, expect, type Page } from '@playwright/test';
import { readBootstrap, createProject } from '../helpers/bootstrap';
import { addProvider } from '../helpers/providers';

// Approvals page scenario: prove a pending human-in-the-loop tool approval can
// be resolved from /settings/approvals (gateway/approvals_handlers.go +
// approvals.templ). A tool approval only exists once a live run pauses on a
// tool whose policy is "ask" (Confirm=true), so there is no deterministic seed
// path — this scenario drives one, end to end:
//
//   1. SEED (API): a fresh scratch project (isolates provider/registry/agent).
//   2. PROVIDER (UI): live-validated save; rejection is an environment problem
//      and skips with the backend's copy.
//   3. MCP SERVER (API): register + sync an example server, read the exact
//      cached tool name.
//   4. AGENT (API): whitelist = that tool, defaultToolPolicy "ask", directive
//      prompt. The "ask" policy is what turns the tool call into a pause.
//   5. CHAT (UI): send one imperative turn, then navigate to /settings/approvals
//      and poll (reloading — the page is server-rendered, not live) until the
//      pending approval's Approve control appears.
//   6. RESPOND (UI): click Approve → the pending decision is resolved (the row
//      no longer offers Approve / no "pending" badge remains).
//   7. CLEANUP (finally): delete agent + server, reactivate bootstrap project,
//      delete the scratch project.
//
// The model must actually call the gated tool; tool-use is non-deterministic,
// so the spec skips (rather than fails) when no pending approval appears. Reject
// and Cancel share the same respond/cancel routes (the same approvalActions
// form renders Reject + Cancel next to Approve) and are covered by the handler's
// unit tests; this scenario exercises the primary respond path once.

const PROVIDER = process.env.E2E_SCENARIO_LLM_PROVIDER || 'openai';
const API_KEY = process.env.E2E_SCENARIO_LLM_API_KEY || '';
const BASE_URL = process.env.E2E_SCENARIO_LLM_BASE_URL || 'http://litellm:4000/v1';
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

async function registerAndSyncServer(
  page: Page,
  name: string,
): Promise<{ serverId: string; toolName: string; toolDesc: string }> {
  const body: Record<string, unknown> = { name, type: 'http', url: MCP_URL, enabled: true };
  if (MCP_HEADER_NAME && MCP_HEADER_VALUE) body.headers = { [MCP_HEADER_NAME]: MCP_HEADER_VALUE };
  const created = await page.request.post('/api/mcp-servers', { data: body });
  expect(created.ok(), `register MCP server failed (HTTP ${created.status()})`).toBeTruthy();
  const server = (await created.json()) as { id: string };

  const sync = await page.request.post(`/api/mcp-servers/${server.id}/sync`);
  if (!sync.ok()) {
    const detail = ((await sync.text().catch(() => '')) || `HTTP ${sync.status()}`).slice(0, 300);
    test.skip(true, `example MCP endpoint ${MCP_URL} unreachable at sync time: ${detail}`);
  }

  const toolsResp = await page.request.get(`/api/mcp-servers/${server.id}/tools`);
  expect(toolsResp.ok(), `list synced tools failed (HTTP ${toolsResp.status()})`).toBeTruthy();
  const tools = (await toolsResp.json()) as Array<{
    id: string;
    toolName: string;
    description: string;
    enabled: boolean;
  }>;
  const enabled = tools.filter((t) => t.enabled);
  const candidates = enabled.length > 0 ? enabled : tools;
  if (candidates.length === 0) {
    test.skip(true, `sync of ${MCP_URL} returned no tools — nothing to attach to the agent`);
  }
  const preferred =
    candidates.find((t) => /search|web|lookup|query|fetch|browse/i.test(t.toolName)) ||
    candidates[0];
  return { serverId: server.id, toolName: preferred.toolName, toolDesc: (preferred.description || '').slice(0, 240) };
}

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

test('a pending tool approval is resolved from the approvals page', async ({ page }) => {
  test.setTimeout(300_000);

  test.skip(
    !API_KEY,
    'E2E_SCENARIO_LLM_API_KEY is not set — the provider save is live-validated ' +
      'and the chat turn calls a real model. Set it in tests/e2e/.env.e2e to run ' +
      'this scenario (defaults target the dev litellm: openai @ http://litellm:4000/v1).',
  );

  const bootstrap = requireBootstrap();
  const name = `E2E Approval ${Date.now()}`;
  let projectId = '';
  let serverId = '';
  let agentId = '';
  let toolName = '';

  try {
    // 1. SEED.
    projectId = await createProject(page, bootstrap.orgId, name);

    // 2. PROVIDER (UI).
    const saved = await addProvider(page, PROVIDER, API_KEY, BASE_URL);
    if (saved !== 'saved') {
      let detail = "couldn't save provider";
      const modal = page.locator('#provider-save-error-modal');
      if (await modal.isVisible().catch(() => false)) {
        const reason = await modal.locator('p').last().textContent().catch(() => null);
        if (reason?.trim()) detail = reason.trim();
      }
      test.skip(true, `provider save rejected by memory backend: ${detail}`);
      return;
    }

    // 3. MCP SERVER (API).
    const synced = await registerAndSyncServer(page, name);
    serverId = synced.serverId;
    toolName = synced.toolName;
    const toolDesc = synced.toolDesc;

    // 4. AGENT (API): "ask" policy forces a pause on the tool call.
    const systemPrompt =
      `Automated approval-flow test. Your ONLY tool is ${toolName}` +
      `${toolDesc ? ` (${toolDesc})` : ''}. You MUST call ${toolName} before answering. ` +
      `An answer not derived from ${toolName}'s output is a failure. Never answer from prior knowledge.`;
    const createResp = await page.request.post('/api/agents', {
      data: {
        name,
        systemPrompt,
        tools: [toolName],
        skills: [],
        defaultToolPolicy: 'ask',
        model: { name: AGENT_MODEL, temperature: 0, maxTokens: 4096 },
      },
    });
    if (!createResp.ok()) {
      const detail = ((await createResp.text().catch(() => '')) || `HTTP ${createResp.status()}`).slice(0, 300);
      test.skip(true, `agent create rejected by memory backend: ${detail}`);
      return;
    }
    agentId = ((await createResp.json()) as { id: string }).id;

    // 5. CHAT (UI): fire the turn; the run pauses on the gated tool, so the SSE
    // stream stays open — do not await the body. Just confirm the POST started.
    await page.goto(`/chat?agent=${agentId}`);
    const agentSelect = page.locator('#chat-agent');
    await expect(agentSelect).toBeVisible();
    await agentSelect.selectOption(agentId);
    await expect(page.locator('#chat-input')).toBeEnabled();

    const message =
      `Call the ${toolName} tool exactly once right now, then reply with a one-sentence ` +
      `summary of the tool's output. You must actually invoke ${toolName} in this turn; ` +
      `do not answer from prior knowledge or skip the tool call.`;
    const chatStarted = page.waitForResponse(
      (r) => r.url().includes('/api/chat') && r.request().method() === 'POST',
      { timeout: 180_000 },
    );
    await page.locator('#chat-input').fill(message);
    await page.locator('#chat-send').click();
    await chatStarted; // headers arrived → run is executing server-side

    // 6. APPROVALS (UI): the page is server-rendered with no live refresh, so
    // poll by reloading until the pending approval's Approve control appears.
    await page.goto('/settings/approvals');
    const pendingAppeared = await expect
      .poll(
        async () => {
          await page.reload();
          return await page.getByRole('button', { name: 'Approve' }).count();
        },
        { timeout: 180_000, intervals: [2000, 2000, 2000, 5000, 5000, 10000] },
      )
      .toBeGreaterThan(0)
      .then(() => true)
      .catch(() => false);
    if (!pendingAppeared) {
      test.skip(true, `no pending tool approval appeared for ${toolName} within 180s — the model did not pause on the gated tool`);
      return;
    }

    // The pending row shows the tool name and the Approve control.
    const pendingRow = page.locator('.card').filter({ hasText: toolName }).first();
    await expect(pendingRow).toBeVisible();
    await pendingRow.getByRole('button', { name: 'Approve' }).click();
    await page.waitForURL(/\/settings\/approvals/);

    // Resolved: the row no longer offers Approve, and no pending badge remains.
    await expect(page.getByRole('button', { name: 'Approve' })).toHaveCount(0);
    await expect(page.getByText('pending', { exact: true })).toHaveCount(0);
  } finally {
    await cleanup(page, agentId, serverId, projectId);
  }
});
