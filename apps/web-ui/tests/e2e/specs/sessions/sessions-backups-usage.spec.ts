import { test } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

const routes: Array<[string, RegExp]> = [
  ['/sessions', /Sessions/],
  ['/backups', /Backups/],
  ['/usage', /Usage/],
];

test.describe('Sessions + Backups + Usage surface', () => {
  for (const [route, title] of routes) {
    test(`renders ${route}`, async ({ page }) => {
      await page.goto(route);
      await expectAppPage(page, title);
    });
  }
});
