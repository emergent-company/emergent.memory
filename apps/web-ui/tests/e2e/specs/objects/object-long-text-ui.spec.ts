import { test, expect } from '@playwright/test';
import { cleanupObjects } from '../../helpers/objects';

// Regression: a long, multi-line string property must present as a grown field
// with an initialised character counter after *boosted* client-side navigation
// (clicking an object row on /objects), not only on a direct URL load.
//
// The gateway shell boosts #main-content (ui.templ), so in-page navigation swaps
// the main region without firing DOMContentLoaded. Before the fix, app.js ran
// autogrowAll() only on DOMContentLoaded, so the swapped textarea stayed one row
// tall (its long value hidden behind a scroller) and its counter never
// initialised. This spec drives the exact reported path: create a Person with a
// long `notes` value, load /objects, then click into the object — never
// hard-loading the detail URL — and assert the field grew and counts.
//
// The scratch object is deleted through the memory API in `finally` (the
// gateway exposes no object-delete route; see the e2e README).

const NOTES = 'textarea[name="prop_notes"]';
// The counter is the textarea's next sibling inside its [data-char-counter]
// wrapper; Person has several string fields, so scope by the field's name.
const NOTES_COUNT = `${NOTES} + [data-testid="char-count"]`;
const DETAIL_URL = /\/objects\/[0-9a-f-]{36}$/;

/** Comma-grouped count, matching the server-rendered format. */
function grouped(n: number): string {
  return n.toLocaleString('en-US');
}

test('long-text field grows and counts after boosted navigation', async ({ page }) => {
  test.setTimeout(120_000);
  const key = `E2E LongText ${Date.now()}`;
  const longBody = Array.from(
    { length: 60 },
    (_, i) => `Line ${i + 1} of a long free-form note`,
  ).join('\n');
  let objectId = '';

  try {
    // 1. Create a Person whose `notes` (a schema string with widget: textarea)
    //    holds a long value, through the real create form (hard navigation).
    await page.goto('/objects/new');
    await page.locator('select[name="type"]').selectOption('Person');
    await expect(page.locator('#object-key')).toBeVisible();
    await page.locator('#object-key').fill(key);
    await expect(page.locator(NOTES)).toBeVisible();
    await page.locator(NOTES).fill(longBody);
    await page.getByRole('button', { name: 'Create object' }).click();
    await page.waitForURL(DETAIL_URL);
    objectId = new URL(page.url()).pathname.split('/').pop() ?? '';
    expect(objectId, 'no object id in the detail URL').not.toBe('');

    // 2. Load the list, then click into the object via the boosted row link —
    //    deliberately NOT page.goto(detail): a hard load would fire
    //    DOMContentLoaded and mask the regression. A window flag survives a
    //    boosted swap but is lost on a full reload, so it also proves the
    //    navigation was client-side.
    await page.goto('/objects');
    await page.evaluate(() => {
      (window as any).__e2eBoosted = true;
    });
    const rowLink = page.locator(`#objects-list a[href="/objects/${objectId}"]`);
    await expect(rowLink).toBeVisible();
    await rowLink.click();

    await expect(page.locator(NOTES)).toBeVisible();
    await expect(page.locator('#object-key')).toHaveValue(key);
    expect(
      await page.evaluate(() => (window as any).__e2eBoosted),
      'navigating to the object must be a boosted in-page swap, not a full load',
    ).toBe(true);

    // 3. The field grew past a single row (one line is ~20px; the value is 60
    //    lines, so the box should be well beyond a couple of rows), and the
    //    resize-y affordance is applied (it can be dragged taller).
    const box = await page.locator(NOTES).boundingBox();
    expect(box, 'long-text textarea has no box').not.toBeNull();
    expect(box!.height).toBeGreaterThan(120);
    await expect(page.locator(NOTES)).toHaveCSS('resize', 'vertical');

    // 4. The character counter is initialised to the LF-normalized length and
    //    updates live on edit (a lone textarea CRLF is normalized to LF on both
    //    sides, so the counts agree).
    await expect(page.locator(NOTES_COUNT)).toHaveText(`${grouped(longBody.length)} characters`);
    await page.locator(NOTES).fill('short');
    await expect(page.locator(NOTES_COUNT)).toHaveText('5 characters');
  } finally {
    await cleanupObjects(page, [objectId]);
  }
});
