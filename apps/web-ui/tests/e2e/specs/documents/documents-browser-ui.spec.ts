import { test, expect } from '@playwright/test';
import { expectAppPage, goto } from '../../helpers/page';
import {
  DOCUMENTS_COUNT,
  DOCUMENTS_LOAD_MORE,
  DOCUMENTS_MOBILE_LIST,
  DOCUMENTS_SEARCH_INPUT,
  DOCUMENTS_STATS,
  DOCUMENTS_STATUS_TABS,
  DOCUMENTS_TABLE,
  documentRowIDs,
  statValue,
  uniqueDocumentToken,
  uploadTestDocument,
} from '../../helpers/documents';

/**
 * Documents browser: the list controls that turn a single frozen page into a
 * dataset view — search, status tabs, source filter, sorting, counts and cursor
 * pagination.
 *
 * The keyset walk itself (every page, both sorts, both directions) is covered
 * against Postgres in the server's integration suite; this spec drives the
 * wiring a unit test cannot: the controls, the URLs they produce, and what the
 * page renders after a control is used.
 *
 * Mutation spec (`-ui` suffix) so it runs in the serialized `mutations` project.
 * Seeded documents are left in the shared e2e project, named with a per-test
 * token so the search box can isolate them.
 */
test.describe('Documents browser', () => {
  test('searches, filters and sorts the document list', async ({ page }) => {
    const token = uniqueDocumentToken();
    await uploadTestDocument(page, `${token}-alpha.md`, `# alpha ${token}\n\nfirst seeded document`);
    await uploadTestDocument(page, `${token}-beta.md`, `# beta ${token}\n\nsecond seeded document`);

    // --- Search narrows the list to this test's documents ---
    await goto(page, `/documents?q=${token}`);
    await expectAppPage(page, /Documents/);

    const rows = page.locator('#documents-list tr[data-document-id]');
    await expect(rows).toHaveCount(2);
    await expect(page.locator(DOCUMENTS_STATS)).toBeVisible();

    // The header count is the number of documents the filter matched, not the
    // length of the page slice; with no status tab active it must equal the
    // strip's total. (That it is not the page length at all is pinned where the
    // seeded population is known: TestRenderDocumentsBrowser.)
    const matched = await statValue(page, 'documents');
    expect(matched).toBeGreaterThanOrEqual(2);
    await expect(page.locator(DOCUMENTS_COUNT)).toContainText(String(matched));
    // Two matches are one page: no "Load more" trigger.
    await expect(page.locator(DOCUMENTS_LOAD_MORE)).toHaveCount(0);

    // --- Status tabs keep the search and mark the active tab ---
    const failedTab = page.locator(`${DOCUMENTS_STATUS_TABS} a[data-status="failed"]`);
    await failedTab.click();
    await expect(page).toHaveURL(/status=failed/);
    await expect(page).toHaveURL(new RegExp(`q=${token}`));
    await expect(failedTab).toHaveAttribute('aria-current', 'page');
    // A filtered-out result never claims the project has no documents.
    await expect(page.getByText('No documents yet')).toHaveCount(0);

    // --- Sorting by last update, then flipping the direction ---
    // The alpha document is uploaded first, so it sorts last under
    // "most recently updated first" and first under the opposite direction: a
    // flip that only re-rendered the same order would fail this.
    await goto(page, `/documents?q=${token}`);
    await page.getByRole('link', { name: 'Updated' }).click();
    await expect(page).toHaveURL(/sort=updated/);
    await expect(page.getByText('Recently updated first')).toBeVisible();
    await expect(page).toHaveURL(new RegExp(`q=${token}`));

    const newestFirst = await documentRowIDs(page);
    expect(newestFirst, 'sort=updated should list the newest update first').toHaveLength(2);
    expect(newestFirst[0]).not.toBe(newestFirst[1]);

    await page.getByRole('link', { name: 'Updated' }).click();
    await expect(page).toHaveURL(/order=asc/);
    await expect(page.getByText('Least recently updated first')).toBeVisible();
    // Same two documents, opposite order: no gaps, no duplicates, no no-op flip.
    const oldestFirst = await documentRowIDs(page);
    expect(oldestFirst).toEqual([...newestFirst].reverse());

    // --- Clearing the search restores the whole list ---
    await page.locator('[data-testid="documents-clear-filters"]').click();
    await expect(page).toHaveURL(/\/documents$/);
    await expect(page.locator(DOCUMENTS_SEARCH_INPUT)).toHaveValue('');
    await expect(page.locator('input[name="file"]')).toBeVisible();

    // --- Source filter: uploads only ---
    await page.locator('#documents-filter-source').selectOption('upload');
    await expect(page).toHaveURL(/sourceType=upload/);
    await expect(page.locator('#documents-filter-source')).toHaveValue('upload');
  });

  test('renders cards instead of the table on a phone viewport', async ({ page }) => {
    // Seed its own rows: the card list only exists when there is something to
    // show, so this must not lean on whatever another test left behind.
    const token = uniqueDocumentToken();
    await uploadTestDocument(page, `${token}-phone.md`, `# phone ${token}\n\ncard list seed`);

    await page.setViewportSize({ width: 390, height: 844 });
    await goto(page, `/documents?q=${token}`);
    await expectAppPage(page, /Documents/);
    await expect(page.locator(`${DOCUMENTS_MOBILE_LIST} [data-document-id]`).first()).toBeVisible();

    await expect(page.locator(DOCUMENTS_MOBILE_LIST)).toBeVisible();
    await expect(page.locator(DOCUMENTS_TABLE)).toBeHidden();
    // The controls are reachable on the small viewport too.
    await expect(page.locator(DOCUMENTS_STATUS_TABS)).toBeVisible();
    await expect(page.locator(DOCUMENTS_SEARCH_INPUT)).toBeVisible();
  });
});
