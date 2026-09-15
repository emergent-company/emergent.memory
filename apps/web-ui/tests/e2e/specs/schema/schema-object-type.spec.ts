import { test } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

test('schema object-type detail renders', async ({ page }) => {
  // `personal-memory` (seeded in setup) defines a `Person` type.
  await page.goto('/schema/object-types/Person');
  await expectAppPage(page, /Person/);
});
