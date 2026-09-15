import { test } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

test.describe('Members', () => {
  test('renders under session', async ({ page }) => {
    await page.goto('/members');
    await expectAppPage(page, /Members/);
  });
});
