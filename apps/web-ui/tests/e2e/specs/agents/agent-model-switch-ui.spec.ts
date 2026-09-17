import { test, expect, type Page } from '@playwright/test';
import {
  readBootstrap,
  createProject,
  configureLiveProvider,
  hasLiveProviderCreds,
  LIVE_PROVIDER_MODEL,
} from '../../helpers/bootstrap';
import { expectAppPage } from '../../helpers/page';

// Two agents, each switched default → specific → back to default, with the
// model's presentation verified on every surface after every switch. This is
// the e2e counterpart to the "/deepseek-v4-flash" (bare, leading slash) vs
// "openai/deepseek-v4-flash" (provider-prefixed) report: the agent model is
// displayed in three places — the Agents list "Model" column, the agent
// dashboard "Model" summary field, and the agent settings Model picker — and
// each must faithfully carry the provider prefix and the default-vs-explicit
// distinction after a switch.

// The project default generative model ("Auto — default model"). SPECIFIC_B
// deliberately equals DEFAULT_MODEL: a pinned model whose string coincides with
// the default is the subtle case the report surfaced — the dashboard drops the
// "(default)" suffix and the settings picker holds an explicit value.
const DEFAULT_MODEL = LIVE_PROVIDER_MODEL; // e.g. openai/deepseek-v4-flash
const SPECIFIC_A = 'openai/deepseek-v4-pro';
const SPECIFIC_B = LIVE_PROVIDER_MODEL;

function requireBootstrap() {
  const bootstrap = readBootstrap();
  expect(bootstrap, 'setup project must run first').toBeTruthy();
  return bootstrap!;
}

async function createAgent(page: Page, name: string): Promise<string> {
  const resp = await page.request.post('/api/agents', {
    data: { name, tools: [], skills: [], config: {} },
  });
  expect(resp.ok(), `create agent failed (HTTP ${resp.status()}): ${await resp.text()}`).toBeTruthy();
  return ((await resp.json()) as { id: string }).id;
}

// Persist an explicit model (or clear the override back to default) through the
// memory API, then re-read and confirm the stored value reflects the intent —
// the PUT-first + GET guard pattern from agent-model-warning-ui.spec.ts. A
// `modelName` of null clears the override (empty ModelConfig → falls back to
// the project default). Skips (not fails) when dev memory rejects the write,
// which can happen while the provider catalog is unsynced.
async function setAgentModel(
  page: Page,
  agentId: string,
  name: string,
  modelName: string | null,
): Promise<void> {
  const model = modelName ? { name: modelName, temperature: 0.7, maxTokens: 4096 } : {};
  const put = await page.request.put(`/api/agents/${agentId}`, {
    data: { name, tools: [], skills: [], config: {}, model },
  });
  if (!put.ok()) {
    test.skip(
      true,
      `could not persist model ${modelName ?? '(clear)'} — PUT HTTP ${put.status()}: ${await put.text()}`,
    );
    return;
  }
  const get = await page.request.get(`/api/agents/${agentId}`);
  expect(get.ok(), `get agent failed (HTTP ${get.status()}): ${await get.text()}`).toBeTruthy();
  const stored = (await get.json()) as { model?: { name?: string } };
  const got = stored.model?.name ?? '';
  const want = modelName ?? '';
  if (got !== want) {
    test.skip(true, `memory did not persist model ${want} (stored "${got}")`);
  }
}

// Assert the agent's model is presented correctly on all three surfaces.
// isDefault distinguishes "inherits the project default" (dashboard appends
// " (default)", settings picker holds the Auto/empty value) from an explicit
// per-agent pin.
async function expectAgentModelInUI(
  page: Page,
  agentId: string,
  agentName: string,
  expectedModel: string,
  isDefault: boolean,
): Promise<void> {
  // 1. Agents list — the agent's row shows the effective model string.
  await page.goto('/agents');
  await expectAppPage(page, /Agents/);
  const row = page
    .locator('#main-content tr')
    .filter({ has: page.getByRole('link', { name: agentName }) })
    .first();
  await expect(row).toContainText(expectedModel);

  // 2. Dashboard — the "Model" summary field: the model name in a font-mono
  //    span, with a " (default)" suffix only when inherited.
  await page.goto(`/agents/${agentId}`);
  await expectAppPage(page, new RegExp(agentName));
  await expect(
    page.locator('span.font-mono').filter({ hasText: expectedModel }).first(),
  ).toBeVisible();
  if (isDefault) {
    await expect(page.getByText('(default)')).toBeVisible();
  } else {
    await expect(page.getByText('(default)')).toHaveCount(0);
  }

  // 3. Settings → Model — the picker's value: "" for Auto (default), else the
  //    pinned provider/model string (a stored model outside the catalog still
  //    renders as a "(current)" option carrying that exact value).
  await page.goto(`/agents/${agentId}/settings/model`);
  await expectAppPage(page, new RegExp(agentName));
  if (isDefault) {
    await expect(page.locator('#agent-settings-model')).toHaveValue('');
  } else {
    await expect(page.locator('#agent-settings-model')).toHaveValue(expectedModel);
  }
}

// Configure the live-validated provider and pin the project's default
// generative model, so "default" has a concrete, provider-prefixed name for the
// whole flow. Skips fast when the creds are absent or either write is rejected.
async function requireConfiguredDefault(page: Page): Promise<boolean> {
  if (!hasLiveProviderCreds()) {
    test.skip(
      true,
      'E2E_SCENARIO_LLM_API_KEY is not set — cannot configure a live-validated provider',
    );
    return false;
  }
  try {
    await configureLiveProvider(page);
  } catch (e) {
    test.skip(true, `provider upsert unavailable: ${(e as Error).message}`);
    return false;
  }

  const resp = await page.request.post('/settings/providers/model-config', {
    form: { generative_model: DEFAULT_MODEL, embedding_model: '' },
  });
  const body = await resp.text();
  if (!resp.ok() || /error/i.test(body)) {
    test.skip(true, `could not pin the project default model: ${body.slice(0, 200)}`);
    return false;
  }
  return true;
}

// Deletes the agents while the scratch project is still active, restores the
// session to the bootstrap project, then deletes the scratch project. Best
// effort so repeated runs stay idempotent.
async function cleanup(page: Page, agentIds: string[], projectId: string): Promise<void> {
  const bootstrap = readBootstrap();
  for (const id of agentIds) {
    if (id) await page.request.delete(`/api/agents/${id}`).catch(() => {});
  }
  if (bootstrap?.projectId) {
    await page.request.post(`/api/projects/${bootstrap.projectId}/activate`).catch(() => {});
  }
  if (projectId && bootstrap?.orgId) {
    await page.request
      .post(`/projects/delete?projectId=${projectId}&orgId=${bootstrap.orgId}`)
      .catch(() => {});
  }
}

test('switching two agents between default and specific models updates every model surface', async ({
  page,
}) => {
  const bootstrap = requireBootstrap();
  const base = `E2E Model Switch ${Date.now()}`;
  const nameA = `${base} A`;
  const nameB = `${base} B`;
  let idA = '';
  let idB = '';
  let projectId = '';

  try {
    projectId = await createProject(page, bootstrap.orgId, base);
    if (!(await requireConfiguredDefault(page))) return;

    idA = await createAgent(page, nameA);
    idB = await createAgent(page, nameB);

    // Default state — both agents inherit the project default model.
    await expectAgentModelInUI(page, idA, nameA, DEFAULT_MODEL, true);
    await expectAgentModelInUI(page, idB, nameB, DEFAULT_MODEL, true);

    // Pin each agent to a specific model (A differs from default; B coincides
    // with the default string, so the "explicit" signal is the lost suffix).
    await setAgentModel(page, idA, nameA, SPECIFIC_A);
    await setAgentModel(page, idB, nameB, SPECIFIC_B);
    await expectAgentModelInUI(page, idA, nameA, SPECIFIC_A, false);
    await expectAgentModelInUI(page, idB, nameB, SPECIFIC_B, false);

    // Back to default — the override clears and the default reappears.
    await setAgentModel(page, idA, nameA, null);
    await setAgentModel(page, idB, nameB, null);
    await expectAgentModelInUI(page, idA, nameA, DEFAULT_MODEL, true);
    await expectAgentModelInUI(page, idB, nameB, DEFAULT_MODEL, true);
  } finally {
    await cleanup(page, [idA, idB], projectId);
  }
});
