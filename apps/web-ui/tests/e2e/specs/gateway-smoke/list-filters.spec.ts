import { test, expect } from '@playwright/test';

// Per-PR browser contract for list filters: a filter refresh replaces the
// results region and nothing else. The page, its toolbar, the address bar and
// the document itself all stay put.
//
// Why this has to be a browser test: the filters used to call form.submit(),
// which never dispatches a `submit` event, so htmx's boosted form never saw it
// and the browser performed a full document load. Every Go-side render test
// still passed — only a real engine can observe "the document did not reload".
//
// The mock harness (run-e2e.sh) has no graph backend, so /objects renders its
// error state there; its region dispatch is covered by the request-level test
// below instead of by clicking.

const REGIONS = [
  { path: '/objects', region: 'objects-results' },
  { path: '/documents', region: 'documents-results' },
  { path: '/search', region: 'search-results' },
  { path: '/inbox', region: 'inbox-list' },
];

test.describe('list filters refresh only the results region', () => {
  for (const { path, region } of REGIONS) {
    test(`${path} answers an htmx request with the region alone`, async ({
      request,
    }) => {
      // A filter form requests the *page* URL with an HX-Target naming the
      // region, so the URL htmx pushes reloads correctly.
      const regionRes = await request.get(path, {
        headers: { 'HX-Request': 'true', 'HX-Target': `div#${region}` },
      });
      expect(regionRes.status()).toBe(200);
      const regionBody = await regionRes.text();
      expect(regionBody).toContain(`id="${region}"`);
      // The swap payload carries no page chrome around it.
      expect(regionBody).not.toContain('id="_layout-sidebar"');

      // The same URL without a region target is still the whole page.
      const pageRes = await request.get(path);
      expect(pageRes.status()).toBe(200);
      expect(await pageRes.text()).toContain('id="_layout-sidebar"');
    });
  }

  test('the documents status filter swaps the listing without reloading the document', async ({
    page,
  }) => {
    await page.goto('/documents');
    await expect(page.getByTestId('documents-table')).toBeVisible();

    // Two markers: one on the window (a document load would drop it) and one on
    // each element (a swap replaces the element, so its marker drops).
    await page.evaluate(() => {
      (window as unknown as { __sameDocument: boolean }).__sameDocument = true;
    });
    await page
      .getByTestId('documents-results')
      .evaluate((el) => ((el as unknown as { __stale: boolean }).__stale = true));
    await page
      .getByTestId('documents-filter-form')
      .evaluate((el) => ((el as unknown as { __stale: boolean }).__stale = true));

    await page.getByTestId('documents-status-filter').selectOption('failed');

    await expect(page).toHaveURL(/[?&]status=failed\b/);
    await expect(page.getByTestId('documents-no-matches')).toBeVisible();

    // The listing was replaced…
    expect(
      await page
        .getByTestId('documents-results')
        .evaluate((el) => (el as unknown as { __stale?: boolean }).__stale),
    ).toBeUndefined();
    // …while the toolbar the user is still operating was not, and no document
    // navigation happened at all.
    expect(
      await page
        .getByTestId('documents-filter-form')
        .evaluate((el) => (el as unknown as { __stale?: boolean }).__stale),
    ).toBe(true);
    expect(
      await page.evaluate(
        () => (window as unknown as { __sameDocument?: boolean }).__sameDocument,
      ),
    ).toBe(true);

    // The control still shows what was applied.
    await expect(page.getByTestId('documents-status-filter')).toHaveValue(
      'failed',
    );
  });

  test('a search filter applies itself, but typing in the query box does not', async ({
    page,
  }) => {
    await page.goto('/search?q=revenue');
    await expect(page.getByTestId('search-hit-document')).toHaveCount(1);
    await page.evaluate(() => {
      (window as unknown as { __sameDocument: boolean }).__sameDocument = true;
    });

    await page.getByTestId('search-type-filter').selectOption('documents');

    await expect(page).toHaveURL(/[?&]type=documents\b/);
    await expect(page.getByTestId('search-hit-object')).toHaveCount(0);
    await expect(page.getByTestId('search-hit-document')).toHaveCount(1);
    // The count is part of the results summary, inside the swapped region, so
    // it always describes the results currently on screen.
    await expect(page.getByTestId('search-result-count')).toContainText(
      '1 result',
    );
    expect(
      await page.evaluate(
        () => (window as unknown as { __sameDocument?: boolean }).__sameDocument,
      ),
    ).toBe(true);

    // A text field must not search on blur: only a <select>'s change applies.
    const query = page.getByTestId('search-query');
    const before = page.url();
    await query.fill('revenue by quarter');
    await page.getByTestId('search-limit-filter').focus();
    await page.waitForTimeout(500);
    expect(page.url()).toBe(before);
  });
});
