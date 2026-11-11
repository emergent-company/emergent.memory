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

// createOnly is the shape of the *first* live frame for a surface: the
// progressive parser yields each JSONL line as it completes, so createSurface
// reaches the renderer on its own, with no components yet.
function createOnly(surfaceId: string) {
  return [{ createSurface: { surfaceId, catalogId: 'memory-basic' } }];
}

test.describe('A2UI surfaces (chat-components.js)', () => {
  test('replays every card answer even when two turns reuse a surfaceId', async ({ page }) => {
    const errors = await loadScripts(page);
    await replayItems(page, [cardTurn(1, 'FIRST_CARD'), cardTurn(2, 'SECOND_CARD')]);

    await expect(page.locator(CARD_BODIES).filter({ hasText: 'FIRST_CARD' })).toHaveCount(1);
    await expect(page.locator(CARD_BODIES).filter({ hasText: 'SECOND_CARD' })).toHaveCount(1);
    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('names each card as a group', async ({ page }) => {
    await loadScripts(page);
    await replayItems(page, [cardTurn(1, 'FIRST_CARD')]);

    const card = page.locator('#chat-messages .memory-card').first();
    await expect(card).toHaveAttribute('role', 'group');
    await expect(card).toHaveAttribute('aria-label', 'Result card');
  });

  test('keeps two live turns apart when the server scopes their surface ids', async ({ page }) => {
    await loadScripts(page);

    // #592 on the live path: the model names both surfaces "s1", and the server
    // scopes each to its run+step, so the renderer receives two distinct ids.
    // Keyed by surfaceId alone, two distinct ids are two cards; the same id
    // twice (the pre-fix wire shape) would leave one card, which is the bug.
    const result = await page.evaluate(
      ({ one, two }) => {
        const container = document.getElementById('chat-messages') as HTMLElement;
        const ctx = { messages: container, hideEmpty() {}, scrollToBottom() {} };
        const api = (
          window as unknown as {
            MemoryChatComponents: {
              renderA2UISurface: (id: string, msgs: unknown, ctx: unknown) => void;
            };
          }
        ).MemoryChatComponents;
        api.renderA2UISurface('run-1.0:s1', one, ctx);
        api.renderA2UISurface('run-2.0:s1', two, ctx);
        const node = document.querySelector('.memory-a2ui-announcer');
        return {
          cards: document.querySelectorAll('#chat-messages .card-body').length,
          texts: Array.from(
            document.querySelectorAll('#chat-messages .card-body'),
          ).map((el) => el.textContent),
          announced: node ? node.textContent : null,
        };
      },
      {
        one: surfaceFrame('run-1.0:s1', [
          { id: 'r1', component: 'result', rows: [{ label: 'status', value: 'FIRST' }] },
        ]),
        two: surfaceFrame('run-2.0:s1', [
          { id: 'r2', component: 'result', rows: [{ label: 'status', value: 'SECOND' }] },
        ]),
      },
    );

    expect(result.cards).toBe(2);
    expect(result.texts.join(' | ')).toContain('FIRST');
    expect(result.texts.join(' | ')).toContain('SECOND');
    expect(result.announced).toBe('New card 2: Result card.');
  });

  test('announces a card that streams in, once, and stays silent on replay', async ({ page }) => {
    await loadScripts(page);

    // The real live shape: createSurface arrives alone in the first frame (the
    // parser yields one JSONL line at a time), and the components follow in the
    // next. The createSurface-only frame must not "use up" the announcement.
    const announced = await page.evaluate(
      ({ created, frame, later }) => {
        const container = document.getElementById('chat-messages') as HTMLElement;
        const ctx = { messages: container, hideEmpty() {}, scrollToBottom() {} };
        const api = (
          window as unknown as {
            MemoryChatComponents: {
              renderA2UISurface: (id: string, msgs: unknown, ctx: unknown) => void;
            };
          }
        ).MemoryChatComponents;
        const read = () => document.querySelector('.memory-a2ui-announcer');
        api.renderA2UISurface('s1', created, ctx);
        const afterCreateOnly = read() ? read()!.textContent : null;
        api.renderA2UISurface('s1', frame, ctx);
        const afterComponents = read() ? read()!.textContent : null;
        api.renderA2UISurface('s1', later, ctx);
        const node = read();
        return {
          afterCreateOnly,
          afterComponents,
          text: node ? node.textContent : null,
          role: node ? node.getAttribute('role') : null,
          live: node ? node.getAttribute('aria-live') : null,
          announcers: document.querySelectorAll('.memory-a2ui-announcer').length,
          cards: document.querySelectorAll('#chat-messages .card-body').length,
        };
      },
      {
        created: createOnly('s1'),
        frame: surfaceFrame('s1', [
          { id: 'r1', component: 'result', rows: [{ label: 'status', value: 'FRAME_ONE' }] },
        ]),
        later: surfaceFrame('s1', [
          { id: 'r1', component: 'result', rows: [{ label: 'status', value: 'FRAME_TWO' }] },
        ]),
      },
    );

    // The frame with no components announces nothing; the first real card does.
    expect(announced.afterCreateOnly).toBeNull();
    expect(announced.afterComponents).toBe('New card 1: Result card.');
    // One surface, updated in place, announced once as a polite status.
    expect(announced.cards).toBe(1);
    expect(announced.announcers).toBe(1);
    expect(announced.text).toBe('New card 1: Result card.');
    expect(announced.role).toBe('status');
    expect(announced.live).toBe('polite');
  });

  test('distinguishes two consecutive surfaces of the same kind', async ({ page }) => {
    await loadScripts(page);

    // Identical status text is not re-announced by a live region, so the second
    // card of the same kind must carry different text to be heard at all.
    const text = await page.evaluate(
      ({ first, second }) => {
        const container = document.getElementById('chat-messages') as HTMLElement;
        const ctx = { messages: container, hideEmpty() {}, scrollToBottom() {} };
        const api = (
          window as unknown as {
            MemoryChatComponents: {
              renderA2UISurface: (id: string, msgs: unknown, ctx: unknown) => void;
            };
          }
        ).MemoryChatComponents;
        api.renderA2UISurface('s1', first, ctx);
        api.renderA2UISurface('s2', second, ctx);
        const node = document.querySelector('.memory-a2ui-announcer');
        return {
          text: node ? node.textContent : null,
          cards: document.querySelectorAll('#chat-messages .card-body').length,
        };
      },
      {
        first: surfaceFrame('s1', [
          { id: 'r1', component: 'result', rows: [{ label: 'status', value: 'ONE' }] },
        ]),
        second: surfaceFrame('s2', [
          { id: 'r2', component: 'result', rows: [{ label: 'status', value: 'TWO' }] },
        ]),
      },
    );

    expect(text.cards).toBe(2);
    expect(text.text).toBe('New card 2: Result card.');
  });

  test('announces a multi-kind surface with singular copy', async ({ page }) => {
    await loadScripts(page);

    // A surface may carry several kinds in one updateComponents. The ordinal
    // counts surfaces, not cards, so the sentence stays singular — "New cards 1"
    // reads as a count and a plural noun with 1 is wrong for AT (#597).
    const text = await page.evaluate(
      (frame) => {
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
        const node = document.querySelector('.memory-a2ui-announcer');
        return {
          text: node ? node.textContent : null,
          cards: document.querySelectorAll('#chat-messages .card-body').length,
        };
      },
      surfaceFrame('s1', [
        { id: 'r1', component: 'result', rows: [{ label: 'status', value: 'OK' }] },
        { id: 's1', component: 'sources', items: [{ title: 'Doc', url: 'https://example.test' }] },
      ]),
    );

    expect(text.cards).toBe(2);
    expect(text.text).toBe('New card 1: Result card, Sources card.');
  });

  test('restarts the ordinal when the transcript is cleared', async ({ page }) => {
    await loadScripts(page);

    // The ordinal is conversation-lifetime, not page-lifetime: clearing the
    // transcript (how a new conversation starts) resets the renderer's A2UI
    // state, so the next card is "New card 1" again instead of a page-lifetime
    // ordinal such as "New card 47" (#597).
    const result = await page.evaluate(
      ({ first, second }) => {
        const container = document.getElementById('chat-messages') as HTMLElement;
        const ctx = { messages: container, hideEmpty() {}, scrollToBottom() {} };
        const api = (
          window as unknown as {
            MemoryChatComponents: {
              renderA2UISurface: (id: string, msgs: unknown, ctx: unknown) => void;
              resetA2UI: (container: HTMLElement) => void;
            };
          }
        ).MemoryChatComponents;
        api.renderA2UISurface('s1', first, ctx);
        container.innerHTML = '';
        api.resetA2UI(container);
        api.renderA2UISurface('s1', second, ctx);
        const node = document.querySelector('.memory-a2ui-announcer');
        return {
          text: node ? node.textContent : null,
          announcers: document.querySelectorAll('.memory-a2ui-announcer').length,
        };
      },
      {
        first: surfaceFrame('s1', [
          { id: 'r1', component: 'result', rows: [{ label: 'status', value: 'ONE' }] },
        ]),
        second: surfaceFrame('s1', [
          { id: 'r1', component: 'result', rows: [{ label: 'status', value: 'TWO' }] },
        ]),
      },
    );

    expect(result.announcers).toBe(1);
    expect(result.text).toBe('New card 1: Result card.');
  });

  test('does not announce cards during history replay', async ({ page }) => {
    await loadScripts(page);
    await replayItems(page, [cardTurn(1, 'FIRST_CARD')]);

    await expect(page.locator(CARD_BODIES).filter({ hasText: 'FIRST_CARD' })).toHaveCount(1);
    await expect(page.locator('.memory-a2ui-announcer')).toHaveCount(0);
  });
});
