import { test } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

const routes: Array<[string, RegExp]> = [
  ['/members/new', /Invite a member/],
  ['/profile/invitations', /Invitations/],
  ['/profile/tokens', /Account API tokens/],
];

test.describe('Members + Profile sub-pages', () => {
  for (const [route, title] of routes) {
    test(`renders ${route}`, async ({ page }) => {
      await page.goto(route);
      await expectAppPage(page, title);
    });
  }
});
