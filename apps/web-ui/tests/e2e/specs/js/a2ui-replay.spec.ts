import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Hermetic replay gate for #427: a persisted card-only turn (content.a2ui with
// no text) must render as a card in the shared timeline renderer and must not
// paint a placeholder assistant bubble. Loads the shipped scripts into a bare
// page — no gateway, no memory API, no auth.

const CHAT_TRANSPORT_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/chat-transport.js',
);
const CHAT_COMPONENTS_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/chat-components.js',
);
const CHAT_HOST_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/chat-host.js',
);
const CHAT_STREAM_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/chat-stream.js',
);

const SURFACE_ID = 'replay-surface';
const CARD_BODIES = '#chat-messages .chat.chat-start.memory-rise .card-body';

const CARD_ONLY_ITEM = {
  kind: 'message',
  role: 'agent',
  step_number: 1,
  created_at: '2026-09-30T10:00:00.000Z',
  content: {
    a2ui: [
      { createSurface: { surfaceId: SURFACE_ID, catalogId: 'memory-basic' } },
      {
        updateComponents: {
          surfaceId: SURFACE_ID,
          components: [
            {
              id: 'result-1',
              component: 'result',
              rows: [{ label: 'status', value: 'REPLAY_CARD_SENTINEL' }],
            },
          ],
        },
      },
    ],
  },
};

async function loadScripts(page: Page): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));
  await page.setContent(
    '<!doctype html><html><head><meta charset="utf-8" /></head><body>' +
      '<div id="chat-messages"></div></body></html>',
  );
  await page.addScriptTag({ path: CHAT_TRANSPORT_JS });
  await page.addScriptTag({ path: CHAT_COMPONENTS_JS });
  await page.addScriptTag({ path: CHAT_HOST_JS });
  await page.addScriptTag({ path: CHAT_STREAM_JS });
  return errors;
}

test.describe('A2UI history replay (chat-components.js)', () => {
  test('renders a card-only turn without a placeholder assistant bubble', async ({ page }) => {
    const errors = await loadScripts(page);

    const assistantBubbles = await page.evaluate((item) => {
      const container = document.getElementById('chat-messages') as HTMLElement;
      let bubbles = 0;
      const ctx = {
        container,
        badgeCtx: { messages: container, hideEmpty() {}, scrollToBottom() {} },
        getAgentName: () => 'Memory',
        begin() {},
        addUserMessage() {},
        addAssistantMessage() {
          bubbles += 1;
          return null;
        },
        toolChip() {},
        renderHistoryQuestion() {},
        renderThinkingBlock() {},
        isThinkingItem() {
          return false;
        },
      };
      (window as unknown as {
        MemoryChatComponents: {
          renderTimeline: (items: unknown[], ctx: unknown, flags: unknown) => void;
        };
      }).MemoryChatComponents.renderTimeline([item], ctx, {});
      return bubbles;
    }, CARD_ONLY_ITEM);

    // The card sits where the turn occurred, with its real content.
    const cardBody = page.locator(CARD_BODIES).filter({ hasText: 'REPLAY_CARD_SENTINEL' });
    await expect(cardBody).toHaveCount(1);
    await expect(cardBody.getByText('status', { exact: true })).toBeVisible();

    // No text was stored for the card-only turn, so no empty assistant bubble.
    expect(assistantBubbles, 'card-only turn painted a text bubble').toBe(0);
    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });
});
