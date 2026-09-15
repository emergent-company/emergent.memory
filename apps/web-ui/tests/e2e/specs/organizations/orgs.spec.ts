import { test, expect } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';
import { readBootstrap } from '../../helpers/bootstrap';

test.describe('Organizations', () => {
  test('lists the bootstrap org', async ({ page }) => {
    const bootstrap = readBootstrap();
    expect(bootstrap?.orgName, 'bootstrap org must exist').toBeTruthy();

    await page.goto('/orgs');
    await expectAppPage(page, /Organizations/);
    // The org name appears in the project switcher, an <option>, and the list;
    // scope to the main content to assert the org list entry specifically.
    await expect(page.locator('main').getByText(bootstrap!.orgName).first()).toBeVisible();
  });
});
