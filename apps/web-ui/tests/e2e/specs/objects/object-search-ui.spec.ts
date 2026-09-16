import { test, expect } from '@playwright/test';
import { createTypedObject, cleanupObjects } from '../../helpers/objects';

// Object search — GET /objects/search (uiObjectSearch).
//
// /objects/search is the JSON autocomplete behind the "Connect object" dialog's
// target picker (#connect-dst → <datalist id="object-search-options">). This
// spec covers it on both layers:
//   1. the JSON contract — a uniquely-named scratch object is the only match for
//      its unique token, and a nonsense token returns nothing (so the surface
//      filters rather than echoing the graph);
//   2. the real search surface — typing the object's name into the dialog's
//      target picker populates the datalist with that object's id/label;
//   3. navigation — the id from a search result opens the matching object page
//      (the "result links through to the object" path).
//
// Cleanup: the scratch object is deleted through the memory API with the
// signed-in user's own Zitadel access token (the gateway exposes no
// object-delete route), in `finally`, pass or fail.

interface SearchResult {
  id: string;
  label: string;
  type: string;
}

test('object search: filters to the created object and its result opens the object', async ({
  page,
}) => {
  test.setTimeout(120_000);
  const stamp = Date.now();
  const key = `E2E Search ${stamp}`;
  let objectId = '';

  try {
    objectId = await createTypedObject(page, 'Person', key);

    // 1a. The JSON contract: the unique token resolves to exactly this object.
    const hitResp = await page.request.get(`/objects/search?q=${encodeURIComponent(key)}`);
    expect(hitResp.ok(), `GET /objects/search failed (HTTP ${hitResp.status()})`).toBeTruthy();
    const hits = (await hitResp.json()) as SearchResult[];
    const mine = hits.filter((r) => r.id === objectId);
    expect(mine, `search for "${key}" did not return the created object`).toHaveLength(1);
    expect(mine[0].label).toBe(key);
    expect(mine[0].type).toBe('Person');

    // 1b. Filtering: an unmatched token returns no results (and an empty query
    // short-circuits to []), so the surface is not echoing the whole graph.
    const noneResp = await page.request.get(
      `/objects/search?q=${encodeURIComponent(`E2Enomatch${stamp}`)}`,
    );
    expect(((await noneResp.json()) as SearchResult[])).toHaveLength(0);
    const emptyResp = await page.request.get('/objects/search?q=');
    expect(((await emptyResp.json()) as SearchResult[])).toHaveLength(0);

    // 2. The real search surface: typing the object's name into the connect
    // dialog's target picker populates the datalist with the object's id + label.
    await page.goto(`/objects/${objectId}`);
    await page.getByRole('button', { name: 'Connect this object to another' }).click();
    await expect(page.locator('#object-connect-modal')).toBeVisible();
    await page.locator('#connect-dst').fill(key);
    const option = page.locator(`#object-search-options option[value="${objectId}"]`);
    await expect(option).toHaveCount(1);
    await expect(option).toHaveText(key);

    // 3. The result links through to the object page (id → /objects/:id).
    await page.goto(`/objects/${objectId}`);
    await expect(page.getByRole('heading', { name: key })).toBeVisible();
    await expect(page.locator('#object-key')).toHaveValue(key);
  } finally {
    await cleanupObjects(page, [objectId]);
  }
});
