import { test, expect, type Page } from '@playwright/test';
import { readBootstrap, createProject } from '../../helpers/bootstrap';
import { expectAppPage, goto } from '../../helpers/page';

// Blueprint-pinned model warnings: the blueprint DETAIL page (/blueprints/<id>)
// is a read-only preview of the manifest, so it must never flag a pinned model
// whose provider isn't configured in this project (#1955) — that notice belongs
// to the install-time model-review step (see blueprint-model-review-ui.spec.ts),
// and to an installed agent's own dashboard/model settings. The built-in
// `operator` blueprint pins openai/deepseek-v4-pro, which makes it the sharpest
// probe: on a FRESH project (no provider at all) its detail page must still be
// quiet while rendering the agent row.
//
// Each test self-cleans: reactivates the bootstrap project, then deletes the
// scratch project (POST /projects/delete, org-scoped). No agent is created, so
// there is nothing else to delete.
const OPERATOR = 'operator';

type Scope = { orgId: string; projectId: string };

/**
 * Open the operator blueprint's detail page via its Available-list link, under
 * the scratch project's explicit scope (#2033).
 */
async function openOperatorDetail(page: Page, scope: Scope): Promise<void> {
  await goto(page, '/blueprints', scope);
  const row = page.locator(`[data-testid="available-blueprint-${OPERATOR}"]`);
  await expect(row).toBeVisible();
  await row.locator('a[href*="/blueprints/"]').click();
  await expectAppPage(page, /operator/);
}

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

test('blueprint detail preview never warns about an unconfigured provider', async ({ page }) => {
  const bootstrap = requireBootstrap();
  const name = `E2E BP Model Warn ${Date.now()}`;
  let projectId = '';

  try {
    projectId = await createProject(page, bootstrap.orgId, name);
    const scope = { orgId: bootstrap.orgId, projectId };

    // No provider is configured on the scratch project, yet the preview renders
    // the operator agent row (its pinned model badge included) without any
    // availability warning.
    await openOperatorDetail(page, scope);
    await expect(page.getByText('openai/deepseek-v4-pro')).toBeVisible();

    await expect(
      page.getByRole('alert').filter({ hasText: 'This blueprint defines agent' }),
    ).toHaveCount(0);
    await expect(page.getByRole('link', { name: 'Configure a provider' })).toHaveCount(0);
  } finally {
    await cleanup(page, projectId);
  }
});
