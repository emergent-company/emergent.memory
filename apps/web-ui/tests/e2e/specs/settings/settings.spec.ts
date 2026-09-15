import { test } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

const routes: Array<[string, RegExp]> = [
  ['/settings', /Project Settings/],
  ['/settings/assistant', /Assistant/],
  ['/settings/overrides', /Agent overrides/],
  ['/settings/voice', /Voice/],
  ['/settings/devices', /Devices/],
  ['/settings/approvals', /Approvals/],
  ['/settings/tokens', /API Tokens/],
];

test.describe('Settings surface', () => {
  for (const [route, title] of routes) {
    test(`renders ${route}`, async ({ page }) => {
      await page.goto(route);
      await expectAppPage(page, title);
    });
  }
});
