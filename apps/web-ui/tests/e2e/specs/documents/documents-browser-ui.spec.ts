import { test, expect } from '@playwright/test';
import { expectAppPage, goto } from '../../helpers/page';
import {
  DOCUMENTS_COUNT,
  DOCUMENTS_LOAD_MORE,
  DOCUMENTS_MOBILE_LIST,
  DOCUMENTS_SEARCH_INPUT,
  DOCUMENTS_STATS,
  DOCUMENTS_STATUS_FILTER,
  DOCUMENTS_TABLE,
  DOCUMENTS_UPLOAD_MODAL,
  DOCUMENTS_UPLOAD_OPEN,
  documentRowIDs,
  statValue,
  uniqueDocumentToken,
  uploadTestDocument,
} from '../../helpers/documents';

/**
 * Documents browser: the list controls that turn a single frozen page into a
 * dataset view — search, status filter, source filter, sorting, counts and cursor
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
    // length of the page slice; with no status filter active it must equal the
    // stats strip's total. (That it is not the page length at all is pinned
    // where the seeded population is known: TestRenderDocumentsBrowser.)
    const matched = await statValue(page, 'documents');
    expect(matched).toBeGreaterThanOrEqual(2);
    await expect(page.locator(DOCUMENTS_COUNT)).toContainText(String(matched));
    // Two matches are one page: no "Load more" trigger.
    await expect(page.locator(DOCUMENTS_LOAD_MORE)).toHaveCount(0);

    // --- Status filter keeps the search and marks the active option ---
    const statusFilter = page.locator(DOCUMENTS_STATUS_FILTER);
    // The select submits its form on change, so wait for the navigation it
    // starts rather than racing the locator call against the page swap.
    await Promise.all([
      page.waitForURL(/status=failed/),
      statusFilter.selectOption('failed'),
    ]);
    await expect(page).toHaveURL(new RegExp(`q=${token}`));
    await expect(statusFilter).toHaveValue('failed');
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
    // #2312 removed the standalone "Clear" button: the search field's own
    // affordance covers it, so clear the way a user would — empty the field and
    // submit it. Sort/order are carried by hidden inputs, so the URL keeps them.
    await page.locator(DOCUMENTS_SEARCH_INPUT).fill('');
    await page.locator(DOCUMENTS_SEARCH_INPUT).press('Enter');
    // A serialized text control submits `q=` even when it is empty, so the URL
    // keeps the parameter — assert its value, not the substring, or this waits
    // out its timeout on a page that cleared correctly.
    await expect
      .poll(() => new URL(page.url()).searchParams.get('q'), {
        message: 'submitting the emptied search field should leave no query',
      })
      .toBe('');
    await expect(page.locator(DOCUMENTS_SEARCH_INPUT)).toHaveValue('');
    await expect(page.getByText('No documents match')).toHaveCount(0);
    expect(
      (await documentRowIDs(page)).length,
      'clearing the search should list documents again instead of the filtered empty state',
    ).toBeGreaterThan(0);

    // --- Upload is a header button that opens a dialog, not an inline form ---
    await expect(page.locator(DOCUMENTS_UPLOAD_MODAL)).toBeHidden();
    await page.locator(DOCUMENTS_UPLOAD_OPEN).click();
    await expect(page.locator(DOCUMENTS_UPLOAD_MODAL)).toBeVisible();
    await expect(page.locator('input[name="file"]')).toBeVisible();
    await page.locator('[data-dialog-close="document-upload-modal"]').click();
    await expect(page.locator(DOCUMENTS_UPLOAD_MODAL)).toBeHidden();

    // --- Source filter: uploads only ---
    await page.locator('#documents-filter-source').selectOption('none');
    await expect(page).toHaveURL(/sourceId=none/);
    await expect(page.locator('#documents-filter-source')).toHaveValue('none');
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
    await expect(page.locator(DOCUMENTS_STATUS_FILTER)).toBeVisible();
    await expect(page.locator(DOCUMENTS_SEARCH_INPUT)).toBeVisible();

    // The whole card is the tap target (data-href), not only the name link, and
    // tapping it opens the document. This is the card's real navigation path: it
    // used to be one big <a>, which cannot contain the source link some rows
    // carry, so the tap target moved to data-href.
    const card = page.locator(`${DOCUMENTS_MOBILE_LIST} [data-document-id]`).first();
    const id = await card.getAttribute('data-document-id');
    expect(id).toBeTruthy();
    await expect(card).toHaveAttribute('data-href', new RegExp(`/documents/${id}$`));
    await card.getByText(/^updated /).click();
    await page.waitForURL(new RegExp(`/documents/${id}$`));
  });
});

/**
 * Cursor pagination: Load more appends the next page and then disappears.
 *
 * The Postgres suite walks every page against the repository; this drives the
 * HTMX swap the server tests cannot see — table append (not replace), the
 * out-of-band mobile card append, and the trigger retiring on the last page.
 * A shared project cannot be truncated, so the spec seeds its own 51 documents
 * (50 is the page size) under one per-run token and filters to them.
 */
test('append next page with Load more, then retire the trigger', async ({ page }) => {
  test.setTimeout(120_000);
  const token = uniqueDocumentToken('e2e-load-more');
  const total = 51;
  const names = Array.from({ length: total }, (_, i) => `${token}-${String(i).padStart(2, '0')}.md`);

  // Seed in batches: sequential uploads would spend most of the budget on
  // round-trips. Bodies stay unique so content dedup cannot collapse two.
  for (let i = 0; i < names.length; i += 12) {
    await Promise.all(
      names.slice(i, i + 12).map((name, j) => uploadTestDocument(page, name, `# ${name}\n\nseed ${i + j} for ${token}`)),
    );
  }

  await goto(page, `/documents?q=${token}`);
  await expectAppPage(page, /Documents/);

  const rows = page.locator('#documents-list tr[data-document-id]');
  const cards = page.locator('#documents-mobile-list [data-document-id]');
  await expect(rows).toHaveCount(50);
  await expect(cards).toHaveCount(50);
  await expect(page.locator(DOCUMENTS_LOAD_MORE)).toBeVisible();

  const firstPage = await documentRowIDs(page);
  expect(firstPage).toHaveLength(50);
  expect(new Set(firstPage).size, 'the first page must not repeat a document').toBe(50);

  await page.locator(DOCUMENTS_LOAD_MORE).click();

  await expect(rows).toHaveCount(total);
  await expect(cards).toHaveCount(total);
  // One page of 50 + the single remaining row is the last page: the trigger is
  // replaced by an empty slot.
  await expect(page.locator(DOCUMENTS_LOAD_MORE)).toHaveCount(0);

  const appended = await documentRowIDs(page);
  expect(appended).toHaveLength(total);
  expect(appended.slice(0, 50), 'Load more must append, not replace the first page').toEqual(firstPage);
  expect(new Set(appended).size, 'appended page must not duplicate a first-page id').toBe(total);

  const cardIDs = await cards.evaluateAll((els) => els.map((e) => e.getAttribute('data-document-id') ?? ''));
  expect(cardIDs, 'the mobile card list must receive the same rows out of band').toEqual(appended);
});
