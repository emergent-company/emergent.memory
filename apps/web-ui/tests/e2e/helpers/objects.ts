import { Page, expect } from '@playwright/test';

// uiObjectCreate redirects to the new object's detail page (/objects/:id).
const OBJECT_DETAIL_URL = /\/objects\/[0-9a-f-]{36}/;

/**
 * Open /objects/new, pick the object type, and wait for the details form
 * (key + fields) to render.
 */
export async function openObjectForm(page: Page, type: string): Promise<void> {
  await page.goto('/objects/new');
  await page.locator('select[name="type"]').selectOption(type);
  await expect(page.locator('#object-key')).toBeVisible();
}

/** Type a label and press Enter — the TagList renders a chip + hidden input. */
export async function addObjectLabel(page: Page, label: string): Promise<void> {
  await page.locator('#object-labels').fill(label);
  await page.locator('#object-labels').press('Enter');
}

/**
 * Click "Create object" and wait for the redirect to the new object's detail
 * page; return the created object's id parsed from the URL.
 */
export async function submitObjectForm(page: Page): Promise<string> {
  await page.getByRole('button', { name: 'Create object' }).click();
  await page.waitForURL(OBJECT_DETAIL_URL);
  const id = new URL(page.url()).pathname.split('/').pop();
  if (!id) {
    throw new Error(`submitObjectForm: no id in the detail URL "${page.url()}"`);
  }
  return id;
}

/**
 * Create an object through the real form: pick the type, fill the key, add
 * each label as a chip, submit, and return the created object's id.
 */
export async function createObjectViaForm(
  page: Page,
  type: string,
  key: string,
  labels: string[],
): Promise<string> {
  await openObjectForm(page, type);
  await page.locator('#object-key').fill(key);
  for (const label of labels) {
    await addObjectLabel(page, label);
  }
  return submitObjectForm(page);
}
