import { test } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';
import { readBootstrap } from '../../helpers/bootstrap';

test.describe('Org context pages', () => {
  test('org landing renders', async ({ page }) => {
    const b = readBootstrap();
    await page.goto(`/orgs/${b!.orgId}`);
    // The org landing's <title> is the org name (not a static label).
    await expectAppPage(page, new RegExp(b!.orgName));
  });

  test('org members renders', async ({ page }) => {
    const b = readBootstrap();
    await page.goto(`/orgs/${b!.orgId}/members`);
    await expectAppPage(page, /Members/);
  });

  test('org settings renders', async ({ page }) => {
    const b = readBootstrap();
    await page.goto(`/orgs/${b!.orgId}/settings`);
    await expectAppPage(page, /Settings/);
  });
});
