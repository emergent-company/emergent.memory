import { test, expect, type Page } from '@playwright/test';

// Per-PR browser smoke for the Needs attention panel on /documents (gateway/
// processing_problems.templ). Regression cover for the dead ends a reader hit
// against a real project:
//
//   1. "View all 12 in Documents →" landed on /documents?status=failed and the
//      page answered "No documents match". The panel aggregates run failures
//      (object extraction, document conversion, chunk embedding); the documents
//      listing filters on a document's DERIVED status, which cannot represent a
//      failed chunk embedding — the document still reads "Extracted".
//   2. A row's "View" link must open the document the run belongs to.
//   3. "Extract data" on that document is a POST + redirect; the reader must
//      land on the rendered document page, not an error page at the same URL.
//
// The mock serves a documents area whose 12 problems are chunk-embedding runs
// on one extracted document (mock-memory.mjs mockProcessingProblems) and honours
// the status filter, so the old destination really does render the empty state
// here — the assertions below fail if the link regresses to it.
//
// Scope: this lane runs the gateway with AUTH_MODE=dev, so it serves unscoped
// pages without complaint and a flat redirect is a working URL rather than a
// 404. The last test therefore loads /o/o1/p/p1/… explicitly: with a URL scope
// the scope middleware rewrites Location exactly as it does in session mode, so
// it pins the real 404 class — a redirect that loses /o/{org}/p/{project} —
// without a Zitadel login (same shape as provider-add-scope.spec.ts).

const PANEL = '[data-testid="processing-problems"]';

/** Rows rendered inside one panel area. */
function areaRows(page: Page, area: string) {
  return page
    .getByTestId(`processing-area-${area}`)
    .locator('[data-testid="processing-problem-row"]');
}

test.describe('Documents needs-attention panel', () => {
  test('"View all" opens the panel rows instead of a filters-that-cannot-match page', async ({
    page,
  }) => {
    await page.goto('/documents');

    await expect(page.locator(PANEL)).toBeVisible();
    // The header badge totals every area: 12 document runs plus 1 schema run.
    await expect(page.locator(PANEL)).toContainText('13 open');

    // Clipped by default: eight document rows plus the one schema row.
    await expect(areaRows(page, 'documents')).toHaveCount(8);
    await expect(areaRows(page, 'schema')).toHaveCount(1);

    const viewAll = page.getByTestId('processing-area-view-all-documents');
    await expect(viewAll).toContainText('View all 12 in Documents');
    await viewAll.click();

    // The destination is the panel's own full list, not a document-status
    // filter: only the panel can list these runs.
    await expect(page).toHaveURL(/\/documents\?attention=documents#needs-attention$/);
    await expect(areaRows(page, 'documents')).toHaveCount(12);
    // The other areas keep their clip.
    await expect(areaRows(page, 'schema')).toHaveCount(1);

    // The old destination's empty state must not appear under a non-zero count.
    await expect(page.getByTestId('documents-no-matches')).toHaveCount(0);
    await expect(page.getByText('No documents match')).toHaveCount(0);

    // ...and the filter that used to be the destination really is empty, so this
    // test fails loudly if the link is ever pointed back at it.
    await page.goto('/documents?status=failed');
    await expect(page.getByTestId('documents-no-matches')).toBeVisible();
  });

  test('"Show fewer" restores the clipped panel and keeps the page state', async ({ page }) => {
    await page.goto('/documents?attention=documents&q=report');

    await expect(areaRows(page, 'documents')).toHaveCount(12);
    const collapse = page.getByTestId('processing-area-collapse-documents');
    await expect(collapse).toContainText('Show fewer');

    await collapse.click();

    await expect(page).toHaveURL(/q=report(?!.*attention=)/);
    await expect(page.locator(PANEL)).toBeVisible();
    await expect(areaRows(page, 'documents')).toHaveCount(8);
    await expect(page.getByTestId('processing-area-view-all-documents')).toBeVisible();
  });

  test('a failure row links to the document it belongs to', async ({ page }) => {
    await page.goto('/documents?attention=documents');

    const view = areaRows(page, 'documents')
      .first()
      .getByRole('link', { name: 'View' });
    await expect(view).toHaveAttribute('href', /\/documents\/doc-report$/);

    await view.click();
    await expect(page).toHaveURL(/\/documents\/doc-report$/);
    // The detail page rendered (a 404/global error page has no document header).
    await expect(page.getByRole('heading', { name: /report\.md/ }).first()).toBeVisible();
  });

  test('extracting from the document a failure row opened returns to that page', async ({
    page,
  }) => {
    // The second half of the reported journey: the panel row's View, then
    // "Extract data". The trigger is a POST + redirect; an assertion on the URL
    // alone passes even when the response at that URL is an error page, so this
    // pins the response status and the rendered document instead.
    await page.goto('/documents?attention=documents');
    await areaRows(page, 'documents')
      .first()
      .getByRole('link', { name: 'View' })
      .click();
    await expect(page).toHaveURL(/\/documents\/doc-report$/);

    const landed = page.waitForResponse(
      (resp) =>
        resp.request().method() === 'GET' && /\/documents\/doc-report\?extracted=1$/.test(resp.url()),
    );
    await page.getByRole('button', { name: 'Extract data' }).click();
    const response = await landed;

    expect(response.status(), 'the redirect target must be a page, not the gateway 404').toBe(200);
    await expect(page).toHaveURL(/\/documents\/doc-report\?extracted=1$/);
    await expect(page.getByRole('heading', { name: /report\.md/ }).first()).toBeVisible();
    await expect(
      page.locator('#toast-container [role="alert"]').filter({ hasText: 'Extraction triggered.' }),
    ).toBeVisible();
  });

  // The exact reported journey, on a scoped URL: needs-attention row → document
  // → "Extract data" → back on the document page, still under /o/{org}/p/{project}.
  // A redirect target that loses the scope keeps the right path and query but is
  // a hard-cut 404 in session mode, so the assertion is on the URL the browser
  // ends on, not just on the response body.
  const SCOPE = '/o/o1/p/p1';

  test('the whole journey keeps its URL scope', async ({ page }) => {
    await page.goto(`${SCOPE}/documents`);
    await expect(page.locator(PANEL)).toBeVisible();
    await expect(areaRows(page, 'documents')).toHaveCount(8);

    // The full-list link is rendered for this scope, so following it cannot
    // leave the scoped app.
    const viewAll = page.getByTestId('processing-area-view-all-documents');
    await expect(viewAll).toHaveAttribute(
      'href',
      `${SCOPE}/documents?attention=documents#needs-attention`,
    );
    await viewAll.click();

    // The expansion is page state on the same scoped page, not a different one.
    await expect(page).toHaveURL(new RegExp(`^.*${SCOPE}/documents\\?attention=documents`));
    await expect(areaRows(page, 'documents')).toHaveCount(12);

    await areaRows(page, 'documents').first().getByRole('link', { name: 'View' }).click();
    await expect(page).toHaveURL(`${SCOPE}/documents/doc-report`);

    const landed = page.waitForResponse(
      (resp) =>
        resp.request().method() === 'GET' && /\/documents\/doc-report\?extracted=1$/.test(resp.url()),
    );
    await page.getByRole('button', { name: 'Extract data' }).click();
    const response = await landed;

    // The PRG target is context-free on the wire; the scope middleware must put
    // the prefix back before the browser follows it. A flat
    // /documents/doc-report?extracted=1 is the reported 404.
    await expect(page).toHaveURL(`${SCOPE}/documents/doc-report?extracted=1`);
    expect(response.status()).toBe(200);
    await expect(page.getByRole('heading', { name: /report\.md/ }).first()).toBeVisible();
  });
});
