import { test, expect } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

test('blueprint detail renders', async ({ page }) => {
  // The seeded `personal-memory` pack is applied; find its blueprint id.
  const installed = (await (
    await page.request.get('/api/blueprints/installed')
  ).json()) as Array<{ blueprintId: string; name: string }>;
  const bp = installed.find((b) => b.name === 'personal-memory');
  expect(bp, 'personal-memory blueprint should be installed').toBeTruthy();

  await page.goto(`/blueprints/${bp!.blueprintId}`);
  await expectAppPage(page, /personal-memory/);
});
