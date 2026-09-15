import { test, expect } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

test.describe('Providers settings', () => {
  test('renders panel with add-provider action', async ({ page }) => {
    await page.goto('/settings/providers');
    await expectAppPage(page, /Providers/);
    // The page has two states: a zero-provider hero ("Connect your first LLM
    // provider" → "Add your first provider") and, once a provider exists, the
    // "Provider configuration" panel → "Add provider". Both expose the same
    // add action, so assert on it in a state-agnostic way.
    await expect(page.locator('a[href="/settings/providers/new"]')).toBeVisible();
    await expect(
      page.getByRole('link', { name: /Add (your first )?provider/i }),
    ).toBeVisible();
  });
});
