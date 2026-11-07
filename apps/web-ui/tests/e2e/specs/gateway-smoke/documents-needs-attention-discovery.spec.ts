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
// flight for the card, the retry route, and the start/cancel routes the card's
// own actions post to, so all of it is exercised here against a real gateway
// render. The fixture-contract case at the bottom checks that the mock's status
// vocabulary is the server's (see #97).

const PANEL = '[data-testid="processing-problems"]';
const CARD = '[data-testid="schema-discovery-card"]';

// Fixture-contract assertions read the mock directly; gateway-smoke.config.ts
// exports the resolved MOCK_PORT to the test process. No numeric fallback: a
// suite run outside its config would otherwise hit a stale pinned origin.
const mockPort = process.env.MOCK_PORT;
if (!mockPort) {
  throw new Error('MOCK_PORT is required; run this suite through gateway-smoke.config.ts (run-e2e.sh sets it).');
}
const mockOrigin = `http://localhost:${mockPort}`;

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
  // is terminal; the mock seeds a non-terminal run, which is the state that
  // polls. This test seeds that state through the card's own start action, so it
  // cannot depend on what an earlier case left behind.
  test('the card refreshes itself while a run is in flight', async ({ page }) => {
    await page.goto('/schema');

    const card = page.locator(CARD);
    await expect(card).toBeVisible();
    await card.getByTestId('schema-discovery-start').click();
    await expect(page).toHaveURL(/\/schema\?discovered=1#discovery$/);

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

  // #97: before the mock had start and cancel routes, the card's "Discover
  // entity types" and "Cancel run" buttons were dead ends, so no spec could
  // cover cancel — nor the start → non-terminal → terminal sequence #90's
  // interval exists for. The statuses here are the server's: the in-flight run
  // is analyzing_documents (the card renders "Running" for any non-terminal
  // job), and a cancel lands the terminal "cancelled".
  test('starting a run then cancelling it lands a terminal state', async ({ page }) => {
    await page.goto('/schema');

    const card = page.locator(CARD);
    await expect(card).toBeVisible();

    await card.getByTestId('schema-discovery-start').click();
    await expect(page).toHaveURL(/\/schema\?discovered=1#discovery$/);
    await expect(
      page
        .locator('#toast-container [role="alert"]')
        .filter({ hasText: 'Schema discovery started.' }),
    ).toBeVisible();

    // Non-terminal: the card's badge says "Running" and the copy is the run's
    // progress message, and the card is polling.
    const state = page.getByTestId('schema-discovery-state');
    await expect(state).toBeVisible();
    await expect(state.locator('.badge')).toHaveText('Running');
    await expect(state).toContainText('Analysing batch 1 of 2');
    await expect(card).toHaveAttribute('hx-trigger', 'every 5s');

    await card.getByTestId('schema-discovery-cancel').click();
    await expect(page).toHaveURL(/\/schema\?cancelled=1#discovery$/);
    await expect(
      page
        .locator('#toast-container [role="alert"]')
        .filter({ hasText: 'Discovery run cancelled. Its results were discarded.' }),
    ).toBeVisible();

    // Terminal: the card renders the server's status verbatim, says the run was
    // cancelled, and stops polling because a terminal job has no interval.
    await expect(state.locator('.badge')).toHaveText('cancelled');
    await expect(state).toContainText('The last run was cancelled.');
    await expect(card).not.toHaveAttribute('hx-trigger');
    await expect(card.getByTestId('schema-discovery-cancel')).toHaveCount(0);
  });

  // The fixtures themselves, not the render: a spec that copies the fixture for
  // a status-dependent path must not be trusting a value memory never emits.
  // Selecting by project id keeps this stateless — the throwaway ids below
  // never touch p1, which the browser cases above drive.
  test('the mock discovery fixtures use the server status vocabulary', async ({ request }) => {
    const inFlight = await request.get(
      `${mockOrigin}/api/discovery-jobs/projects/p-discovery-fixture`,
    );
    expect(inFlight.ok()).toBeTruthy();
    expect((await inFlight.json())[0].status).toBe('analyzing_documents');

    // The terminal seed is selectable by project id, without driving a run.
    const terminal = await request.get(
      `${mockOrigin}/api/discovery-jobs/projects/p-discovery-terminal`,
    );
    expect(terminal.ok()).toBeTruthy();
    expect((await terminal.json())[0].status).toBe('cancelled');

    // Start answers the shape the gateway decodes (DiscoveryStartResult) and
    // leaves a non-terminal run; cancel lands the terminal state.
    const start = await request.post(
      `${mockOrigin}/api/discovery-jobs/projects/p-discovery-contract/start`,
      { data: { scope: 'project' } },
    );
    expect(start.status()).toBe(200);
    const started = await start.json();
    expect(started).toEqual({
      job_id: expect.any(String),
      document_count: 8,
      total_documents: 8,
    });

    const afterStart = await request.get(
      `${mockOrigin}/api/discovery-jobs/projects/p-discovery-contract`,
    );
    expect((await afterStart.json())[0].status).toBe('analyzing_documents');

    const cancel = await request.delete(`${mockOrigin}/api/discovery-jobs/${started.job_id}`);
    expect(cancel.status()).toBe(200);
    expect((await cancel.json()).message).toContain('cancelled');

    const afterCancel = await request.get(
      `${mockOrigin}/api/discovery-jobs/projects/p-discovery-contract`,
    );
    expect((await afterCancel.json())[0].status).toBe('cancelled');
  });
});
