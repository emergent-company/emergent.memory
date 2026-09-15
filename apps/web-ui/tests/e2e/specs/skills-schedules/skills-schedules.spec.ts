import { test } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

const routes: Array<[string, RegExp]> = [
  ['/skills', /Skills/],
  ['/skills/new', /New skill/],
  ['/schedules', /Schedules/],
  ['/schedules/new', /New schedule/],
];

test.describe('Skills + Schedules surface', () => {
  for (const [route, title] of routes) {
    test(`renders ${route}`, async ({ page }) => {
      await page.goto(route);
      await expectAppPage(page, title);
    });
  }
});
