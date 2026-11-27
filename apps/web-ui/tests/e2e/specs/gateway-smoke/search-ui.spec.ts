import { test, expect } from '@playwright/test';

// Browser smoke for project search (/search). Runs in the gateway-smoke suite,
// i.e. on the `dev -> main` promotion PR and on pushes to `main` — not on a
// feature PR.
//
// /search is the one object surface (#1076): browsing and searching render the
// same card grid, so an object hit is a card while a text-leg document hit keeps
// its own row above the cards. The page runs POST /api/search/unified's text leg
// for the document section and its graph leg for the cards; the mock
// (mock-memory.mjs) answers one chunk hit and one object hit per query and
// returns an empty set for a `nomatch` query, so these contracts — one request
// fills both legs, the scope filter narrows the request, a chunk renders trimmed
// with its mode and score — are all observable without a live backend.
test.describe('Project search', () => {
  test('the sidebar links to Search, and an empty query browses instead of searching', async ({
    page,
  }) => {
    await page.goto('/agents');
    const sidebar = page.locator('#_layout-sidebar');
    await expect(sidebar.getByRole('link', { name: 'Search' })).toHaveAttribute(
      'href',
      /\/search$/,
    );

    await page.goto('/search');
    // An empty query is the browse list on the merged surface, so the explainer
    // lives in the page header (and the query box's placeholder) rather than in
    // a pre-query card inside the results region.
    await expect(
      page.getByText(
        'Search inside ingested documents and across the knowledge graph',
      ),
    ).toBeVisible();
    await expect(page.getByTestId('search-query')).toBeVisible();
    // One browsed object from the mock: an empty query rendered the list…
    await expect(page.getByTestId('object-card')).toHaveCount(1);
    // …and no query means no request: the results summary only exists after a search.
    await expect(page.getByTestId('search-results-summary')).toHaveCount(0);
    await expect(page.getByTestId('search-hit-document')).toHaveCount(0);
  });

  test('one query fills both legs with usable hits', async ({ page }) => {
    await page.goto('/search?q=revenue');

    const summary = page.getByTestId('search-results-summary');
    await expect(summary).toContainText('2 results');
    await expect(summary).toContainText('revenue');
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

    // Object hit: a card on the same grid the browse list uses, linking through
    // the object's canonical id, naming its type and showing its score.
    const objHit = page.getByTestId('object-card');
    await expect(objHit).toHaveCount(1);
    const objLink = objHit.getByRole('link', { name: 'Open revenue-note' });
    await expect(objLink).toHaveAttribute(
      'href',
      /\/objects\/11223344-5566-4778-8999-000000000001\/wiki$/,
    );
    await expect(objHit).toContainText('Note');
    await expect(objHit).toContainText('0.72');
  });

  test('the scope filter narrows the search instead of hiding results client-side', async ({
    page,
  }) => {
    await page.goto('/search?q=revenue');
    await expect(page.getByTestId('object-card')).toHaveCount(1);

    // The merged page's `in` scope control is the retired search page's `type`
    // filter: documents, objects, or everything, applied by the server.
    await page.getByTestId('search-in-filter').selectOption('documents');
    await expect(page).toHaveURL(/[?&]in=documents\b/);
    await expect(page.getByTestId('search-hit-document')).toHaveCount(1);
    await expect(page.getByTestId('object-card')).toHaveCount(0);

    await page.getByTestId('search-in-filter').selectOption('objects');
    await expect(page).toHaveURL(/[?&]in=objects\b/);
    await expect(page.getByTestId('object-card')).toHaveCount(1);
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
