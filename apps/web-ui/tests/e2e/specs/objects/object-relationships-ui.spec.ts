import { test, expect, type Page } from '@playwright/test';
import { createTypedObject, cleanupObjects } from '../../helpers/objects';

// Object relationships — POST /objects/:id/relationships (uiObjectRelationshipCreate).
//
// Drives the real "Connect object" dialog on an object's detail page: the empty
// state's "Connect this object to another" CTA opens the native <dialog>, the
// relationship-type select is populated from the project's compiled
// relationship types, and the target picker (#connect-dst) autocompletes through
// the /objects/search datalist. Submitting posts to
// POST /objects/:id/relationships and PRG-redirects back to the source object.
//
// The assertion is the resulting state on BOTH pages: the edge is listed in the
// "Relationships" section of the source (Task → Person) and of the target
// (Person, incoming) — never a transient toast.
//
// Cleanup: objects (and the edges touching them) are deleted through the memory
// API with the signed-in user's own Zitadel access token. The gateway exposes no
// object-delete route, so this is the only self-cleanup path; see the run
// report. Every object this spec creates is deleted in `finally`, pass or fail.

const escapeRe = (s: string): string => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

/** The Relationships section on an object detail page (the dialog lives outside it). */
function relationshipsSection(page: Page) {
  return page
    .locator('section')
    .filter({ has: page.getByRole('heading', { name: 'Relationships' }) });
}

test('object relationships: an edge created from the connect dialog is visible from both objects', async ({
  page,
}) => {
  test.setTimeout(120_000);
  const stamp = Date.now();
  const taskKey = `E2E Rel Task ${stamp}`;
  const personKey = `E2E Rel Person ${stamp}`;
  let taskId = '';
  let personId = '';

  try {
    // Two scratch objects. personal-memory's `assigned_to` relationship type is
    // Task → Person, so the Task is the source and the Person the target.
    taskId = await createTypedObject(page, 'Task', taskKey);
    personId = await createTypedObject(page, 'Person', personKey);

    // Open the source object: no edges yet, so the empty state's Connect CTA
    // (the only button with this aria-label) opens the dialog.
    await page.goto(`/objects/${taskId}`);
    const relSection = relationshipsSection(page);
    await expect(relSection.getByText('No relationships')).toBeVisible();

    await page.getByRole('button', { name: 'Connect this object to another' }).click();
    const dialog = page.locator('#object-connect-modal');
    await expect(dialog).toBeVisible();

    // The dialog is pre-seeded with this object as the source.
    await page.locator('#connect-type').selectOption('assigned_to');
    await expect(page.locator('#connect-src')).toHaveValue(taskId);

    // The target picker autocompletes via /objects/search (datalist options
    // carry the object id as value and its label as text).
    await page.locator('#connect-dst').fill(personKey);
    const option = page.locator(`#object-search-options option[value="${personId}"]`);
    await expect(option).toHaveCount(1);
    await expect(option).toHaveText(personKey);

    // A datalist option cannot be "clicked" by Playwright; submitting the id the
    // search surfaced is exactly the value the browser would set on selection.
    await page.locator('#connect-dst').fill(personId);
    await dialog.getByRole('button', { name: 'Connect' }).click();
    await page.waitForURL(new RegExp(`/objects/${escapeRe(taskId)}$`));

    // Resulting state on the SOURCE: the Assigned To edge with both endpoints.
    const sourceRel = relationshipsSection(page);
    await expect(sourceRel).toBeVisible();
    await expect(sourceRel.getByText('Assigned To')).toBeVisible();
    await expect(sourceRel.getByText(personKey, { exact: true })).toBeVisible();
    await expect(sourceRel.getByText(taskKey, { exact: true })).toBeVisible();
    await expect(sourceRel.getByText('No relationships')).toHaveCount(0);

    // Resulting state on the TARGET: the same edge is listed as incoming.
    await page.goto(`/objects/${personId}`);
    const targetRel = relationshipsSection(page);
    await expect(targetRel.getByText('Assigned To')).toBeVisible();
    await expect(targetRel.getByText(taskKey, { exact: true })).toBeVisible();
    await expect(targetRel.getByText(personKey, { exact: true })).toBeVisible();
  } finally {
    await cleanupObjects(page, [taskId, personId]);
  }
});
