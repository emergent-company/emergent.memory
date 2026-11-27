import { test, expect } from '@playwright/test';
import { DOCUMENTS_SEARCH_INPUT } from '../../helpers/documents';

// Browser contract for list filters: a filter refresh replaces the
// results region and nothing else. The page, its toolbar, the address bar and
// the document itself all stay put. Runs in the gateway-smoke suite, i.e. on the
// `dev -> main` promotion PR and on pushes to `main` — not on a feature PR.
//
// Why this has to be a browser test: the filters used to call form.submit(),
// which never dispatches a `submit` event, so htmx's boosted form never saw it
// and the browser performed a full document load. Every Go-side render test
// still passed — only a real engine can observe "the document did not reload".
//
// /objects is retired (#1076): it 301s to /search, which renders the same cards
// and the same `search-results` region, so it no longer has a region of its own
// to assert here. The redirect itself is pinned by its own test below.

const REGIONS = [
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

  test('/objects is retired and redirects to /search, keeping the query string', async ({
    request,
  }) => {
    // #1076 merged the objects browser into /search: same cards, same filters,
    // plus the document-text leg. A bookmarked filter, or a type-scoped link,
    // must land on the same list — so the redirect carries the query string —
    // while `mode` (the search-mode chooser the merged page no longer has) is
    // dropped rather than forwarded as a dead parameter.
    const redirect = await request.get('/objects?type=Note&q=revenue&mode=cards', {
      maxRedirects: 0,
    });
    expect(redirect.status()).toBe(301);
    const location = redirect.headers()['location'];
    expect(location).toBeTruthy();
    const target = new URL(location, 'http://localhost');
    expect(target.pathname).toBe('/search');
    expect(target.searchParams.get('q')).toBe('revenue');
    expect(target.searchParams.get('type')).toBe('Note');
    expect(target.searchParams.has('mode')).toBe(false);

    // Following it lands on the merged surface itself, not on an error page.
    const pageRes = await request.get('/objects');
    expect(pageRes.status()).toBe(200);
    expect(await pageRes.text()).toContain('id="search-results"');
  });

  test('the documents toolbar has a single search control, with the applied query mirrored as a hidden input', async ({
    page,
  }) => {
    // #584: the scheduled mutations suite filled `input[name="q"]` and Playwright
    // rejected it as ambiguous on a `?q=` URL. The duplicate is intentional —
    // the search form and the sibling filter form each carry the other's state —
    // so this pins the shape that makes a bare `input[name="q"]` selector
    // ambiguous, and imports the selector the specs use, so the helper and this
    // guard cannot drift apart.
    await page.goto('/documents?q=quarterly');

    const search = page.locator(DOCUMENTS_SEARCH_INPUT);
    await expect(search).toHaveCount(1);
    await expect(search).toHaveValue('quarterly');

    // Exactly one *visible* control named q; the other match is the hidden
    // mirror inside the filter form.
    await expect(page.locator('input[name="q"]:visible')).toHaveCount(1);
    await expect(
      page
        .getByTestId('documents-filter-form')
        .locator('input[type="hidden"][name="q"]'),
    ).toHaveValue('quarterly');
  });

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
    // Object hits are cards on the same grid the browse list uses (#1076).
    await expect(page.getByTestId('object-card')).toHaveCount(1);
    await page.evaluate(() => {
      (window as unknown as { __sameDocument: boolean }).__sameDocument = true;
    });

    // The scope control (`in`) replaces the retired search page's `type` select:
    // same question — documents, objects, or both — and the same server-side
    // narrowing.
    await page.getByTestId('search-in-filter').selectOption('documents');

    await expect(page).toHaveURL(/[?&]in=documents\b/);
    await expect(page.getByTestId('object-card')).toHaveCount(0);
    await expect(page.getByTestId('search-hit-document')).toHaveCount(1);
    // The count is part of the results summary, inside the swapped region, so
    // it always describes the results currently on screen.
    await expect(page.getByTestId('search-results-summary')).toContainText(
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
