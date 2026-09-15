import { test, expect, type Page } from '@playwright/test';
import { readBootstrap, createProject } from '../../helpers/bootstrap';

// The assistant agent selection is shell chrome: choosing an agent gates the
// topbar "Assistant" button (and the sidepanel), clearing it hides them. Both
// live OUTSIDE #main-content, so the inline save forces a full page load
// (render.RedirectAfterMutation) — a no-swap toast would leave the button stale
// until a manual refresh. This spec asserts the button appears (and disappears)
// immediately after the select change, never calling page.reload().
//
// The setting is PROJECT-scoped, and the assistant select only offers the active
// project's agents, so each test creates a fresh scratch project + one agent,
// drives the UI, then self-cleans (delete agent → reactivate bootstrap project →
// delete scratch project).

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

test('assistant button appears after setting the agent — no manual reload', async ({ page }) => {
  const bootstrap = requireBootstrap();
  const name = `E2E Assistant ${Date.now()}`;
  let agentId = '';
  let projectId = '';

  try {
    projectId = await createProject(page, bootstrap.orgId, name);
    agentId = await createAgent(page, name);

    const toggle = page.locator('[data-testid="sidepanel-toggle"]');

    // Initial: a fresh project has no assistant configured → no topbar button.
    await page.goto('/settings/assistant');
    await expect(page).not.toHaveURL(/\/auth\/login/);
    await expect(page.locator('#assistant-agent')).toBeVisible();
    await expect(toggle).toHaveCount(0);

    // Set the assistant. The inline hx-post full-loads the shell, so the button
    // appears on the reloaded page without any manual refresh.
    await page.locator('#assistant-agent').selectOption(agentId);
    await expect(page).toHaveURL(/\/settings\/assistant\?updated=1/);
    await expect(toggle).toBeVisible();

    // Clear the assistant (None) → the button is gone again, same full-load path.
    await page.locator('#assistant-agent').selectOption('');
    await expect(page).toHaveURL(/\/settings\/assistant\?updated=1/);
    await expect(toggle).toHaveCount(0);
  } finally {
    await cleanup(page, agentId, projectId);
  }
});
