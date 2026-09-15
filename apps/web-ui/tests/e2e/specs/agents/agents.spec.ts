import { test, expect } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

test.describe('Agents page', () => {
  test('renders under session', async ({ page }) => {
    await page.goto('/agents');
    await expectAppPage(page, /Agents/);
    await expect(page.getByRole('link', { name: 'Agents', exact: true })).toBeVisible();
    // Stable data-testid anchor (derived from the page title).
    await expect(page.getByTestId('page-agents')).toBeVisible();
  });

  test('sidebar nav crosses to Chat and back', async ({ page }) => {
    await page.goto('/agents');
    await expect(page).toHaveTitle(/Agents/);
    // Sidebar nav is HTMX (swaps #main-content, pushes the URL) — the <title>
    // is not updated, so assert on the URL, not the title.
    await page.getByRole('link', { name: 'Chat', exact: true }).click();
    await expect(page).toHaveURL(/\/chat$/);
    await page.getByRole('link', { name: 'Agents', exact: true }).click();
    await expect(page).toHaveURL(/\/agents$/);
  });
});
