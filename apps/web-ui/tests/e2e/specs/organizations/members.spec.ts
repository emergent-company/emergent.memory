import { test } from '@playwright/test';
import { expectAppPage, goto } from '../../helpers/page';

test.describe('Members', () => {
  test('renders under session', async ({ page }) => {
    await goto(page, '/members');
    await expectAppPage(page, /Members/);
  });
});
