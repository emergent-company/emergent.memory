import { test } from '@playwright/test';
import { expectAppPage, goto } from '../../helpers/page';

test.describe('Objects page', () => {
  test('renders under session', async ({ page }) => {
    await goto(page, '/objects');
    await expectAppPage(page, /Objects/);
  });

  test('new-object page renders', async ({ page }) => {
    await goto(page, '/objects/new');
    await expectAppPage(page, /New object/);
  });
});
