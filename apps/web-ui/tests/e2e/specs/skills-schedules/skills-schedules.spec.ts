import { test } from '@playwright/test';
import { expectAppPage, goto } from '../../helpers/page';

const routes: Array<[string, RegExp]> = [
  ['/skills', /Skills/],
  ['/skills/new', /New skill/],
  ['/schedules', /Schedules/],
  ['/schedules/new', /New schedule/],
];

test.describe('Skills + Schedules surface', () => {
  for (const [route, title] of routes) {
    test(`renders ${route}`, async ({ page }) => {
      await goto(page, route);
      await expectAppPage(page, title);
    });
  }
});
