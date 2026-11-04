import { test } from '@playwright/test';
import { expectAppPage, goto } from '../../helpers/page';

const routes: Array<[string, RegExp]> = [
  ['/schema', /Schema/],
  ['/blueprints', /Blueprints/],
  ['/blueprints/migrations', /Migrations/],
];

test.describe('Schema + Blueprints surface', () => {
  for (const [route, title] of routes) {
    test(`renders ${route}`, async ({ page }) => {
      await goto(page, route);
      await expectAppPage(page, title);
    });
  }
});
