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
    // The hero shows the generic product copy: a static title, and the default
    // subtitle because this link configures no welcome message.
    await expect(page.locator('#share-first-load-title')).toHaveText('Chat with Memory');
    await expect(page.locator('#share-first-load-desc')).toHaveText('Chat by text.');
    // The browser title mirrors the exchanged identity ("<agent> — Memory"),
    // not the generic "Shared chat — Memory" the server renders pre-exchange.
    await expect(page).toHaveTitle(`${AGENT_NAME} — Memory`);
    // The configured appearance (icon + color) is applied to the header avatar.
    const avatar = page.locator('#share-avatar');
    await expect(avatar.locator('.iconify')).toHaveClass(/lucide--database/);
    await expect(avatar.locator('div').first()).toHaveCSS('color', 'rgb(37, 99, 235)');
    // The mock seeds one active session for the (anonymous) visitor.
    await expect(
      page.getByTestId('share-session-row').filter({ hasText: 'Welcome chat' }),
    ).toBeVisible();
  });

  test('the session rail stays hidden while the exchange is pending (no flash) for showSessionList:false links', async ({ page }) => {
    // Hold the exchange response so we can observe the pre-config paint before
    // applyConfig runs. This asserts the flash the server-rendered `hidden`
    // class eliminates: the rail must already be hidden before the config that
    // would (or would not) reveal it has even arrived.
    let releaseExchange = () => {};
    const held = new Promise<void>((resolve) => {
      releaseExchange = resolve;
    });
    await page.route('**/share/api/exchange', async (route) => {
      const response = await route.fetch();
      await held;
      await route.fulfill({ response });
    });

    await openShare(page, 'no-rail-key');

    // Before the config response is released, the rail must already be hidden.
    await expect(page.getByTestId('share-session-rail')).toBeHidden();

    releaseExchange();

    // Once the config resolves with showSessionList:false, it stays hidden.
    await expect(page.locator('#share-agent-name')).toHaveText(AGENT_NAME);
    await expect(page.getByTestId('share-session-rail')).toBeHidden();
  });

  test('a session row renders, selecting it loads its transcript, and the archived filter switches the list', async ({ page }) => {
    await openShare(page, 'valid-key-123');

    const welcome = page.getByTestId('share-session-row').filter({ hasText: 'Welcome chat' });
    await expect(welcome).toBeVisible();
    await expect(
      page.getByTestId('share-session-row').filter({ hasText: 'Archived chat' }),
    ).toHaveCount(0);

    // Selecting the row loads and renders the full admin-history timeline
    // through the shared engine (GET /share/api/sessions/:id → items +
    // pending_approvals): user + assistant bubbles, tool chip, reasoning,
    // grounded Sources, and copy affordances.
    await welcome.click();
    const chatLog = page.getByTestId('share-chat-log');
    await expect(chatLog.locator('.chat-bubble-primary')).toContainText('What can you do?');
    await expect(chatLog.locator('.chat-bubble-neutral')).toContainText('I can help with tasks.');
    await expect(chatLog.locator('[data-tool="web_search"]')).toBeVisible();
    await expect(chatLog.locator('.memory-thinking')).toBeVisible();
    await expect(chatLog.locator('.memory-thinking-body')).toContainText('The visitor asked what I can do.');
    await expect(chatLog.getByTestId('chat-sources')).toBeVisible();
    await expect(chatLog.getByTestId('chat-sources-toggle')).toHaveText(/1 source/);
    await expect(chatLog.getByRole('button', { name: 'Copy message' })).toBeVisible();
    await expect(chatLog.getByRole('button', { name: 'Copy code' })).toBeVisible();
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

  test('streams a full admin-engine reply and surfaces an inline tool approval', async ({ page }) => {
    await openShare(page, 'valid-key-123');

    await page.getByTestId('share-input').fill('hello');
    await page.getByTestId('share-send').click();

    // Live SSE streaming is fully expressible in the mock: the gateway proxies
    // the upstream token frames and the assistant bubble accumulates them.
    await expect(page.locator('.chat-bubble-neutral')).toContainText('Hello from share!');

    // Markdown in the reply is rendered server-side: the gateway buffers the
    // token deltas and emits an `html` frame at completion, so **Hello** becomes
    // <strong>Hello</strong> in the assistant bubble.
    await expect(page.locator('.chat-bubble-neutral strong')).toContainText('Hello');

    // The live stream emits the full shared-engine frame set: a web_search tool
    // chip, a thinking block, and grounded Sources.
    await expect(page.locator('[data-tool="web_search"]')).toBeVisible();
    await expect(page.locator('.memory-thinking')).toBeVisible();
    await expect(page.getByTestId('chat-sources')).toBeVisible();

    // A tool-policy approval renders inline (approve / reject / cancel).
    const card = page.getByTestId('chat-approval-card');
    await expect(card).toBeVisible();
    await expect(card).toContainText('web_search');
    await expect(card.getByTestId('chat-approval-approve')).toBeVisible();
    await expect(card.getByTestId('chat-approval-reject')).toBeVisible();
    await expect(card.getByTestId('chat-approval-cancel')).toBeVisible();

    // Approve → the decision is posted and the card is removed.
    await card.getByTestId('chat-approval-approve').click();
    await expect(page.getByTestId('chat-approval-card')).toHaveCount(0);
  });

  test('rejecting an inline approval with a reason clears the card', async ({ page }) => {
    await openShare(page, 'valid-key-123');

    await page.getByTestId('share-input').fill('run this action');
    await page.getByTestId('share-send').click();

    const card = page.getByTestId('chat-approval-card');
    await expect(card).toBeVisible();
    // The reject button first reveals a reason box, then (as "Confirm reject")
    // posts the decision with the reason.
    await card.getByTestId('chat-approval-reject').click();
    await card.getByPlaceholder('Reason (optional)').fill('Not needed');
    await card.getByTestId('chat-approval-reject').click();
    await expect(page.getByTestId('chat-approval-card')).toHaveCount(0);
  });

  test('cancelling an inline approval clears the card', async ({ page }) => {
    await openShare(page, 'valid-key-123');

    await page.getByTestId('share-input').fill('another action');
    await page.getByTestId('share-send').click();

    const card = page.getByTestId('chat-approval-card');
    await expect(card).toBeVisible();
    await card.getByTestId('chat-approval-cancel').click();
    await expect(page.getByTestId('chat-approval-card')).toHaveCount(0);
  });

  test('a paused ask_user question renders the inline question card from history', async ({ page }) => {
    await openShare(page, 'valid-key-123');

    const qrow = page.getByTestId('share-session-row').filter({ hasText: 'Question chat' });
    await expect(qrow).toBeVisible();
    await qrow.click();

    // The seeded timeline carries an ask_user tool_call (paused run); the shared
    // engine renders it as the interactive question card, not a tool chip.
    const card = page.getByTestId('chat-question-card');
    await expect(card).toBeVisible();
    await expect(card).toContainText('Which option?');
    await expect(card.getByTestId('chat-question-option')).toHaveCount(2);
  });

  test('a live ask_user pause renders the inline question card from the stream', async ({ page }) => {
    await openShare(page, 'valid-key-123');

    // The mock emits the real wire for a live pause: an ask_user MCP tool call.
    // The gateway must translate it into the client `question` frame (as it does
    // for /api/chat) — the raw tool call must not leak through as a chip.
    await page.getByTestId('share-input').fill('please ask me a question');
    await page.getByTestId('share-send').click();

    const card = page.getByTestId('chat-question-card');
    await expect(card).toBeVisible();
    await expect(card).toContainText('Which option do you want?');
    await expect(card.getByTestId('chat-question-option')).toHaveCount(2);
    await expect(page.locator('[data-tool="ask_user"]')).toHaveCount(0);
  });

  test('the rail menu archives, unarchives, and deletes a session', async ({ page }) => {
    await openShare(page, 'valid-key-123');

    // Archive "Welcome chat" from its row menu.
    const welcome = page.getByTestId('share-session-row').filter({ hasText: 'Welcome chat' });
    await welcome.getByRole('button', { name: 'Actions for Welcome chat' }).click();
    await welcome.getByTestId('share-session-archive').click();
    await expect(
      page.getByTestId('share-session-row').filter({ hasText: 'Welcome chat' }),
    ).toHaveCount(0);

    // Unarchive it from the archived list.
    await page.getByTestId('share-archived-filter').selectOption('archived');
    const archivedWelcome = page.getByTestId('share-session-row').filter({ hasText: 'Welcome chat' });
    await expect(archivedWelcome).toBeVisible();
    await archivedWelcome.getByRole('button', { name: 'Actions for Welcome chat' }).click();
    await archivedWelcome.getByTestId('share-session-unarchive').click();
    await page.getByTestId('share-archived-filter').selectOption('active');
    await expect(
      page.getByTestId('share-session-row').filter({ hasText: 'Welcome chat' }),
    ).toBeVisible();

    // Delete the archived "Archived chat" session.
    await page.getByTestId('share-archived-filter').selectOption('archived');
    const archivedChat = page.getByTestId('share-session-row').filter({ hasText: 'Archived chat' });
    await expect(archivedChat).toBeVisible();
    await archivedChat.getByRole('button', { name: 'Actions for Archived chat' }).click();
    await archivedChat.getByTestId('share-session-delete').click();
    await expect(
      page.getByTestId('share-session-row').filter({ hasText: 'Archived chat' }),
    ).toHaveCount(0);
  });

  test('a second browser context does not see the first visitor’s sessions', async ({ browser }) => {
    const ctxA = await browser.newContext();
    const ctxB = await browser.newContext();
    try {
      const pageA = await ctxA.newPage();
      await openShare(pageA, 'valid-key-123');
      await pageA.getByTestId('share-input').fill('alpha-unique-123');
      await pageA.getByTestId('share-send').click();
      await expect(pageA.locator('.chat-bubble-neutral')).toContainText('Hello from share!');
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

  test('a failed rail refresh shows an explicit error with a working retry', async ({ page }) => {
    await openShare(page, 'valid-key-123');
    // The rail is server-rendered, so rows (and their per-row menu) arrive with
    // the page. This is the state the old JSON fallback silently replaced.
    const welcome = page.getByTestId('share-session-row').filter({ hasText: 'Welcome chat' });
    await expect(welcome).toBeVisible();

    // Fail the next server-rendered rail partial. The post-stream refresh is the
    // deterministic trigger for it.
    await page.route('**/share/api/partial/sessions*', (route) => route.abort());
    await page.getByTestId('share-input').fill('hello');
    await page.getByTestId('share-send').click();

    // The failure is surfaced instead of swapping in a menu-less list (#1890).
    await expect(page.getByTestId('share-sessions-error')).toBeVisible();
    await expect(page.getByTestId('share-sessions-retry')).toBeVisible();

    // Retry with the upstream reachable again restores the server-rendered rail,
    // including the row menu that the fallback used to drop.
    await page.unroute('**/share/api/partial/sessions*');
    await page.getByTestId('share-sessions-retry').click();
    await expect(
      page.getByTestId('share-session-row').filter({ hasText: 'Welcome chat' }),
    ).toBeVisible();
    await expect(
      page.getByRole('button', { name: 'Actions for Welcome chat' }),
    ).toBeVisible();
  });

  test('a link with end-user approvals disabled renders question and approval cards read-only', async ({ page }) => {
    await openShare(page, 'no-approvals-key');
    await expectReadyShell(page);

    // Live path first, from a fresh chat: a streamed tool approval renders
    // read-only — the tool call stays visible, the decision buttons do not,
    // matching the server's 403 for the same link (#1892).
    await page.getByTestId('share-input').fill('run this action');
    await page.getByTestId('share-send').click();

    const approvalCard = page.getByTestId('chat-approval-card');
    await expect(approvalCard).toBeVisible();
    await expect(approvalCard.getByTestId('chat-approval-approve')).toHaveCount(0);
    await expect(approvalCard.getByTestId('chat-approval-reject')).toHaveCount(0);
    await expect(approvalCard.getByTestId('chat-approval-cancel')).toHaveCount(0);

    // History path: the seeded paused ask_user question renders its body but no
    // decision controls either.
    const questionRow = page.getByTestId('share-session-row').filter({ hasText: 'Question chat' });
    await expect(questionRow).toBeVisible();
    await questionRow.click();

    const questionCard = page.getByTestId('chat-question-card');
    await expect(questionCard).toBeVisible();
    await expect(questionCard).toContainText('Which option?');
    await expect(questionCard.getByTestId('chat-question-option')).toHaveCount(0);
    await expect(questionCard.getByTestId('chat-question-submit')).toHaveCount(0);
  });
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
    // The reveal URL renders as form.ClipboardCopy's readonly input; the full
    // URL (including the `#sh_key_…` fragment) is the input's value.
    await expect(revealed.locator('input')).toHaveValue(new RegExp(`#${key}$`));

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
    // The hero title is generic product copy — never the agent name. The
    // subtitle shows the owner-configured welcome message when one is set.
    await expect(page.locator('#share-first-load-title')).toHaveText('Chat with Memory');
    await expect(page.locator('#share-first-load-desc')).toHaveText('Welcome to the demo.');
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
    await expect(page.locator('.chat-bubble-neutral')).toContainText('Hello from share!');
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
