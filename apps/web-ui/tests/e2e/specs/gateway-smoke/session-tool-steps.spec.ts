import { test, expect, type Page } from '@playwright/test';

// A real gateway rendering a seeded recorded session (#27). Runs in the
// gateway-smoke suite, i.e. on the `dev -> main` promotion PR and on pushes to
// `main` — not on a feature PR.
//
// The session (`conv-tool-steps`) is seeded into the mock memory service this
// lane boots (`mock-memory.mjs` + `run-e2e.sh`): the real gateway binary, the
// real templ output, the shipped chat JS and AUTH_MODE=dev. The live dev stack
// has no API that writes run history, so this hermetic lane is where a
// recorded session can be seeded deterministically; it still catches the
// template↔JS wiring regressions the live suite would.
//
// One turn holds three `search-hybrid` calls. `/sessions/<id>` renders the
// recorded steps server-side (no JS) and `/chat?c=<id>` replays the same items
// through the chat JS, so the spec pins both surfaces to one reading: the
// category verb + subject, the collapsed outcome, the readable body, the raw
// payload behind a closed disclosure — and a turn with three calls as one run
// row stating the count, opening to three steps in execution order.

const SESSION_ID = 'conv-tool-steps';
const CALL_IDS = ['tc-steps-1', 'tc-steps-2', 'tc-steps-3'];
const LABEL = 'Searched memory · swiftui';
const OUTCOME = '2 records';

const sessionSteps = (page: Page) => page.getByTestId('session-tool-step');

test.describe('recorded session — tool activity', () => {
  test('the session page reads each call as a step, not as a payload', async ({ page }) => {
    await page.goto(`/sessions/${SESSION_ID}`);

    const steps = sessionSteps(page);
    await expect(steps).toHaveCount(3);

    const first = steps.first();
    await expect(first.getByTestId('session-tool-label')).toHaveText(LABEL);
    // The collapsed outcome sits with the step; it is not the payload.
    await expect(first.getByText(OUTCOME, { exact: true })).toBeVisible();
    // The readable body is the result itself, title plus detail per record.
    await expect(first.locator('ul li')).toHaveCount(2);
    await expect(first.locator('ul li').nth(0)).toContainText('Alice');
    await expect(first.locator('ul li').nth(0)).toContainText('Engineer');
    await expect(first.locator('ul li').nth(1)).toContainText('retrieval');
    await expect(first.locator('ul li').nth(1)).toContainText('How answers are found');

    // Execution order: the run's cards carry their call ids in sequence.
    for (const [i, id] of CALL_IDS.entries()) {
      await expect(steps.nth(i).locator(`[title="${id}"]`)).toHaveCount(1);
    }
  });

  test('the raw payload stays closed until the disclosure is opened', async ({ page }) => {
    await page.goto(`/sessions/${SESSION_ID}`);

    const card = sessionSteps(page).first();
    const raw = card.locator('details').filter({ hasText: 'Show raw JSON' });
    await expect(raw).toHaveCount(1);
    await expect(raw).toHaveJSProperty('open', false);

    const output = raw.locator('pre');
    await expect(output).toBeHidden();

    await raw.locator('summary').click();
    await expect(raw).toHaveJSProperty('open', true);
    await expect(output).toBeVisible();
    await expect(output).toContainText('"total": 2');
  });

  test('three calls in one turn are one run row that opens to three ordered steps', async ({ page }) => {
    await page.goto(`/chat?c=${SESSION_ID}`);

    const run = page.locator('details.memory-tool-run');
    await expect(run).toHaveCount(1);
    await expect(run).toHaveAttribute('data-tool-count', '3');
    await expect(run).toHaveJSProperty('open', false);

    const summary = run.locator('summary');
    await expect(summary).toContainText('3 tool calls');
    await expect(summary).toContainText('Search hybrid ×3');

    await summary.click();
    await expect(run).toHaveJSProperty('open', true);

    const chips = run.locator('.memory-tool-run-body .memory-tool-chip');
    await expect(chips).toHaveCount(3);
    for (const [i, id] of CALL_IDS.entries()) {
      await expect(chips.nth(i)).toHaveAttribute('data-call-id', id);
      await expect(chips.nth(i).locator('.memory-badge-label')).toHaveText(LABEL);
      await expect(chips.nth(i).locator('.memory-badge-secondary')).toHaveText(OUTCOME);
    }
  });

  test('the replay words the call exactly as the recorded page does', async ({ page }) => {
    await page.goto(`/sessions/${SESSION_ID}`);
    const card = sessionSteps(page).first();
    const sessionLabel = (await card.getByTestId('session-tool-label').textContent())?.trim();
    const sessionOutcome = (await card.getByText(OUTCOME, { exact: true }).textContent())?.trim();

    await page.goto(`/chat?c=${SESSION_ID}`);
    const run = page.locator('details.memory-tool-run');
    await expect(run).toHaveCount(1);
    await run.locator('summary').click();

    const chip = run.locator('.memory-tool-run-body .memory-tool-chip').first();
    await expect(chip).toBeVisible();
    await expect(chip.locator('.memory-badge-label')).toHaveText(sessionLabel || '');
    await expect(chip.locator('.memory-badge-secondary')).toHaveText(sessionOutcome || '');

    // Guard the comparison itself: the shared reading is the contract's, not
    // two empty strings agreeing.
    expect(sessionLabel).toBe(LABEL);
    expect(sessionOutcome).toBe(OUTCOME);
  });
});
