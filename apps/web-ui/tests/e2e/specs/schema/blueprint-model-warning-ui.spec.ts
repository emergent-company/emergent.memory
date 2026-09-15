import { test, expect, type Page } from '@playwright/test';
import { readBootstrap, createProject, configureProvider } from '../../helpers/bootstrap';
import { expectAppPage } from '../../helpers/page';

// Blueprint-pinned model warnings (gateway unservable-model surface): the
// blueprint details page (/blueprints/<id>) renders agent rows from the
// blueprint's bundled definitions, and an agent whose pinned model can't be
// served by the project warns at error severity. The bundled `operator`
// blueprint pins openai/deepseek-v4-pro, so on a FRESH project (no configured
// provider) /blueprints/operator shows the error alert; once a provider
// matching the model's prefix (openai) is configured, the same page is quiet.
//
// Each test self-cleans: reactivates the bootstrap project, then deletes the
// scratch project (POST /projects/delete, org-scoped). No agent is created,
// so there is nothing else to delete.
const PIN_ERR =
  "This blueprint defines agent operator pinned to openai/deepseek-v4-pro, but no provider is configured in this project. Chats with it will fail until a provider is added or the agent's model is changed in Agent settings.";

function requireBootstrap() {
  const bootstrap = readBootstrap();
  expect(bootstrap, 'setup project must run first').toBeTruthy();
  return bootstrap!;
}

// Restores the session to the bootstrap project, then deletes the scratch
// project. All steps best-effort so repeated runs stay idempotent.
async function cleanup(page: Page, projectId: string): Promise<void> {
  const bootstrap = readBootstrap();
  if (bootstrap?.projectId) {
    await page.request.post(`/api/projects/${bootstrap.projectId}/activate`).catch(() => {});
  }
  if (projectId && bootstrap?.orgId) {
    await page.request
      .post(`/projects/delete?projectId=${projectId}&orgId=${bootstrap.orgId}`)
      .catch(() => {});
  }
}

test("bundled operator agent warns when its pinned model's provider is unconfigured", async ({ page }) => {
  const bootstrap = requireBootstrap();
  const name = `E2E BP Model Warn ${Date.now()}`;
  let projectId = '';

  try {
    projectId = await createProject(page, bootstrap.orgId, name);

    // No provider configured on the scratch project → the operator agent's
    // pinned openai/deepseek-v4-pro model can't be served.
    await page.goto('/blueprints/operator');
    await expectAppPage(page, /operator/);

    const alert = page
      .getByRole('alert')
      .filter({ hasText: 'This blueprint defines agent operator pinned to openai/deepseek-v4-pro' });
    await expect(alert).toBeVisible();
    await expect(alert).toHaveClass(/alert-error/);
    await expect(alert).toContainText(PIN_ERR);
    const link = page.getByRole('link', { name: 'Configure a provider' });
    await expect(link).toBeVisible();
    await expect(link).toHaveAttribute('href', '/settings/providers');
  } finally {
    await cleanup(page, projectId);
  }
});

test("no warning when the pinned model's provider is configured", async ({ page }) => {
  const bootstrap = requireBootstrap();
  const name = `E2E BP Model Warn ${Date.now()}`;
  let projectId = '';

  try {
    projectId = await createProject(page, bootstrap.orgId, name);

    // Best-effort provider upsert, mirroring the suite's provider precedent:
    // the openai upsert is env-fragile (credential probe / catalog), so skip
    // when the save fails rather than asserting against an unconfigured state.
    try {
      await configureProvider(page, 'openai', 'sk-e2e-test-key');
    } catch (e) {
      test.skip(true, `openai provider upsert unavailable on dev memory: ${(e as Error).message}`);
      return;
    }

    // The openai provider matches the operator agent's pinned model prefix, so
    // the blueprint details page must not flag it.
    await page.goto('/blueprints/operator');
    await expectAppPage(page, /operator/);
    await expect(
      page.getByRole('alert').filter({ hasText: 'This blueprint defines agent operator' }),
    ).toHaveCount(0);
  } finally {
    await cleanup(page, projectId);
  }
});
