import { test, expect } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';
import { openObjectForm, addObjectLabel, submitObjectForm, createObjectViaForm } from '../../helpers/objects';

test.describe('Object create', () => {

test('creates an object with labels via the form', async ({ page }) => {
  const key = `e2e-obj-${Date.now()}`;

  await openObjectForm(page, 'Person');
  await page.locator('#object-key').fill(key);
  await addObjectLabel(page, 'e2e-tag-a');
  await addObjectLabel(page, 'e2e-tag-b');
  await expect(page.locator('.badge', { hasText: 'e2e-tag-a' }).first()).toBeVisible();
  await expect(page.locator('.badge', { hasText: 'e2e-tag-b' }).first()).toBeVisible();

  // uiObjectCreate redirects to the new object's detail page; labels persist.
  // submitObjectForm already waits for that redirect, so assert the detail
  // page content once it returns.
  await submitObjectForm(page);
  await expectAppPage(page, new RegExp(key));
  await expect(page.locator('.badge', { hasText: 'e2e-tag-a' }).first()).toBeVisible();
  await expect(page.locator('.badge', { hasText: 'e2e-tag-b' }).first()).toBeVisible();
});

test('reuses an existing label via the suggestion dropdown', async ({ page }) => {
  const label = `e2e-tag-reuse-${Date.now()}`;

  // Seed: create one object carrying a unique label, so it exists in the graph.
  await createObjectViaForm(page, 'Person', `e2e-seed-${Date.now()}`, [label]);

  // Create a second object, reusing the label via the suggestion dropdown.
  await openObjectForm(page, 'Person');
  await page.locator('#object-key').fill(`e2e-reuse-${Date.now()}`);
  await page.locator('#object-labels').fill('e2e-tag-reuse'); // partial prefix
  await page.getByRole('option', { name: label }).click();
  await expect(page.locator('.badge', { hasText: label }).first()).toBeVisible();

  await submitObjectForm(page);
  await expect(page.locator('.badge', { hasText: label }).first()).toBeVisible();
});
});
