import { test } from '@playwright/test';
import { expectAppPage, goto } from '../../helpers/page';

test.describe('Profile', () => {
  test('renders under session', async ({ page }) => {
    await goto(page, '/profile');
    await expectAppPage(page, /Profile/);
  });
});
