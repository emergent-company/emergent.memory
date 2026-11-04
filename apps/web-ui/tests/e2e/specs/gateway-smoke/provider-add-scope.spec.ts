import { test, expect, type Page } from '@playwright/test';

// Providers settings PRG keeps the URL scope (regression guard for #2479).
//
// A successful provider save answers with a PRG redirect to the context-free
// `/settings/providers?updated=1`. On a scoped page (/o/{org}/p/{project}/…)
// that target must regain the scope prefix on the wire — `scopeLocation`
// rewrites Location / HX-Redirect. Before 7fc46aba6 the rewrite ran after
// `next(c)` returned, but `c.Redirect` had already committed the status line,
// so the browser followed a flat target: the session-mode gateway hard-cuts an
// unscoped page GET and the operator lands on a 404 **after** their provider
// was added (the reported symptom).
//
// The dev-mode harness serves unscoped page GETs, so the 404 itself cannot
// reproduce here — the scope loss that causes it can, and that is exactly what
// this spec pins. It also asserts the redirect landed on the rendered Providers
// page (title + saved row), so a regression that keeps the URL but breaks the
// landing page still fails.
const SCOPE = '/o/o1/p/p1';

const rowFor = (page: Page, slug: string) =>
  page.getByTestId(`provider-instance-${slug}`);

test.describe('Providers settings — scoped PRG', () => {
  test.afterEach(async ({ page }) => {
    // Mock state is per-process; clear the provider so repeated runs and any
    // later spec in this lane start from zero providers.
    await page.request
      .post('/o/o1/p/p1/settings/providers/litellm/remove', { form: {} })
      .catch(() => {});
  });

  test('adding a provider redirects to the scoped Providers page', async ({ page }) => {
    await page.goto(`${SCOPE}/settings/providers/new`);
    await expect(page).toHaveTitle(/Add provider/);

    // The reported flow: the LiteLLM vendor, which declares no default
    // endpoint, so the operator supplies the proxy URL.
    await page.locator('#provider-type').selectOption('litellm');
    await page.locator('input[name="api_key"]').fill('sk-e2e-test-key');
    await page.locator('input[name="base_url"]').fill('http://litellm:4000/v1');
    await page.locator('#provider-save-btn').click();

    await page.waitForURL(/\/settings\/providers\?updated=1/, { timeout: 15_000 });

    // The redirect target must keep the scope: a flat /settings/providers is
    // the bug (404 in session mode).
    const landed = new URL(page.url());
    expect(landed.pathname).toBe(`${SCOPE}/settings/providers`);
    expect(landed.searchParams.get('updated')).toBe('1');

    // …and it must be the real page, not an error page that happens to carry
    // the right URL.
    await expect(page).toHaveTitle(/Providers/);
    await expect(page.getByText('Providers saved.')).toBeVisible();
    const row = rowFor(page, 'litellm');
    await expect(row).toBeVisible();
    await expect(row.getByText('litellm', { exact: true }).first()).toBeVisible();
    await expect(row.getByText('http://litellm:4000/v1')).toBeVisible();
  });
});
