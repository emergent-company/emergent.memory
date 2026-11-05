import { test, expect, type Page } from '@playwright/test';

// Browser coverage for the discovery row in the Needs attention panel (#88).
//
// The panel aggregates run failures, and schema discovery is one of them. Three
// things about that row were only ever asserted in gateway unit tests, against
// fixtures that described the old shape:
//
//   1. The row linked "Open schema" → /schema, while the server's own action for
//      a discovery run is "Open discovery" → /schema#discovery, because the
//      schema page as a whole is the compiled type browser and landing at the
//      top left the reader hunting for the run the row is about.
//   2. The row's Retry posts to /schema/discovery/retry/:id. The mock had no
//      Memory route behind it, so no spec could click it without a 404.
//   3. The card a run deep-links to never refreshed itself: it was the only
//      surface reporting a project-wide run, and its progress stood still until
//      the page was reloaded.
//
// The mock (mock-memory.mjs) serves the corrected discovery row, one run in
// flight for the card, and the retry route, so all three are exercised here
// against a real gateway render.

const PANEL = '[data-testid="processing-problems"]';
const CARD = '[data-testid="schema-discovery-card"]';

/** Rows rendered inside the panel's schema area. */
function schemaRows(page: Page) {
  return page
    .getByTestId('processing-area-schema')
    .locator('[data-testid="processing-problem-row"]');
}

test.describe('Discovery in the needs-attention panel', () => {
  test('the row links to the discovery card, not the top of the schema page', async ({
    page,
  }) => {
    await page.goto('/documents');

    await expect(page.locator(PANEL)).toBeVisible();
    const row = schemaRows(page).first();
    await expect(row).toContainText('Schema discovery');

    // The server's actionFor(QueueDiscovery) answer, not a bare /schema.
    const view = row.getByRole('link', { name: 'View' });
    await expect(view).toHaveAttribute('href', '/schema#discovery');

    await view.click();
    await expect(page).toHaveURL(/\/schema#discovery$/);
    // The card rendered, and it is the element the fragment names.
    await expect(page.locator(CARD)).toBeVisible();
    await expect(page.locator(CARD)).toHaveAttribute('id', 'discovery');
  });

  // The reported dead end: the row offers a Retry, and before this the mock had
  // nothing behind its POST, so the click 404'd inside the gateway's Memory call
  // and the reader was dropped on an error page.
  test("the row's Retry re-runs the run and reports it on the card", async ({ page }) => {
    await page.goto('/documents');

    // A configuration failure is deliberately not retryable, so the button asks
    // first — the confirmation is part of the contract, not noise.
    const confirmations: string[] = [];
    page.on('dialog', async (dialog) => {
      confirmations.push(dialog.message());
      await dialog.accept();
    });

    await schemaRows(page).first().getByRole('button', { name: 'Retry' }).click();

    await expect(page).toHaveURL(/\/schema\?discovered=1#discovery$/);
    await expect(
      page
        .locator('#toast-container [role="alert"]')
        .filter({ hasText: 'Schema discovery started.' }),
    ).toBeVisible();
    await expect(page.locator(CARD)).toBeVisible();
    expect(confirmations.join(' ')).toContain('Retry anyway?');
  });

  // #90: the card is the only place a project-wide run reports progress, so it
  // has to refresh itself. The interval lives on the card and stops when the job
  // is terminal; the mock serves a running job, which is the state that polls.
  test('the card refreshes itself while a run is in flight', async ({ page }) => {
    await page.goto('/schema');

    const card = page.locator(CARD);
    await expect(card).toBeVisible();
    await expect(card).toHaveAttribute('data-testid', 'schema-discovery-card');
    await expect(card).toHaveAttribute('hx-get', '/schema/discovery/card');
    await expect(card).toHaveAttribute('hx-trigger', 'every 5s');
    // Pinned to the card: <main> carries hx-target="#main-content" and htmx
    // inherits it, which would swap the whole page on every tick.
    await expect(card).toHaveAttribute('hx-target', 'this');

    // The interval really fires, and the fragment endpoint answers it. A static
    // attribute would pass the assertions above and still leave the user with a
    // frozen card, so the request itself is the assertion.
    const poll = await page.waitForResponse(
      (resp) => resp.url().includes('/schema/discovery/card'),
      { timeout: 20_000 },
    );
    expect(poll.status(), 'the poll endpoint must render the fragment, not 404').toBe(200);
    await expect(page.locator(CARD)).toBeVisible();
  });
});
