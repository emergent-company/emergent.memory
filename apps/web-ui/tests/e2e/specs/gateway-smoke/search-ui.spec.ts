import { test, expect } from '@playwright/test';

// Browser smoke for project search (/search). Runs in the gateway-smoke suite,
// i.e. on the `dev -> main` promotion PR and on pushes to `main` — not on a
// feature PR.
//
// This is the one UI surface that renders POST /api/search/unified's TEXT hits.
// The objects browser calls the same endpoint with resultTypes=graph, so a
// project that has ingested documents but extracted no objects had no way to
// search its content from the UI. The mock (mock-memory.mjs) answers one chunk
// hit and one object hit per query and returns an empty set for a `nomatch`
// query, so these contracts — one request fills both sections, the type filter
// narrows the request, a chunk renders trimmed with its mode and score — are
// all observable without a live backend.
test.describe('Project search', () => {
  test('the sidebar links to Search, and an empty query renders the explainer', async ({
    page,
  }) => {
    await page.goto('/agents');
    const sidebar = page.locator('#_layout-sidebar');
    await expect(sidebar.getByRole('link', { name: 'Search' })).toHaveAttribute(
      'href',
      /\/search$/,
    );

    await page.goto('/search');
    await expect(page.getByTestId('search-intro')).toBeVisible();
    await expect(page.getByTestId('search-query')).toBeVisible();
    // No query means no request: the results summary only exists after a search.
    await expect(page.getByTestId('search-results-summary')).toHaveCount(0);
    await expect(page.getByTestId('search-hit-document')).toHaveCount(0);
  });

  test('one query fills both sections with usable hits', async ({ page }) => {
    await page.goto('/search?q=revenue');

    await expect(page.getByTestId('search-results-summary')).toContainText(
      '2 results',
    );
    await expect(page.getByTestId('search-results-summary')).toContainText(
      'revenue',
    );
    await expect(page.getByTestId('search-timing')).toContainText('12 ms');

    // Document hit: the resolved filename, a link to the document, the match
    // mode and the score.
    const docHit = page.getByTestId('search-hit-document');
    await expect(docHit).toHaveCount(1);
    const docLink = docHit.getByTestId('search-hit-document-link');
    await expect(docLink).toHaveText('report.md');
    await expect(docLink).toHaveAttribute('href', /\/documents\/doc-report$/);
    await expect(docHit).toContainText('Keyword + semantic');
    await expect(docHit.getByTestId('search-hit-score')).toHaveText('0.91');

    // The endpoint returns the whole chunk; the page trims it, so the rendered
    // passage is a bounded prefix, not the full repeated body.
    const snippet = docHit.getByTestId('search-hit-snippet');
    await expect(snippet).toContainText('Revenue grew 12% in Q3');
    const snippetText = (await snippet.innerText()).trim();
    expect(snippetText.length).toBeLessThan(600);
    expect(snippetText.endsWith('…')).toBe(true);

    // Object hit: the object's key links through its canonical id.
    const objHit = page.getByTestId('search-hit-object');
    await expect(objHit).toHaveCount(1);
    const objLink = objHit.getByTestId('search-hit-object-link');
    await expect(objLink).toHaveText('revenue-note');
    await expect(objLink).toHaveAttribute(
      'href',
      /\/objects\/11223344-5566-4778-8999-000000000001$/,
    );
    await expect(objHit).toContainText('Note');
    await expect(objHit.getByTestId('search-hit-score')).toHaveText('0.72');
  });

  test('the type filter narrows the search instead of hiding results client-side', async ({
    page,
  }) => {
    await page.goto('/search?q=revenue');
    await expect(page.getByTestId('search-hit-object')).toHaveCount(1);

    await page.getByTestId('search-type-filter').selectOption('documents');
    await expect(page).toHaveURL(/[?&]type=documents\b/);
    await expect(page.getByTestId('search-hit-document')).toHaveCount(1);
    await expect(page.getByTestId('search-hit-object')).toHaveCount(0);

    await page.getByTestId('search-type-filter').selectOption('objects');
    await expect(page).toHaveURL(/[?&]type=objects\b/);
    await expect(page.getByTestId('search-hit-object')).toHaveCount(1);
    await expect(page.getByTestId('search-hit-document')).toHaveCount(0);
  });

  test('the limit select is round-tripped through the URL', async ({ page }) => {
    await page.goto('/search?q=revenue');
    await page.getByTestId('search-limit-filter').selectOption('50');

    await expect(page).toHaveURL(/[?&]limit=50\b/);
    // The control reflects the applied limit rather than snapping back to the
    // default while the query ran with 50.
    await expect(page.getByTestId('search-limit-filter')).toHaveValue('50');
  });

  test('a query with no hits renders the empty state, not a blank page', async ({
    page,
  }) => {
    await page.goto('/search?q=nomatch');

    await expect(page.getByTestId('search-results-summary')).toContainText(
      '0 results',
    );
    await expect(page.getByText('No matches for "nomatch"')).toBeVisible();
    await expect(page.getByTestId('search-hit-document')).toHaveCount(0);
    await expect(page.getByTestId('search-hit-object')).toHaveCount(0);
    // One page-level empty state, not that plus two section empty states.
    await expect(page.getByText('No document text matched')).toHaveCount(0);
    await expect(page.getByText('No objects matched')).toHaveCount(0);
  });
});
