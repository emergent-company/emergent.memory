import { test } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

const routes: Array<[string, RegExp]> = [
  ['/schema', /Schema/],
  ['/blueprints', /Blueprints/],
  ['/blueprints/migrations', /Migrations/],
];

test.describe('Schema + Blueprints surface', () => {
  for (const [route, title] of routes) {
    test(`renders ${route}`, async ({ page }) => {
      await page.goto(route);
      await expectAppPage(page, title);
    });
  }
});
