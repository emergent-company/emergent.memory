import { test, expect } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

test.describe('Documents page', () => {
  test('renders with upload form', async ({ page }) => {
    await page.goto('/documents');
    await expectAppPage(page, /Documents/);
    await expect(page.locator('input[type="file"][name="file"]')).toBeVisible();
  });
});
