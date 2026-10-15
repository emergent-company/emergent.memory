import { test } from '@playwright/test';
import { expectAppPage, goto } from '../../helpers/page';

const routes: Array<[string, RegExp]> = [
  ['/settings', /Project Settings/],
  ['/settings/assistant', /Assistant/],
  ['/settings/overrides', /Agent overrides/],
  ['/settings/voice', /Voice/],
  ['/settings/devices', /Connected devices/],
  ['/settings/approvals', /Approvals/],
  ['/settings/tokens', /API Tokens/],
];

test.describe('Settings surface', () => {
  for (const [route, title] of routes) {
    test(`renders ${route}`, async ({ page }) => {
      await goto(page, route);
      await expectAppPage(page, title);
    });
  }
});
