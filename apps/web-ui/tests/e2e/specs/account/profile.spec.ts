import { test } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

test.describe('Profile', () => {
  test('renders under session', async ({ page }) => {
    await page.goto('/profile');
    await expectAppPage(page, /Profile/);
  });
});
