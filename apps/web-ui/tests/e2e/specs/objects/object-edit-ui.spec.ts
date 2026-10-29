import { test, expect } from '@playwright/test';
import { goto } from '../../helpers/page';
import { addObjectLabel } from '../../helpers/objects';

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
//
// Person declares no workflow (no boardEnabled / allowedStatuses), so the
// built-in workflow-status field must not render for it; this spec also pins
// that absence and persists edits through Key and Labels instead.
async function createPerson(page: import('@playwright/test').Page, key: string): Promise<void> {
  await goto(page, '/objects/new');
  await page.locator('select[name="type"]').selectOption('Person');
  await expect(page.locator('#object-key')).toBeVisible();
  await page.locator('#object-key').fill(key);
  await page.getByRole('button', { name: 'Create object' }).click();
  await page.waitForURL(/\/objects\/[0-9a-f-]{36}$/);
}

test('edit form save persists changes and shows a success toast', async ({ page }) => {
  const key = `e2e-edit-${Date.now()}`;
  const renamed = `${key}-edited`;

  await createPerson(page, key);
  await expect(page.locator('#object-key')).toHaveValue(key);

  // A type with no workflow renders no built-in status control at all.
  await expect(page.locator('#object-status')).toHaveCount(0);
  await expect(page.locator('select[name="status"]')).toHaveCount(0);

  // Edit the key and add a label, then save.
  await page.locator('#object-key').fill(renamed);
  await addObjectLabel(page, 'e2e-edited');
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

  // The submitted values are persisted on the resulting page…
  await expect(page.locator('#object-key')).toHaveValue(renamed);
  await expect(page.locator('.badge', { hasText: 'e2e-edited' }).first()).toBeVisible();

  // …and survive a full reload (server round-trip, not client state).
  await page.reload();
  await expect(page.locator('#object-key')).toHaveValue(renamed);
  await expect(page.locator('.badge', { hasText: 'e2e-edited' }).first()).toBeVisible();
  await expect(page.locator('#object-status')).toHaveCount(0);
});
