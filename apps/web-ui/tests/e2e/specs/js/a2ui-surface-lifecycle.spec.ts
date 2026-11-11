import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Hermetic gate for two A2UI renderer regressions found in the same review:
//
//  1. A history replay that carries more than one card answer made the later
//     answer delete the earlier card. Both answers named their surface "s1"
//     (the prompt teaches that id), and the renderer keyed its surface registry
//     by surfaceId alone across the whole transcript, so replaying the second
//     card "updated" the first one's nodes in place. Only the newest card
//     survived a reload.
//  2. A card that streamed in was silent to assistive tech — the transcript is
//     deliberately not a live region, and nothing announced the arrival.
//
// Loads the shipped scripts into a bare page like a2ui-replay.spec.ts: no
// gateway, no memory API, no auth.

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

const CARD_BODIES = '#chat-messages .chat.chat-start.memory-rise .card-body';

// cardTurn builds one persisted agent turn carrying a single-card surface. Both
// turns intentionally reuse the same surfaceId: that is the collision under test.
function cardTurn(step: number, sentinel: string) {
  return {
    kind: 'message',
    role: 'agent',
    step_number: step,
    created_at: `2026-09-30T10:00:0${step}.000Z`,
    content: {
      a2ui: [
        { createSurface: { surfaceId: 's1', catalogId: 'memory-basic' } },
        {
          updateComponents: {
            surfaceId: 's1',
            components: [
              {
                id: `result-${step}`,
                component: 'result',
                rows: [{ label: 'status', value: sentinel }],
              },
            ],
          },
        },
      ],
    },
  };
}

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

// replayItems renders items through the shipped timeline renderer, the same
// entry point chat.js, the side panel and the share page use for history.
async function replayItems(page: Page, items: unknown[]) {
  return page.evaluate((list) => {
    const container = document.getElementById('chat-messages') as HTMLElement;
    const ctx = {
      container,
      badgeCtx: { messages: container, hideEmpty() {}, scrollToBottom() {} },
      getAgentName: () => 'Memory',
      begin() {},
      addUserMessage() {},
      addAssistantMessage() {
        return null;
      },
      toolChip() {},
      renderHistoryQuestion() {},
      renderThinkingBlock() {},
      isThinkingItem() {
        return false;
      },
    };
    (
      window as unknown as {
        MemoryChatComponents: {
          renderTimeline: (items: unknown[], ctx: unknown, flags: unknown) => void;
        };
      }
    ).MemoryChatComponents.renderTimeline(list, ctx, {});
  }, items);
}

function surfaceFrame(surfaceId: string, components: unknown[]) {
  return [
    { createSurface: { surfaceId, catalogId: 'memory-basic' } },
    { updateComponents: { surfaceId, components } },
  ];
}

test.describe('A2UI surfaces (chat-components.js)', () => {
  test('replays every card answer even when two turns reuse a surfaceId', async ({ page }) => {
    const errors = await loadScripts(page);
    await replayItems(page, [cardTurn(1, 'FIRST_CARD'), cardTurn(2, 'SECOND_CARD')]);

    await expect(page.locator(CARD_BODIES).filter({ hasText: 'FIRST_CARD' })).toHaveCount(1);
    await expect(page.locator(CARD_BODIES).filter({ hasText: 'SECOND_CARD' })).toHaveCount(1);
    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('names each card as a navigable group', async ({ page }) => {
    await loadScripts(page);
    await replayItems(page, [cardTurn(1, 'FIRST_CARD')]);

    const card = page.locator('#chat-messages .memory-card').first();
    await expect(card).toHaveAttribute('role', 'group');
    await expect(card).toHaveAttribute('aria-label', 'Result card');
  });

  test('announces a card that streams in, once, and stays silent on replay', async ({ page }) => {
    await loadScripts(page);

    // Live path: progressive frames for one surface, no per-item surface key.
    const announced = await page.evaluate(
      ({ frame, later }) => {
        const container = document.getElementById('chat-messages') as HTMLElement;
        const ctx = { messages: container, hideEmpty() {}, scrollToBottom() {} };
        const api = (
          window as unknown as {
            MemoryChatComponents: {
              renderA2UISurface: (id: string, msgs: unknown, ctx: unknown) => void;
            };
          }
        ).MemoryChatComponents;
        api.renderA2UISurface('s1', frame, ctx);
        api.renderA2UISurface('s1', later, ctx);
        const node = document.querySelector('.memory-a2ui-announcer');
        return {
          text: node ? node.textContent : null,
          role: node ? node.getAttribute('role') : null,
          live: node ? node.getAttribute('aria-live') : null,
          announcers: document.querySelectorAll('.memory-a2ui-announcer').length,
          cards: document.querySelectorAll('#chat-messages .card-body').length,
        };
      },
      {
        frame: surfaceFrame('s1', [
          { id: 'r1', component: 'result', rows: [{ label: 'status', value: 'FRAME_ONE' }] },
        ]),
        later: surfaceFrame('s1', [
          { id: 'r1', component: 'result', rows: [{ label: 'status', value: 'FRAME_TWO' }] },
        ]),
      },
    );

    // One surface, updated in place, announced once as a polite status.
    expect(announced.cards).toBe(1);
    expect(announced.announcers).toBe(1);
    expect(announced.text).toBe('New card: Result card.');
    expect(announced.role).toBe('status');
    expect(announced.live).toBe('polite');
  });

  test('does not announce cards during history replay', async ({ page }) => {
    await loadScripts(page);
    await replayItems(page, [cardTurn(1, 'FIRST_CARD')]);

    await expect(page.locator(CARD_BODIES).filter({ hasText: 'FIRST_CARD' })).toHaveCount(1);
    await expect(page.locator('.memory-a2ui-announcer')).toHaveCount(0);
  });
});
