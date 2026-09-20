import { test, expect, type Locator, type Page } from '@playwright/test';

// Public agent-share surface (gateway /share/agent + /share/api/* and the
// owner /agents/:id/share surface). Runs against the dev-mode (no-auth) mock
// harness (see share.config.ts): mock-memory.mjs serves the share upstream and
// the gateway (AUTH_MODE=dev) sits in front. No OIDC session — the public page
// is anonymous by design, and the owner page is open in dev mode.
//
// The share key rides the URL fragment and is exchanged once for an HttpOnly
// cookie; every later /share/api/* call carries that cookie. The mock keys
// sessions by the X-End-User-Ref header, so a fresh browser context never sees
// another visitor's sessions. The exchange config (agentName/description) is
// applied to the header by share-agent.js, and GET /share/api/config rehydrates
// it from the cookie on a plain refresh (no fragment).

const PUBLIC = '/share/agent';
const OWNER = '/agents/agent-1/share';
const AGENT_NAME = 'Memory';

/** The ready chat shell: root, header, composer, and session rail. */
async function expectReadyShell(page: Page): Promise<void> {
  await expect(page).not.toHaveURL(/\/auth\/login/);
  await expect(page.getByTestId('share-page')).toBeVisible();
  await expect(page.locator('header')).toBeVisible();
  await expect(page.getByTestId('share-composer')).toBeVisible();
  await expect(page.getByTestId('share-input')).toBeVisible();
  await expect(page.getByTestId('share-send')).toBeVisible();
  await expect(page.getByTestId('share-session-rail')).toBeVisible();
}

/** Visit the public page with a key and wait for the shell to settle. */
async function openShare(page: Page, key?: string): Promise<void> {
  const target = key ? `${PUBLIC}#${key}` : PUBLIC;
  await page.goto(target);
}

/** Extract the share key from a row's one-time public URL (the key rides the fragment). */
async function keyFromRow(row: Locator): Promise<string> {
  const url = ((await row.locator('[data-share-link-url]').textContent()) ?? '').trim();
  const key = url.split('#')[1] ?? '';
  expect(key).toMatch(/^sh_key_\d+$/);
  return key;
}

/** The create-form toggles put their testid on the wrapping <label>; the checkbox sits inside. */
function createToggle(page: Page, testId: string): Locator {
  return page.getByTestId(testId).locator('input[type="checkbox"]');
}

/**
 * Open the create page the way an owner does: from the list page's "New share
 * link" call to action. The list has no inline form (it was split out).
 */
async function startCreate(page: Page): Promise<void> {
  await page.goto(OWNER);
  await page.getByTestId('share-new-link').click();
  await expect(page).toHaveURL(/\/agents\/agent-1\/share\/new$/);
  await expect(page.getByTestId('share-create-form')).toBeVisible();
}

test.describe('public share page', () => {
  test('loads without authentication (no login redirect) and resolves a bare visit to the invalid state', async ({ page }) => {
    await openShare(page);
    // The public surface must never redirect to an auth wall.
    await expect(page).not.toHaveURL(/\/auth\/login/);
    // No key fragment and no cookie → the client resolves the link to the
    // invalid terminal state (still the public document, not a login page).
    await expect(page.getByTestId('share-state-invalid')).toBeVisible();
  });

  test('a valid key exchange renders the shell and populates the agent header', async ({ page }) => {
    await openShare(page, 'valid-key-123');
    await expectReadyShell(page);
    // The exchange config is applied to the header identity.
    await expect(page.locator('#share-agent-name')).toHaveText(AGENT_NAME);
    await expect(page.locator('#share-agent-description')).toHaveText('A helpful assistant.');
    // The mock seeds one active session for the (anonymous) visitor.
    await expect(
      page.getByTestId('share-session-row').filter({ hasText: 'Welcome chat' }),
    ).toBeVisible();
  });

  test('a session row renders, selecting it loads its transcript, and the archived filter switches the list', async ({ page }) => {
    await openShare(page, 'valid-key-123');

    const welcome = page.getByTestId('share-session-row').filter({ hasText: 'Welcome chat' });
    await expect(welcome).toBeVisible();
    await expect(
      page.getByTestId('share-session-row').filter({ hasText: 'Archived chat' }),
    ).toHaveCount(0);

    // Selecting the row loads and renders the session transcript (the gateway
    // now relays messages through /share/api/sessions/:id).
    await welcome.click();
    const chatLog = page.getByTestId('share-chat-log');
    await expect(chatLog.locator('[data-role="user"]')).toContainText('What can you do?');
    await expect(chatLog.locator('[data-role="assistant"]')).toContainText('I can help with tasks.');
    await expect(page.getByTestId('share-first-load')).toBeHidden();

    // Archived filter swaps the list to the archived session.
    await page.getByTestId('share-archived-filter').selectOption('archived');
    await expect(
      page.getByTestId('share-session-row').filter({ hasText: 'Archived chat' }),
    ).toBeVisible();
    await expect(
      page.getByTestId('share-session-row').filter({ hasText: 'Welcome chat' }),
    ).toHaveCount(0);

    // "All" shows both.
    await page.getByTestId('share-archived-filter').selectOption('all');
    await expect(
      page.getByTestId('share-session-row').filter({ hasText: 'Welcome chat' }),
    ).toBeVisible();
    await expect(
      page.getByTestId('share-session-row').filter({ hasText: 'Archived chat' }),
    ).toBeVisible();
  });

  test('streams a reply and surfaces a pending approval with approve/deny', async ({ page }) => {
    await openShare(page, 'valid-key-123');

    await page.getByTestId('share-input').fill('hello');
    await page.getByTestId('share-send').click();

    // Live SSE streaming is fully expressible in the mock: the gateway proxies
    // the upstream token frames and the assistant bubble accumulates them.
    await expect(page.locator('[data-role="assistant"]')).toContainText('Hello from share!');

    // The just-created session owns one pending approval.
    const card = page.getByTestId('share-approval');
    await expect(card).toBeVisible();
    await expect(card).toContainText('Approve this action?');
    await expect(card.getByTestId('share-approval-approve')).toBeVisible();
    await expect(card.getByTestId('share-approval-deny')).toBeVisible();

    // Approve → the gateway posts the decision and the card is removed.
    await card.getByTestId('share-approval-approve').click();
    await expect(page.getByTestId('share-approval')).toHaveCount(0);
  });

  test('a second browser context does not see the first visitor’s sessions', async ({ browser }) => {
    const ctxA = await browser.newContext();
    const ctxB = await browser.newContext();
    try {
      const pageA = await ctxA.newPage();
      await openShare(pageA, 'valid-key-123');
      await pageA.getByTestId('share-input').fill('alpha-unique-123');
      await pageA.getByTestId('share-send').click();
      await expect(pageA.locator('[data-role="assistant"]')).toContainText('Hello from share!');
      // The first visitor's new chat is titled from its first message.
      await expect(
        pageA.getByTestId('share-session-row').filter({ hasText: 'alpha-unique-123' }),
      ).toBeVisible();

      const pageB = await ctxB.newPage();
      await openShare(pageB, 'valid-key-123');
      // Same link, fresh cookies: the second visitor sees only its own sessions.
      await expect(
        pageB.getByTestId('share-session-row').filter({ hasText: 'Welcome chat' }),
      ).toBeVisible();
      await expect(
        pageB.getByTestId('share-session-row').filter({ hasText: 'alpha-unique-123' }),
      ).toHaveCount(0);
    } finally {
      await ctxA.close();
      await ctxB.close();
    }
  });

  test('a returning visitor rehydrates the header from the cookie without the fragment', async ({ page }) => {
    await openShare(page, 'valid-key-123');
    await expect(page.locator('#share-agent-name')).toHaveText(AGENT_NAME);

    // Plain navigation without the fragment: the cookie-gated GET /share/api/config
    // rehydrates the header identity and the ready shell.
    await page.goto(PUBLIC);
    await expectReadyShell(page);
    await expect(page.locator('#share-agent-name')).toHaveText(AGENT_NAME);
    await expect(page.locator('#share-agent-description')).toHaveText('A helpful assistant.');
  });

  test('a revoked link rehydrates to the revoked terminal state', async ({ page }) => {
    // revoke-later-key passes the exchange, then the link is revoked — so the
    // config rehydrate on the next visit reveals the revoked terminal.
    await openShare(page, 'revoke-later-key');
    await expect(page.locator('#share-agent-name')).toHaveText(AGENT_NAME);

    await page.goto(PUBLIC); // no fragment → cookie-gated config → revoked
    await expect(page.getByTestId('share-state-revoked')).toBeVisible();
  });

  // Terminal exchange states, keyed by the mock's well-known keys.
  const TERMINAL_CASES: Array<[string, string]> = [
    ['revoked-key', 'share-state-revoked'],
    ['expired-key', 'share-state-expired'],
    ['rate-limit-key', 'share-state-rate-limited'],
    ['budget-key', 'share-state-budget-exceeded'],
    ['bad-key-123', 'share-state-invalid'],
  ];
  for (const [key, testId] of TERMINAL_CASES) {
    test(`an invalid or terminal key reveals the ${testId.replace('share-state-', '')} state (${key})`, async ({ page }) => {
      await openShare(page, key);
      await expect(page.getByTestId(testId)).toBeVisible();
      await expect(page.getByTestId('share-composer')).toHaveCount(0);
    });
  }
});

test.describe('owner share-link management', () => {
  test('creates a link, reveals/copies it, and revokes it', async ({ page }) => {
    // List page: no inline form, a prominent "New share link" call to action.
    await page.goto(OWNER);
    await expect(page.getByTestId('share-links-panel')).toBeVisible();
    await expect(page.getByTestId('share-create-form')).toHaveCount(0);
    await page.getByTestId('share-new-link').click();

    // Create page: the form lives here, reached from the list.
    await expect(page).toHaveURL(/\/agents\/agent-1\/share\/new$/);
    await expect(page.getByTestId('share-create-form')).toBeVisible();

    const label = `E2E Share ${Date.now()}`;
    await page.getByTestId('share-create-label').fill(label);
    await page.getByTestId('share-create-submit').click();

    // Success renders the list (no redirect) with the one-time URL revealed: the
    // new row carries it and an auto-open modal shows the same full link.
    const row = page.getByTestId('share-link-row').filter({ hasText: label });
    await expect(row).toBeVisible();
    const urlEl = row.locator('[data-share-link-url]');
    const createdUrl = ((await urlEl.textContent()) ?? '').trim();
    expect(createdUrl).toContain('/share/agent#sh_key_');
    const key = createdUrl.split('#')[1];
    expect(key).toMatch(/^sh_key_\d+$/);

    const revealed = page.getByTestId('share-link-reveal-url');
    await expect(revealed).toBeVisible();
    await expect(revealed).toContainText(key);

    // Reload: the key is not recoverable at list time, so the copy action now
    // reveals it on demand — and returns the same key.
    await page.goto(OWNER);
    const relisted = page.getByTestId('share-link-row').filter({ hasText: label });
    await expect(relisted).toBeVisible();
    await relisted.getByTestId('share-copy-link').click();
    await expect(relisted.locator('[data-share-link-url]')).toContainText(key);

    // Revoke: the row flips to the revoked status and the flash confirms it.
    page.once('dialog', (d) => d.accept());
    await relisted.getByTestId('share-revoke-link').click();
    const revokedRow = page.getByTestId('share-link-row').filter({ hasText: label });
    await expect(revokedRow.getByTestId('share-link-status')).toContainText('Revoked');
    await expect(page.getByTestId('share-links-flash')).toContainText('Share link revoked');
  });
});

test.describe('owner share-link options', () => {
  test('create a link with all options and verifies each is reflected', async ({ page }) => {
    await startCreate(page);

    const label = `E2E Options ${Date.now()}`;
    await page.getByTestId('share-create-label').fill(label);
    await page.getByTestId('share-create-expiry').selectOption('90');
    await page.getByTestId('share-create-welcome').fill('Welcome to the demo.');

    // Tick both tool checkboxes by stable tool id, not list order (agent tools:
    // web_search, memory_write).
    await page.locator('[data-testid="share-create-tool"][value="web_search"]').check();
    await page.locator('[data-testid="share-create-tool"][value="memory_write"]').check();

    await createToggle(page, 'share-create-require-email').check();
    await createToggle(page, 'share-create-show-sessions').uncheck();
    await createToggle(page, 'share-create-sandbox').check();

    await page.getByTestId('share-create-max-sessions').fill('3');
    await page.getByTestId('share-create-budget-messages').fill('50');
    await page.getByTestId('share-create-budget-tokens').fill('10000');

    await page.getByTestId('share-create-submit').click();

    const row = page.getByTestId('share-link-row').filter({ hasText: label });
    await expect(row).toBeVisible();
    await expect(row.getByTestId('share-link-status')).toContainText('Active');

    // Numeric caps are only rendered when the value is > 0.
    await expect(row).toContainText('3 sessions / person');
    await expect(row).toContainText('50 msg budget');
    await expect(row).toContainText('10000 token budget');

    // 90-day expiry → the Expires metadata shows a relative time, not "Never".
    await expect(row.getByTestId('share-link-expires')).not.toContainText('Never');

    // Behaviour chips render their label whether on or off.
    await expect(row).toContainText('Requires email');
    await expect(row).toContainText('Sandbox');

    // Prove the create-form options reach the public page end-to-end.
    const key = await keyFromRow(row);
    await page.goto(PUBLIC + '#' + key);
    await expect(page.getByTestId('share-email-gate')).toBeVisible();
    await expect(page.locator('#share-first-load-desc')).toContainText('Welcome to the demo.');
  });

  test('a link created with never expiry shows Never', async ({ page }) => {
    await startCreate(page);

    const label = `E2E Never ${Date.now()}`;
    await page.getByTestId('share-create-label').fill(label);
    await page.getByTestId('share-create-expiry').selectOption('never');
    await page.getByTestId('share-create-submit').click();

    const row = page.getByTestId('share-link-row').filter({ hasText: label });
    await expect(row).toBeVisible();
    await expect(row.getByTestId('share-link-expires')).toContainText('Never');
  });

  test('rotating a link issues a new key', async ({ page }) => {
    await startCreate(page);

    const label = `E2E Rotate ${Date.now()}`;
    await page.getByTestId('share-create-label').fill(label);
    await page.getByTestId('share-create-submit').click();

    const row = page.getByTestId('share-link-row').filter({ hasText: label });
    await expect(row).toBeVisible();
    const before = await keyFromRow(row);

    // Reload: the key is no longer recoverable at list time, so the row's URL
    // is empty until rotate reveals the fresh key.
    await page.goto(OWNER);
    const relisted = page.getByTestId('share-link-row').filter({ hasText: label });
    await expect(relisted).toBeVisible();

    page.once('dialog', (d) => d.accept());
    await relisted.getByTestId('share-rotate-link').click();

    // Rotation re-renders the page with the new one-time URL in the fragment.
    const rotatedRow = page.getByTestId('share-link-row').filter({ hasText: label });
    await expect(rotatedRow).toBeVisible();
    await expect(rotatedRow.getByTestId('share-link-status')).toContainText('Active');
    const after = await keyFromRow(rotatedRow);
    expect(after).toMatch(/^sh_key_\d+$/);
    expect(after).not.toBe(before);
  });
});

test.describe('public share page (owner-created links)', () => {
  test('a requireEmail link gates the composer until an email is entered', async ({ page }) => {
    await startCreate(page);

    const label = `E2E EmailGate ${Date.now()}`;
    await page.getByTestId('share-create-label').fill(label);
    await createToggle(page, 'share-create-require-email').check();
    await page.getByTestId('share-create-submit').click();

    const row = page.getByTestId('share-link-row').filter({ hasText: label });
    await expect(row).toBeVisible();
    const key = await keyFromRow(row);

    await page.goto(PUBLIC + '#' + key);
    await expect(page.getByTestId('share-email-gate')).toBeVisible();
    await expect(page.getByTestId('share-input')).toBeDisabled();

    await page.getByTestId('share-email-input').fill('visitor@example.com');
    await page.getByTestId('share-email-submit').click();

    await expect(page.getByTestId('share-email-gate')).toBeHidden();
    await expect(page.getByTestId('share-input')).toBeEnabled();

    await page.getByTestId('share-input').fill('hi');
    await page.getByTestId('share-send').click();
    await expect(page.locator('[data-role="assistant"]')).toContainText('Hello from share!');
  });

  test('showSessionList off hides the session rail', async ({ page }) => {
    await startCreate(page);

    const label = `E2E NoRail ${Date.now()}`;
    await page.getByTestId('share-create-label').fill(label);
    await createToggle(page, 'share-create-show-sessions').uncheck();
    await page.getByTestId('share-create-submit').click();

    const row = page.getByTestId('share-link-row').filter({ hasText: label });
    await expect(row).toBeVisible();
    const key = await keyFromRow(row);

    await page.goto(PUBLIC + '#' + key);
    await expect(page.locator('header')).toBeVisible();
    await expect(page.getByTestId('share-session-rail')).toBeHidden();
  });
});
