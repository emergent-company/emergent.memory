import { test } from '@playwright/test';
import { expectAppPage, goto } from '../../helpers/page';

test('schema object-type detail renders', async ({ page }) => {
  // `personal-memory` (seeded in setup) defines a `Person` type.
  await goto(page, '/schema/object-types/Person');
  await expectAppPage(page, /Person/);
});
