import { test, expect } from '@playwright/test';

// Regression coverage for the object detail edit form. The gateway shell is
// hx-boosted (SPA-style in-page navigation), so assertions must target the URL
// and page content — document.title intentionally stays stale on boosted
// navigations (that is a known shell limitation, tracked separately).
//
// What this guards: saving changes must (a) redirect with the ?updated=1 PRG
// param, (b) flash a "Object updated." success toast (delivered via the
// HX-Trigger header under hx-boost), and (c) actually persist the edit so it
// survives a full reload. Before the fix, an errored save redirected silently
// with no toast and no saved data — indistinguishable from a no-op.
async function createPerson(page: import('@playwright/test').Page, key: string): Promise<void> {
  await page.goto('/objects/new');
  await page.locator('select[name="type"]').selectOption('Person');
  await expect(page.locator('#object-key')).toBeVisible();
  await page.locator('#object-key').fill(key);
  await page.getByRole('button', { name: 'Create object' }).click();
  await page.waitForURL(/\/objects\/[0-9a-f-]{36}$/);
}

test('edit form save persists changes and shows a success toast', async ({ page }) => {
  const key = `e2e-edit-${Date.now()}`;
  const status = `reviewed-${Date.now()}`;

  await createPerson(page, key);
  await expect(page.locator('#object-key')).toHaveValue(key);

  // Edit the Status field and save.
  await page.locator('#object-status').fill(status);
  await page.getByRole('button', { name: 'Save changes' }).click();

  // PRG: the boosted POST redirects to the detail page with ?updated=1 (the
  // graph store is versioned, so the object id may differ between renders).
  await page.waitForURL(/\/objects\/[0-9a-f-]{36}\?updated=1/);

  // Success toast surfaces (auto-dismisses after ~4s, so poll immediately).
  await expect
    .poll(async () => (await page.locator('body').innerText()).includes('Object updated.'), {
      timeout: 8000,
      intervals: [100, 200, 200, 500],
    })
    .toBeTruthy();

  // The submitted value is persisted on the resulting page…
  await expect(page.locator('#object-status')).toHaveValue(status);
  await expect(page.locator('#object-key')).toHaveValue(key);

  // …and survives a full reload (server round-trip, not client state).
  await page.reload();
  await expect(page.locator('#object-status')).toHaveValue(status);
  await expect(page.locator('#object-key')).toHaveValue(key);
});
