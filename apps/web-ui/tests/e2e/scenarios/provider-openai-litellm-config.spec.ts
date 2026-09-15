import { test, expect, type Page } from '@playwright/test';
import { readBootstrap, createProject } from '../helpers/bootstrap';
import { expectAppPage } from '../helpers/page';

// OpenAI-compatible provider scenario: configure the "openai" provider type
// against a LiteLLM proxy from a FRESH project, via the settings UI.
//
// The gateway treats "openai" as the OpenAI-compatible provider slot (the one
// with a Base-URL field); pointing it at a LiteLLM proxy is the documented dev
// setup. Flow:
//   1. SEED (API): create a fresh scratch project under the bootstrap org
//      (provider config is project-scoped, so a fresh project keeps the save
//      from touching the bootstrap project's provider);
//   2. NAVIGATE (UI): /settings/providers → "Add your first provider" CTA;
//   3. CONFIGURE (UI): pick provider `openai`, which reveals the Base-URL
//      field; type the API key while asserting the password show/hide toggle
//      flips input[type] between password and text (and back);
//   4. SAVE (UI): the save is live-validated by the memory backend (catalog
//      sync + a real generate test against the submitted base_url + key), so
//      a real LiteLLM URL + key are required. When dev memory rejects the
//      credentials the test skips with the backend's copy — it does NOT fail,
//      mirroring the skip precedent in scenarios/blueprint-object-chat.
//   5. VERIFY (UI): PRG redirect → the provider row on /settings/providers
//      shows `openai` with the LiteLLM base URL.
// Cleanup mirrors the blueprint-object-chat scenario: remove the provider
// config, reactivate the bootstrap project, delete the scratch project — all
// best-effort so repeated runs and mid-journey skips stay idempotent.
//
// Env vars (see tests/e2e/.env.e2e.example):
//   E2E_OPENAI_BASE_URL  LiteLLM base URL (default 'http://litellm:4000/v1' —
//                        the documented dev installation's proxy endpoint)
//   E2E_OPENAI_API_KEY   real LiteLLM key; no default — the scenario skips
//                        when unset
const LITELLM_BASE_URL = process.env.E2E_OPENAI_BASE_URL || 'http://litellm:4000/v1';
const LITELLM_API_KEY = process.env.E2E_OPENAI_API_KEY || '';

function requireBootstrap() {
  const bootstrap = readBootstrap();
  expect(bootstrap, 'setup project must run first').toBeTruthy();
  return bootstrap!;
}

// The sidebar "Project" (/settings) nav row flags a project with zero configured
// LLM providers with a warning icon (sidebar_user.templ). It lives in the shell
// OUTSIDE #main-content, so it only updates when the provider save forces a full
// page load (render.RedirectAfterMutation). This test never reloads manually —
// asserting the icon clears right after the save is the regression guard: a
// boosted 303 / no-swap save would leave the shell (and icon) stale.
function sidebarProviderWarning(page: Page) {
  return page
    .locator('#_layout-sidebar a[href="/settings"]')
    .locator('[role="img"][aria-label*="No provider configured"]');
}

// All steps best-effort: idempotent across repeated runs, skips, and failures.
async function cleanup(page: Page, projectId: string): Promise<void> {
  const bootstrap = readBootstrap();
  // Provider configs are project-scoped; removing first keeps the scratch
  // project's provider from lingering if the project delete is interrupted.
  await page.request
    .post('/settings/providers/openai/remove', { form: {} })
    .catch(() => {});
  if (bootstrap?.projectId) {
    await page.request.post(`/api/projects/${bootstrap.projectId}/activate`).catch(() => {});
  }
  if (projectId && bootstrap?.orgId) {
    await page.request
      .post(`/projects/delete?projectId=${projectId}&orgId=${bootstrap.orgId}`)
      .catch(() => {});
  }
}

test.describe('OpenAI-compatible provider via LiteLLM', () => {
  test('fresh project → add provider → API-key show/hide → saved with base URL', async ({
    page,
  }) => {
    // The memory backend live-tests the credentials on save (real generate
    // call against base_url), so this only completes when a real LiteLLM is
    // reachable from it. Without a key there is nothing to validate against.
    test.skip(
      !LITELLM_API_KEY,
      'E2E_OPENAI_API_KEY is not set — the openai provider save is live-validated ' +
        'by the memory backend against the LiteLLM base URL. Set it in ' +
        'tests/e2e/.env.e2e to run this scenario (defaults assume ' +
        `${LITELLM_BASE_URL}).`,
    );
    test.setTimeout(180_000);

    const bootstrap = requireBootstrap();
    const name = `E2E OpenAI provider ${Date.now()}`;
    let projectId = '';

    try {
      // 1. SEED (API): fresh project isolates the provider config; createProject
      // also activates it for the session.
      projectId = await createProject(page, bootstrap.orgId, name);

      // 2. NAVIGATE (UI): a fresh project has zero providers, so the Providers
      // page shows the hero CTA instead of the management panel — and the shell
      // sidebar carries the zero-provider warning icon (initial state).
      await page.goto('/settings/providers');
      await expectAppPage(page, /Providers/);
      await expect(sidebarProviderWarning(page)).toBeVisible();
      await page.getByRole('link', { name: 'Add your first provider' }).click();
      await page.waitForURL(/\/settings\/providers\/new$/);

      // 3. CONFIGURE (UI): the add form renders credentials-only. Selecting the
      // "openai" provider type reveals the Base URL (OpenAI-compatible) field.
      await expect(page.locator('#provider-type')).toBeVisible();
      const keyInput = page.locator('input[name="api_key"]');
      const baseUrlField = page.locator('#base-url-field');
      const baseUrlInput = page.locator('input[name="base_url"]');
      const saveBtn = page.locator('#provider-save-btn');
      await expect(keyInput).toBeVisible();
      await expect(baseUrlField).toBeHidden();
      await expect(saveBtn).toBeVisible();

      await page.locator('#provider-type').selectOption('openai');
      await expect(baseUrlField).toBeVisible();
      await expect(page.getByText('Base URL (OpenAI-compatible)')).toBeVisible();

      // The API key is typed as a masked password field with a show/hide
      // toggle next to it. Verify the toggle flips the input between
      // password and plaintext while the value is being entered.
      const toggle = page.getByRole('button', { name: 'Toggle password visibility' });
      await expect(toggle).toBeVisible();
      await expect(keyInput).toHaveAttribute('type', 'password');

      await keyInput.fill(LITELLM_API_KEY);
      await expect(keyInput).toHaveValue(LITELLM_API_KEY);

      await toggle.click(); // reveal
      await expect(keyInput).toHaveAttribute('type', 'text');
      await expect(keyInput).toHaveValue(LITELLM_API_KEY);
      await expect(toggle.locator('.iconify')).toHaveClass(/lucide--eye-off/);

      await toggle.click(); // mask again
      await expect(keyInput).toHaveAttribute('type', 'password');
      await expect(keyInput).toHaveValue(LITELLM_API_KEY);
      await expect(toggle.locator('.iconify')).toHaveClass(/lucide--eye/);

      // The advisory reachability check under the base-URL field fires only
      // after a debounce and never gates Save (add mode) — leave it alone and
      // just point the provider at the LiteLLM proxy.
      await baseUrlInput.fill(LITELLM_BASE_URL);

      // 4. SAVE (UI): success PRG-redirects to /settings/providers?updated=1;
      // a rejection re-renders the form with the "Couldn't save provider"
      // modal carrying the backend's reason — treat that as an environment
      // problem (unreachable proxy or invalid key), not a product regression.
      await saveBtn.click();
      await Promise.race([
        page.waitForURL(/\/settings\/providers\?updated=1/, { timeout: 90_000 }),
        page
          .getByText(/couldn't save provider|could not save provider/i)
          .first()
          .waitFor({ state: 'visible', timeout: 90_000 }),
      ]);

      if (!page.url().includes('updated=1')) {
        let detail = "couldn't save provider";
        const modal = page.locator('#provider-save-error-modal');
        if (await modal.isVisible().catch(() => false)) {
          const reason = await modal.locator('p').last().textContent().catch(() => null);
          if (reason?.trim()) detail = reason.trim();
        }
        test.skip(
          true,
          `openai provider save rejected by memory backend for ${LITELLM_BASE_URL}: ${detail}`,
        );
        return;
      }

      // 5. VERIFY (UI): the provider is now configured, so the shell sidebar
      // warning icon has cleared — asserted WITHOUT a manual reload, since the
      // save's HX-Redirect full-loads the shell (the regression this covers).
      await expect(sidebarProviderWarning(page)).toHaveCount(0);

      // The provider row under the "Provider configuration"
      // heading shows the `openai` slug and the LiteLLM base URL it was
      // configured with, plus the Edit action. The row is the rounded card
      // that also carries the base-URL text, so scope on that card.
      await expectAppPage(page, /Providers/);
      await expect(
        page.getByRole('heading', { name: 'Provider configuration' }),
      ).toBeVisible();
      const baseUrlText = page.getByText(LITELLM_BASE_URL);
      const row = baseUrlText.locator(
        'xpath=ancestor::div[contains(concat(" ", normalize-space(@class), " "), " rounded-box ")][1]',
      );
      await expect(baseUrlText).toBeVisible();
      await expect(row.getByText('openai', { exact: true })).toBeVisible();
      await expect(row.getByRole('link', { name: 'Edit' })).toHaveAttribute(
        'href',
        '/settings/providers/openai/edit',
      );
    } finally {
      await cleanup(page, projectId);
    }
  });
});
