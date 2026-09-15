import { test, expect, type Page } from '@playwright/test';
import { readBootstrap, createProject, configureProvider } from '../../helpers/bootstrap';
import { expectAppPage } from '../../helpers/page';

// Agent-model config warnings (gateway 9c573b7): an agent with no explicit
// model and no resolvable default renders a severity alert on the agent
// dashboard (/agents/:id) and the agent settings (/agents/:id/settings):
// "error" when the project has no configured provider, "warning" when
// providers exist but no project default generative model is pinned. Each
// alert carries a link to /settings/providers. Providers and the project
// model-config are PROJECT-scoped in memory (session bootstrap only configured
// the bootstrap project), so a FRESH project under the bootstrap org has
// neither — the error severity is reachable without a scratch org.
//
// An agent WITH an explicit (pinned) model now warns at error severity too
// when the model's provider prefix is unconfigured (or the project has no
// provider at all); only a model whose provider IS configured is alert-free.
// The chat workspace (/chat?agent=<id>) shows the same banner pre-send.
//
// Each test self-cleans: deletes its agent while its scratch project is still
// the session's active project, reactivates the bootstrap project, then
// deletes the scratch project (POST /projects/delete, org-scoped).
const DASH_ERR =
  "No provider or default model is configured — this agent can't run chats yet.";
const SET_ERR =
  'This agent has no model. The project has no configured provider and no default generative model, so chats will fail.';
const DASH_WARN =
  "No project default model is set — the model this agent uses isn't pinned.";
const SET_WARN =
  "This agent has no explicit model and the project has no default generative model, so the exact model isn't pinned. Chats fall back to a configured provider's default.";
const MODEL_NAME = 'deepseek/deepseek-v4-flash';
const DASH_PIN_ERR = `No provider is configured, so this agent's model ${MODEL_NAME} can't run yet. Configure a provider or change the agent's model.`;
const SET_PIN_ERR = `This agent is pinned to ${MODEL_NAME}, but the project has no configured provider — chats will fail until one is added or the model is changed.`;

function requireBootstrap() {
  const bootstrap = readBootstrap();
  expect(bootstrap, 'setup project must run first').toBeTruthy();
  return bootstrap!;
}

async function createAgent(page: Page, name: string, model?: object): Promise<string> {
  const body: Record<string, unknown> = { name, tools: [], skills: [], config: {} };
  if (model) body.model = model;
  const resp = await page.request.post('/api/agents', { data: body });
  expect(resp.ok(), `create agent failed (HTTP ${resp.status()}): ${await resp.text()}`).toBeTruthy();
  return ((await resp.json()) as { id: string }).id;
}

// Deletes the agent while the scratch project is still active, restores the
// session to the bootstrap project, then deletes the scratch project. All
// steps best-effort so repeated runs stay idempotent.
async function cleanup(page: Page, agentId: string, projectId: string): Promise<void> {
  const bootstrap = readBootstrap();
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

// Best-effort provider upsert + a re-read guard. configureProvider only proves
// the gateway ACCEPTED the form (HTTP 200/303 without a save-error body); dev
// memory can still reject the credential probe, leave its catalog unsynced, or
// simply not return the provider on the next project read. The two tests below
// only hold on a project that actually HAS a configured provider (warning
// severity, or no alert for an explicit model whose provider prefix matches),
// so confirm the provider is visible on the same ListProjectProviders-backed
// settings panel the gateway's warning classifier reads, and skip when it is
// not. Returns true once the precondition truly holds.
async function requireConfiguredProvider(page: Page): Promise<boolean> {
  try {
    await configureProvider(page, 'deepseek', 'sk-e2e-test-key');
  } catch (e) {
    test.skip(true, `provider upsert unavailable on dev memory (catalog unsynced): ${(e as Error).message}`);
    return false;
  }
  // A 303 redirect + "Providers saved." flash is not proof the provider
  // survived the credential/catalog check; re-read the project's provider list
  // from the settings panel and skip when the row is absent.
  await page.goto('/settings/providers');
  if ((await page.locator('a[href="/settings/providers/deepseek/edit"]').count()) === 0) {
    test.skip(true, 'dev memory did not retain the deepseek provider — project still has no configured provider');
    return false;
  }
  return true;
}

test.describe('Agent model-config warnings', () => {

test('error severity: no explicit model on a provider-less project', async ({ page }) => {
  const bootstrap = requireBootstrap();
  const name = `E2E Model Warn ${Date.now()}`;
  let agentId = '';
  let projectId = '';

  try {
    projectId = await createProject(page, bootstrap.orgId, name);
    agentId = await createAgent(page, name);
    const title = new RegExp(name);

    // Dashboard: error alert + "Configure a provider" link.
    await page.goto(`/agents/${agentId}`);
    await expectAppPage(page, title);
    const dashAlert = page
      .getByRole('alert')
      .filter({ hasText: 'No provider or default model is configured' });
    await expect(dashAlert).toBeVisible();
    await expect(dashAlert).toHaveClass(/alert-error/);
    await expect(dashAlert).toContainText(DASH_ERR);
    const dashLink = page.getByRole('link', { name: 'Configure a provider' });
    await expect(dashLink).toBeVisible();
    await expect(dashLink).toHaveAttribute('href', '/settings/providers');

    // Settings: settings-flavored error text + the same link.
    await page.goto(`/agents/${agentId}/settings`);
    await expectAppPage(page, title);
    const setAlert = page.getByRole('alert').filter({ hasText: 'This agent has no model' });
    await expect(setAlert).toBeVisible();
    await expect(setAlert).toHaveClass(/alert-error/);
    await expect(setAlert).toContainText(SET_ERR);
    const setLink = page.getByRole('link', { name: 'Configure a provider' });
    await expect(setLink).toBeVisible();
    await expect(setLink).toHaveAttribute('href', '/settings/providers');
  } finally {
    await cleanup(page, agentId, projectId);
  }
});

test('warning severity: provider configured, no default model pinned', async ({ page }) => {
  const bootstrap = requireBootstrap();
  const name = `E2E Model Warn ${Date.now()}`;
  let agentId = '';
  let projectId = '';

  try {
    projectId = await createProject(page, bootstrap.orgId, name);

    // Best-effort provider upsert, then re-read the project's provider list:
    // warning severity (and the no-alert case) only holds when the provider
    // really landed, and dev memory's deepseek catalog is typically unsynced.
    if (!(await requireConfiguredProvider(page))) return;

    agentId = await createAgent(page, name);
    const title = new RegExp(name);

    // Dashboard: warning alert + "Set a default model" link.
    await page.goto(`/agents/${agentId}`);
    await expectAppPage(page, title);
    const dashAlert = page
      .getByRole('alert')
      .filter({ hasText: 'No project default model is set' });
    await expect(dashAlert).toBeVisible();
    await expect(dashAlert).toHaveClass(/alert-warning/);
    await expect(dashAlert).toContainText(DASH_WARN);
    const dashLink = page.getByRole('link', { name: 'Set a default model' });
    await expect(dashLink).toBeVisible();
    await expect(dashLink).toHaveAttribute('href', '/settings/providers');

    // Settings: settings-flavored warning text + the same link.
    await page.goto(`/agents/${agentId}/settings`);
    await expectAppPage(page, title);
    const setAlert = page
      .getByRole('alert')
      .filter({ hasText: 'This agent has no explicit model' });
    await expect(setAlert).toBeVisible();
    await expect(setAlert).toHaveClass(/alert-warning/);
    await expect(setAlert).toContainText(SET_WARN);
    const setLink = page.getByRole('link', { name: 'Set a default model' });
    await expect(setLink).toBeVisible();
    await expect(setLink).toHaveAttribute('href', '/settings/providers');
  } finally {
    await cleanup(page, agentId, projectId);
  }
});

test('error severity: explicit model on a provider-less project', async ({ page }) => {
  const bootstrap = requireBootstrap();
  const name = `E2E Model Warn ${Date.now()}`;
  let agentId = '';
  let projectId = '';

  try {
    projectId = await createProject(page, bootstrap.orgId, name);
    agentId = await createAgent(page, name);

    const explicit = { name: MODEL_NAME, temperature: 0.7, maxTokens: 4096 };

    // Persist the explicit model via the PUT-first fallback pattern (memory
    // model validation / the unsynced catalog can reject unknown names; when
    // the PUT does not stick, recreate the agent WITH the model in the POST
    // body and skip only if neither works).
    const put = await page.request.put(`/api/agents/${agentId}`, {
      data: { name, tools: [], skills: [], config: {}, model: explicit },
    });
    if (!put.ok()) {
      await page.request.delete(`/api/agents/${agentId}`).catch(() => {});
      const created = await page.request.post('/api/agents', {
        data: { name, tools: [], skills: [], config: {}, model: explicit },
      });
      if (!created.ok()) {
        test.skip(
          true,
          `could not persist an explicit model — PUT HTTP ${put.status()}: ${await put.text()}; ` +
            `POST HTTP ${created.status()}: ${await created.text()}`,
        );
        return;
      }
      agentId = ((await created.json()) as { id: string }).id;
    }

    // Verify via the API that the model is actually explicit before asserting
    // on the UI (avoids asserting the alert against a silently dropped model).
    const get = await page.request.get(`/api/agents/${agentId}`);
    expect(get.ok(), `get agent failed (HTTP ${get.status()}): ${await get.text()}`).toBeTruthy();
    const stored = (await get.json()) as { model?: { name?: string } };
    if (!stored.model?.name) {
      test.skip(true, 'memory did not persist the explicit model on the agent');
      return;
    }

    const title = new RegExp(name);

    // Dashboard: error alert naming the pinned model + "Configure a provider" link.
    await page.goto(`/agents/${agentId}`);
    await expectAppPage(page, title);
    const dashAlert = page
      .getByRole('alert')
      .filter({ hasText: `so this agent's model ${MODEL_NAME} can't run yet` });
    await expect(dashAlert).toBeVisible();
    await expect(dashAlert).toHaveClass(/alert-error/);
    await expect(dashAlert).toContainText(DASH_PIN_ERR);
    const dashLink = page.getByRole('link', { name: 'Configure a provider' });
    await expect(dashLink).toBeVisible();
    await expect(dashLink).toHaveAttribute('href', '/settings/providers');

    // Settings: settings-flavored error text ("pinned") + the same link.
    await page.goto(`/agents/${agentId}/settings`);
    await expectAppPage(page, title);
    const setAlert = page.getByRole('alert').filter({ hasText: 'This agent is pinned to' });
    await expect(setAlert).toBeVisible();
    await expect(setAlert).toHaveClass(/alert-error/);
    await expect(setAlert).toContainText(SET_PIN_ERR);
    const setLink = page.getByRole('link', { name: 'Configure a provider' });
    await expect(setLink).toBeVisible();
    await expect(setLink).toHaveAttribute('href', '/settings/providers');
  } finally {
    await cleanup(page, agentId, projectId);
  }
});

test("no alert when the explicit model's provider is configured", async ({ page }) => {
  const bootstrap = requireBootstrap();
  const name = `E2E Model Warn ${Date.now()}`;
  let agentId = '';
  let projectId = '';

  try {
    projectId = await createProject(page, bootstrap.orgId, name);

    // Best-effort provider upsert, then re-read the project's provider list:
    // the no-alert assertion only holds when the explicit model's deepseek
    // provider really landed, and dev memory's catalog is typically unsynced.
    if (!(await requireConfiguredProvider(page))) return;

    agentId = await createAgent(page, name);

    const explicit = { name: MODEL_NAME, temperature: 0.7, maxTokens: 4096 };

    // Persist the explicit model (deepseek/... — prefix matches the configured
    // deepseek provider) via the same PUT-first fallback as the test above.
    const put = await page.request.put(`/api/agents/${agentId}`, {
      data: { name, tools: [], skills: [], config: {}, model: explicit },
    });
    if (!put.ok()) {
      await page.request.delete(`/api/agents/${agentId}`).catch(() => {});
      const created = await page.request.post('/api/agents', {
        data: { name, tools: [], skills: [], config: {}, model: explicit },
      });
      if (!created.ok()) {
        test.skip(
          true,
          `could not persist an explicit model — PUT HTTP ${put.status()}: ${await put.text()}; ` +
            `POST HTTP ${created.status()}: ${await created.text()}`,
        );
        return;
      }
      agentId = ((await created.json()) as { id: string }).id;
    }

    // Verify via the API that the model is actually explicit before asserting
    // on the UI (avoids asserting "no alert" against a silently dropped model).
    const get = await page.request.get(`/api/agents/${agentId}`);
    expect(get.ok(), `get agent failed (HTTP ${get.status()}): ${await get.text()}`).toBeTruthy();
    const stored = (await get.json()) as { model?: { name?: string } };
    if (!stored.model?.name) {
      test.skip(true, 'memory did not persist the explicit model on the agent');
      return;
    }

    const title = new RegExp(name);

    // Dashboard renders no model-config alert (provider prefix is configured).
    await page.goto(`/agents/${agentId}`);
    await expectAppPage(page, title);
    await expect(page.getByRole('alert')).toHaveCount(0);

    // Settings renders no model-config alert and the model select is pinned to
    // the explicit model (catalog match or the "(current)" fallback both give
    // the select the stored model name as its value).
    await page.goto(`/agents/${agentId}/settings`);
    await expectAppPage(page, title);
    await expect(page.getByRole('alert')).toHaveCount(0);
    await expect(page.locator('#agent-settings-model')).toHaveValue(MODEL_NAME);
  } finally {
    await cleanup(page, agentId, projectId);
  }
});

test('chat workspace warns before send when the selected agent model is unservable', async ({ page }) => {
  const bootstrap = requireBootstrap();
  const name = `E2E Model Warn ${Date.now()}`;
  let agentId = '';
  let projectId = '';

  try {
    projectId = await createProject(page, bootstrap.orgId, name);
    agentId = await createAgent(page, name);

    const explicit = { name: MODEL_NAME, temperature: 0.7, maxTokens: 4096 };

    // Persist the explicit model via the PUT-first fallback pattern (memory
    // model validation / the unsynced catalog can reject unknown names; when
    // the PUT does not stick, recreate the agent WITH the model in the POST
    // body and skip only if neither works).
    const put = await page.request.put(`/api/agents/${agentId}`, {
      data: { name, tools: [], skills: [], config: {}, model: explicit },
    });
    if (!put.ok()) {
      await page.request.delete(`/api/agents/${agentId}`).catch(() => {});
      const created = await page.request.post('/api/agents', {
        data: { name, tools: [], skills: [], config: {}, model: explicit },
      });
      if (!created.ok()) {
        test.skip(
          true,
          `could not persist an explicit model — PUT HTTP ${put.status()}: ${await put.text()}; ` +
            `POST HTTP ${created.status()}: ${await created.text()}`,
        );
        return;
      }
      agentId = ((await created.json()) as { id: string }).id;
    }

    // Verify via the API that the model is actually explicit before asserting
    // on the UI (avoids asserting the banner against a silently dropped model).
    const get = await page.request.get(`/api/agents/${agentId}`);
    expect(get.ok(), `get agent failed (HTTP ${get.status()}): ${await get.text()}`).toBeTruthy();
    const stored = (await get.json()) as { model?: { name?: string } };
    if (!stored.model?.name) {
      test.skip(true, 'memory did not persist the explicit model on the agent');
      return;
    }

    // The chat workspace selects the agent via ?agent=<id> and must show the
    // same unservable-model banner the agent dashboard renders, before send.
    await page.goto(`/chat?agent=${agentId}`);
    await expectAppPage(page, /Chat/);
    const banner = page
      .getByRole('alert')
      .filter({ hasText: `so this agent's model ${MODEL_NAME} can't run yet` });
    await expect(banner).toBeVisible();
    await expect(banner).toHaveClass(/alert-error/);
    await expect(banner).toContainText(DASH_PIN_ERR);
    const link = page.getByRole('link', { name: 'Configure a provider' });
    await expect(link).toBeVisible();
    await expect(link).toHaveAttribute('href', '/settings/providers');
  } finally {
    await cleanup(page, agentId, projectId);
  }
});
});
