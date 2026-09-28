import { test, expect } from '@playwright/test';
import { createAgentViaModal } from '../../helpers/agents';

// Regression coverage for the hx-boosted chat navigation bug: the chat page's
// client scripts used to load INSIDE the swapped #chat-root fragment, so htmx
// re-executed them in-fragment in a racy order relative to the swap — chat.js's
// init() found the stale pre-swap root (already data-ready) and returned early,
// leaving the swapped-in root uninitialized (no submit handler, no rail resize
// grip). The fix loads the scripts once from the shell and re-inits chat.js on
// htmx:after:swap, so a boosted navigation to /chat re-binds the composer and
// the session-rail grip without a native full reload.
//
// Runs under the serial `mutations` project (the `-ui.spec.ts` suffix), and
// never starts an LLM run — it only proves the composer + rail grip are live
// after the in-place navigation.

test('boosted navigation to /chat re-initializes the composer and rail grip', async ({ page }) => {
  test.setTimeout(120_000);
  const name = `E2E Chat Bootstrap ${Date.now()}`;
  let id = '';

  try {
    id = await createAgentViaModal(page, name);

    await page.goto('/chat');
    await expect(page.getByTestId('chat-input')).toBeVisible();

    // Full page load booted the chat root.
    await expect(page.locator('#chat-root')).toHaveAttribute('data-ready', '1');

    // Click "New chat" — an hx-boosted link to /chat. This swaps #main-content
    // in place; the swapped #chat-root must be re-initialized (not dead).
    await page.locator('[data-testid="new-chat"]').click();

    // The swapped root is re-bound: data-ready set, rail resize grip present.
    await expect(page.locator('#chat-root')).toHaveAttribute('data-ready', '1');
    await expect(page.locator('#chat-rail-resize')).toHaveCount(1);

    // The composer is live: the navigation was in-place, so the URL is still
    // exactly /chat — no native `GET /chat?` full reload.
    await expect(page).toHaveURL(/\/chat$/);
  } finally {
    if (id) await page.request.delete(`/api/agents/${id}`).catch(() => {});
  }
});
