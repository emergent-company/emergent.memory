import { test, expect, type Page } from '@playwright/test';
import { goto } from '../../helpers/page';
import {
  readBootstrap,
  createProject,
  configureLiveProvider,
  hasLiveProviderCreds,
} from '../../helpers/bootstrap';

// Blueprint-pinned model review (gateway install surface): installing a
// blueprint whose manifest agents pin a model now renders a model-review step
// BEFORE applying (one row per pinned agent) instead of applying directly. The
// built-in `operator` blueprint pins openai/deepseek-v4-pro, so on a FRESH
// project (no configured provider) its install lands on the review page with an
// unresolved-model note; submitting the review applies and lands on
// /blueprints?installed=1. Once an openai provider is configured, the review
// pre-fills the pinned model with no note. Blueprints that pin no models
// (e.g. agent-notes) still install directly.
//
// Each test self-cleans: reactivates the bootstrap project, then deletes the
// scratch project (POST /projects/delete, org-scoped).

const OPERATOR = 'operator';
const PINNED_MODEL = 'openai/deepseek-v4-pro';

function requireBootstrap() {
  const bootstrap = readBootstrap();
  expect(bootstrap, 'setup project must run first').toBeTruthy();
  return bootstrap!;
}

/** Click the operator blueprint's install action and wait for the review step. */
async function openOperatorReview(page: Page): Promise<void> {
  await goto(page, '/blueprints');
  const row = page.locator(`[data-testid="available-blueprint-${OPERATOR}"]`);
  await expect(row).toBeVisible();
  await row.locator(`[data-testid="blueprint-install-${OPERATOR}"]`).click();
  await expect(page.locator('[data-testid="blueprint-import-map"]')).toBeVisible();
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

test('installing a pinned blueprint shows the model-review step, then applies on confirm', async ({
  page,
}) => {
  const bootstrap = requireBootstrap();
  const name = `E2E BP Model Review ${Date.now()}`;
  let projectId = '';

  try {
    projectId = await createProject(page, bootstrap.orgId, name);

    await openOperatorReview(page);

    // The review form posts to the install-confirm route and carries the
    // blueprint id through a hidden input.
    const form = page.locator('[data-testid="blueprint-import-map"]');
    await expect(form).toHaveAttribute('action', /\/blueprints\/install\/confirm$/);
    await expect(form.locator('input[name="id"]')).toHaveAttribute('value', /.+/);

    // One row per pinned agent, with the neutral "Blueprint: <model>" badge and
    // an unresolved-model note (no provider configured) linking to providers.
    const row = page.locator('[data-testid="blueprint-model-map-row"]');
    await expect(row).toHaveCount(1);
    await expect(row).toContainText(OPERATOR);
    await expect(row).toContainText(`Blueprint: ${PINNED_MODEL}`);
    await expect(row.locator('[data-testid="blueprint-model-map-select"]')).toHaveAttribute(
      'name',
      'model.operator',
    );
    await expect(row.locator('a[href$="/settings/providers"]')).toBeVisible();

    // Submitting the review (project default) applies and lands on the
    // installed flash.
    await page.locator('[data-testid="blueprint-model-map-submit"]').click();
    await page.waitForURL(
      (u) => u.pathname.endsWith('/blueprints') && u.searchParams.get('installed') === '1',
    );
  } finally {
    await cleanup(page, projectId);
  }
});

test('review pre-fills the pinned model when its provider is configured', async ({ page }) => {
  const bootstrap = requireBootstrap();
  const name = `E2E BP Model Review Prefill ${Date.now()}`;
  let projectId = '';

  try {
    projectId = await createProject(page, bootstrap.orgId, name);

    // Live-validated provider upsert (see blueprint-model-warning-ui.spec.ts):
    // the backend live-tests credentials on save, so skip when env creds are
    // unset or the save fails.
    if (!hasLiveProviderCreds()) {
      test.skip(
        true,
        'E2E_SCENARIO_LLM_API_KEY is not set — the openai provider save is live-validated by the backend',
      );
      return;
    }
    try {
      await configureLiveProvider(page);
    } catch (e) {
      test.skip(true, `openai provider upsert unavailable on dev memory: ${(e as Error).message}`);
      return;
    }

    await openOperatorReview(page);

    // The openai provider matches the pinned model's prefix, so the row has no
    // unresolved note and the select pre-fills the pinned model.
    const row = page.locator('[data-testid="blueprint-model-map-row"]');
    await expect(row).toHaveCount(1);
    await expect(row.locator('a[href$="/settings/providers"]')).toHaveCount(0);
    await expect(row.locator('[data-testid="blueprint-model-map-select"]')).toHaveValue(PINNED_MODEL);
  } finally {
    await cleanup(page, projectId);
  }
});
