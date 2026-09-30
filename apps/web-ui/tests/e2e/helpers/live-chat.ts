import { Page, expect, test } from '@playwright/test';
import { readBootstrap, createProject } from './bootstrap';
import { addProvider } from './providers';
import { expectAppPage } from './page';

// Shared scaffolding for the live-LLM chat scenarios (scenarios/chat/). Each
// scenario runs on a FRESH scratch project so the session rail, provider and
// agents are isolated from the bootstrap tenant and from every other run.
//
// Env vars (see tests/e2e/.env.e2e.example — no new keys):
//   E2E_SCENARIO_LLM_PROVIDER/API_KEY/BASE_URL/MODEL  live provider for the
//       scratch project (each chat turn calls a real model; skip when key unset)

export const PROVIDER = process.env.E2E_SCENARIO_LLM_PROVIDER || 'openai';
export const API_KEY = process.env.E2E_SCENARIO_LLM_API_KEY || '';
export const BASE_URL = process.env.E2E_SCENARIO_LLM_BASE_URL || 'http://litellm:4000/v1';
// The add-provider form renders the base_url field only for the OpenAI-
// compatible provider; passing a base URL for any other provider would make
// fillProviderForm wait on a non-existent field and time out.
export const PROVIDER_BASE_URL = PROVIDER === 'openai' ? BASE_URL : undefined;
// The scenario suite treats E2E_SCENARIO_LLM_MODEL as the already-prefixed
// "provider/model" catalog value; tolerate a bare value but never double-prefix.
export const MODEL = process.env.E2E_SCENARIO_LLM_MODEL || 'openai/deepseek-v4-flash';
export const AGENT_MODEL = MODEL.includes('/') ? MODEL : `${PROVIDER}/${MODEL}`;

/** Message used by every live-chat scenario when the key is unset. */
export const SKIP_NO_KEY =
  'E2E_SCENARIO_LLM_API_KEY is not set — the provider save is live-validated ' +
  'and the chat turn calls a real model. Set it in tests/e2e/.env.e2e to run ' +
  'this scenario (defaults target the dev litellm: openai @ http://litellm:4000/v1).';

export function requireBootstrap() {
  const bootstrap = readBootstrap();
  expect(bootstrap, 'setup project must run first').toBeTruthy();
  return bootstrap!;
}

/**
 * Save the live provider through the settings UI. The memory backend
 * live-validates the save (catalog sync + a real generate call), so a
 * rejection is an environment problem, not a product regression — skip with
 * the backend's copy. Returns true when the provider saved.
 */
export async function saveScenarioProvider(page: Page): Promise<boolean> {
  const saved = await addProvider(page, PROVIDER, API_KEY, PROVIDER_BASE_URL);
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
    return false;
  }
  return true;
}

export interface ScratchAgent {
  projectId: string;
  agentId: string;
}

/**
 * Seed a fresh project, save the live provider, and create one agent via the
 * API (the agent modal cannot express the deterministic system prompt +
 * tool-policy config these tests need). Skips the test when the environment
 * rejects a step. Returns the ids.
 */
export async function createScratchAgent(
  page: Page,
  name: string,
  systemPrompt: string,
  tools: string[],
  defaultToolPolicy: string,
): Promise<ScratchAgent> {
  const bootstrap = requireBootstrap();

  const projectId = await createProject(page, bootstrap.orgId, name);

  if (!(await saveScenarioProvider(page))) {
    // test.skip throws; the guard keeps the type narrow for callers.
    throw new Error('unreachable: provider save did not succeed');
  }

  const createResp = await page.request.post('/api/agents', {
    data: {
      name,
      systemPrompt,
      tools,
      skills: [],
      defaultToolPolicy,
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
  }
  const created = (await createResp.json()) as { id: string };
  return { projectId, agentId: created.id };
}

/** Navigate to /chat for an agent and wait for the composer to be usable. */
export async function openChat(page: Page, agentId: string): Promise<void> {
  await page.goto(`/chat?agent=${agentId}`);
  await expectAppPage(page, /Chat/);
  const agentSelect = page.locator('#chat-agent');
  await expect(agentSelect).toBeVisible();
  await agentSelect.selectOption(agentId);
  await expect(page.locator('#chat-input')).toBeEnabled();
}

export interface CleanupOptions {
  agentIds?: string[];
  serverIds?: string[];
  projectId?: string;
}

/**
 * Best-effort teardown: delete created agents/servers, reactivate the
 * bootstrap project, delete the scratch project. Idempotent across repeated
 * runs, skips and failures.
 */
export async function cleanup(page: Page, opts: CleanupOptions = {}): Promise<void> {
  const bootstrap = readBootstrap();
  for (const id of opts.agentIds ?? []) {
    await page.request.delete(`/api/agents/${id}`).catch(() => {});
  }
  for (const id of opts.serverIds ?? []) {
    await page.request.delete(`/api/mcp-servers/${id}`).catch(() => {});
  }
  if (bootstrap?.projectId) {
    await page.request.post(`/api/projects/${bootstrap.projectId}/activate`).catch(() => {});
  }
  if (opts.projectId && bootstrap?.orgId) {
    await page.request
      .post(`/projects/delete?projectId=${opts.projectId}&orgId=${bootstrap.orgId}`)
      .catch(() => {});
  }
}

export interface ConversationHistoryItem {
  role?: string;
  content?: string;
  text?: string;
  run_id?: string;
  [key: string]: unknown;
}

/** Read a conversation's persisted history (turns + runs) via the gateway. */
export async function readConversationHistory(
  page: Page,
  conversationId: string,
): Promise<ConversationHistoryItem[]> {
  const resp = await page.request.get(`/api/conversations/${conversationId}/history`);
  if (!resp.ok()) {
    throw new Error(`readConversationHistory failed (HTTP ${resp.status()}): ${await resp.text()}`);
  }
  const body = (await resp.json()) as { items?: ConversationHistoryItem[] };
  return body.items ?? [];
}

/** The assistant markdown node the transcript renders replies into. */
export const ASSISTANT_MD = '#chat-messages .chat.chat-start .chat-bubble .memory-md';

/**
 * Wait until the transcript's text content contains every marker. Used after
 * an inline answer resumes a parked run: memory resumes in the background and
 * the gateway reads the continuation back from history (it is not streamed),
 * so the continuation is detected as rendered transcript text rather than a
 * settled bubble made by the current send.
 */
export async function waitForTranscriptText(
  page: Page,
  markers: string[],
  timeout = 180_000,
): Promise<string> {
  const handle = await page.waitForFunction(
    (needles: string[]) => {
      const el = document.getElementById('chat-messages');
      const text = el ? (el.textContent || '') : '';
      return needles.every((n) => text.includes(n)) ? text : null;
    },
    markers,
    { timeout },
  );
  return (await handle.jsonValue()) as string;
}
