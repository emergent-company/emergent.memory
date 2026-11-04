import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Hermetic DOM spec for Paseo-style message styling: assistant turns are plain
// full-width markdown (no bubble/avatar/name header), user turns keep a
// right-aligned bubble without a "You" label, copy lives in the assistant
// action row, and Sources mount inside the assistant turn after the markdown.
const core = (name: string) =>
  path.resolve(__dirname, '../../../../gateway/webui/static/js', name);

async function load(page: Page): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));
  await page.setContent('<!doctype html><html><body><div id="d"></div></body></html>');
  for (const name of ['chat-transport.js', 'chat-components.js', 'chat-host.js', 'chat-stream.js']) {
    await page.addScriptTag({ path: core(name) });
  }
  return errors;
}

async function render(page: Page): Promise<void> {
  await page.evaluate(() => {
    const w = window as unknown as {
      MemoryChatComponents: {
        attachSources: (wrap: Element, citations: unknown[]) => void;
      };
      MemoryChatStream: {
        createEngine: (ctx: unknown) => {
          addUserMessage: (t: string) => void;
          addAssistantMessage: (h: string, n: string) => HTMLElement;
        };
      };
    };
    const messages = document.getElementById('d') as HTMLElement;
    const badgeCtx = { messages, hideEmpty() {}, scrollToBottom() {} };
    const stream = w.MemoryChatStream.createEngine({
      messages,
      badgeCtx,
      hideEmpty() {},
      scrollToBottom() {},
      setStreaming() {},
      onStreamFinish() {},
      onStreamFail() {},
      currentAgentName: () => 'Memory',
      currentAgentUI: () => null,
    });
    stream.addUserMessage('hello');
    const reply = stream.addAssistantMessage('<p>plain <strong>markdown</strong></p>', 'Memory');
    w.MemoryChatComponents.attachSources(reply, [
      { id: '11111111-1111-4111-8111-111111111111', type: 'Thing', label: 'A source' },
    ]);
  });
}

test('assistant turns are plain markdown; user turns keep a labelled-free bubble', async ({ page }) => {
  const errors = await load(page);
  await render(page);

  const user = page.locator('#d .chat-end');
  await expect(user.locator('.chat-bubble.chat-bubble-primary')).toContainText('hello');
  await expect(user.locator('.chat-header')).toHaveCount(0);

  const assistant = page.locator('#d .chat-start.memory-assistant').last();
  await expect(assistant.locator('.chat-bubble')).toHaveCount(0);
  await expect(assistant.locator('.chat-header')).toHaveCount(0);
  await expect(assistant.locator('.chat-image')).toHaveCount(0);
  await expect(assistant.locator('> .memory-md')).toContainText('plain markdown');

  // Copy is in the action/footer row, never the removed header.
  const copy = assistant.locator('.memory-assistant-actions .memory-copy-msg');
  await expect(copy).toHaveCount(1);
  await expect(copy).toHaveAttribute('aria-label', 'Copy message');

  // Sources are a sibling after the markdown inside the assistant turn.
  const sources = assistant.locator('[data-testid="chat-sources"]');
  await expect(sources).toHaveCount(1);
  const order = await assistant.evaluate((el) => {
    const md = el.querySelector('.memory-md');
    const src = el.querySelector('[data-testid="chat-sources"]');
    if (!md || !src) return null;
    return !!(md.compareDocumentPosition(src) & Node.DOCUMENT_POSITION_FOLLOWING);
  });
  expect(order).toBe(true);
  expect(errors).toEqual([]);
});
