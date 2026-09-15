import { Page, expect } from '@playwright/test';

// Assistant reply bubbles stream into #chat-messages as
// .chat.chat-start > .chat-bubble.chat-bubble-neutral > .memory-md
// (chat-stream.js addAssistantMessage / openAssistantBubble). While waiting
// for the first token the bubble shows a .memory-typing indicator; while
// tokens stream a .memory-caret is appended; a stream failure injects
// <span class="text-error"> into .memory-md.
const ASSISTANT_MD = '#chat-messages .chat.chat-start .chat-bubble .memory-md';

/**
 * From an object detail page: click "Chat about object" and wait for the deep
 * link into the object's conversation (/chat?c=…&agent=…), then wait for the
 * composer. When no editor agent is configured the gateway redirects back to
 * /objects/{objectId} with an ?err= flash instead — surface that as an error.
 */
export async function startObjectChat(page: Page, objectId: string): Promise<void> {
  await page.goto(`/objects/${objectId}`);
  await page.getByRole('link', { name: 'Chat about object' }).click();

  try {
    await page.waitForURL(/\/chat\?c=/, { timeout: 10_000 });
  } catch (err) {
    const u = new URL(page.url());
    if (u.pathname === `/objects/${objectId}` && u.searchParams.has('err')) {
      throw new Error(
        `startObjectChat: chat about object ${objectId} redirected back with an error ` +
          `(${u.searchParams.get('err')}) — is an agent configured?`,
        { cause: err },
      );
    }
    throw new Error(
      `startObjectChat: did not reach the object conversation from /objects/${objectId}; ` +
        `landed on ${page.url()}`,
      { cause: err },
    );
  }

  await expect(page.locator('#chat-input')).toBeVisible();
}

/**
 * Send a chat message and wait for the assistant's reply to settle. Returns
 * the reply's trimmed inner text. The reply streams into the last assistant
 * bubble; a failure surfaces as an error marker (descendant span.text-error or
 * text matching /error|failed/i) inside the bubble and throws instead.
 */
export async function sendChatMessage(page: Page, message: string): Promise<string> {
  const input = page.locator('#chat-input');
  await expect(input).toBeEnabled();

  // A resumed (?c=) conversation loads its transcript asynchronously and the
  // history render wipes #chat-messages, which would destroy the streaming
  // bubble if we sent mid-render. Wait for that first render to land.
  try {
    await page.locator('#chat-messages .chat').first().waitFor({ state: 'visible', timeout: 5_000 });
  } catch {
    // No transcript bubbles yet (fresh/empty conversation) — nothing to wipe.
  }

  // Snapshot the assistant-bubble count so the poll below keys on the bubble
  // this send creates, not an earlier turn's.
  const baseline = await page.locator(ASSISTANT_MD).count();

  await input.fill(message);
  await page.locator('#chat-send').click();

  // Poll (NOT expect) so the wait can branch on bubble state: resolve when
  // the newest assistant bubble shows an error marker, or when it has
  // non-empty text and the turn has settled (typing indicator gone, stream
  // finished — #chat-send visible/enabled again and no streaming caret).
  let settled: { state: 'done' | 'error'; text: string } | null = null;
  try {
    const handle = await page.waitForFunction<
      { state: 'done' | 'error'; text: string } | null,
      { baseline: number }
    >(
      ({ baseline: b }: { baseline: number }) => {
        const nodes = document.querySelectorAll(
          '#chat-messages .chat.chat-start .chat-bubble .memory-md',
        );
        if (nodes.length <= b) return null; // this send's bubble isn't appended yet
        const el = nodes[b] as HTMLElement;
        const text = (el.textContent || '').trim();
        if (el.querySelector('span.text-error') || /error|failed/i.test(text)) {
          return { state: 'error', text };
        }
        const wrap = el.closest('.chat-bubble');
        const typing = wrap ? wrap.querySelector('.memory-typing') : null;
        const caret = wrap ? wrap.querySelector('.memory-caret') : null;
        const send = document.getElementById('chat-send');
        const streamEnded =
          !!send && !send.classList.contains('hidden') && !(send as HTMLButtonElement).disabled;
        if (text && !typing && !caret && streamEnded) {
          return { state: 'done', text };
        }
        return null;
      },
      { baseline },
      { timeout: 120_000 },
    );
    settled = await handle.jsonValue();
  } catch (err) {
    throw new Error(
      `sendChatMessage: the assistant reply did not settle within 120s ` +
        `(message: ${JSON.stringify(message)}); last URL ${page.url()}`,
      { cause: err },
    );
  }

  if (!settled) {
    throw new Error(`sendChatMessage: settle poll resolved without a bubble state`);
  }
  if (settled.state === 'error') {
    throw new Error(
      `sendChatMessage: the assistant reply shows an error marker: ${JSON.stringify(settled.text)}`,
    );
  }
  return settled.text;
}
