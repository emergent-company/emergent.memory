import { test } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

test.describe('Objects page', () => {
  test('renders under session', async ({ page }) => {
    await page.goto('/objects');
    await expectAppPage(page, /Objects/);
  });

  test('new-object page renders', async ({ page }) => {
    await page.goto('/objects/new');
    await expectAppPage(page, /New object/);
  });
});
