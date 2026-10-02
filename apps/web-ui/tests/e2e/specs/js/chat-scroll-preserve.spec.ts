import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Hermetic, gateway-free wiring spec for #1369: a refresh-driven transcript
// rebuild must preserve the viewer's scroll position.
//
// A /chat?c=<id> page opens scrolled to the latest message. A moment later the
// live push channel delivers a `refresh` frame, chat.js re-renders the
// transcript by emptying and rebuilding #chat-messages. Emptying the scroll
// container clamps its scrollTop to 0, and the per-bubble auto-scroll stops
// once the rebuilt content is taller than the viewport — so the user is left at
// the very top of a long transcript. This spec reproduces that exact sequence
// in a real Chromium engine and asserts the viewport is restored: pinned to the
// bottom when the user was at the bottom, and held at the previous offset when
// the user had scrolled up to read.
//
// No gateway, no memory API, no Zitadel, no `.env.e2e` — runs in CI alongside
// the other specs under specs/js (see js-dom.config.ts).

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
const CHAT_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/chat.js',
);

const CONV_ID = 'conv-scroll';

type HistoryItem = Record<string, unknown>;

// A long transcript: enough rows to overflow the 600px scroll container several
// times over. Every row is forced to a tall min-height by the injected CSS so a
// rebuild's per-bubble auto-scroll (which only fires within 90px of the bottom)
// cannot accidentally keep the view pinned.
function longHistory(marker = ''): HistoryItem[] {
  const items: HistoryItem[] = [
    { kind: 'run_start', run_id: 'r1', run_status: 'completed', run_model: 'm', created_at: '2026-09-30T10:00:00.000Z' },
  ];
  for (let i = 0; i < 24; i++) {
    const mins = String(i).padStart(2, '0');
    items.push({
      kind: 'message',
      role: 'user',
      content: { text: `Question ${i}${i === 0 ? ' ' + marker : ''}` },
      step_number: i * 2 + 1,
      created_at: `2026-09-30T10:${mins}:00.000Z`,
    });
    items.push({
      kind: 'message',
      role: 'assistant',
      content: { text: `Answer ${i}` },
      step_number: i * 2 + 2,
      created_at: `2026-09-30T10:${mins}:30.000Z`,
    });
  }
  items.push({ kind: 'run_end', run_id: 'r1', run_status: 'completed', created_at: '2026-09-30T10:00:00.000Z', completed_at: '2026-09-30T11:00:00.000Z' });
  return items;
}

interface FakeEventSource {
  url: string;
  onmessage: ((ev: { data: string }) => void) | null;
  onerror: ((ev: unknown) => void) | null;
  closed: boolean;
  close: () => void;
}

interface StubWindow {
  __eventsources: FakeEventSource[];
  __historyItems: unknown[];
  fetch: (url: unknown) => Promise<Response>;
}

// The bare DOM skeleton mirrors chat.templ's ids that chat.js's init()/resume
// path depends on. The `.hidden` rule stands in for Tailwind's utility; the
// #chat-log rule comes from the real page's layout (a bounded, scrollable
// transcript column) and is what makes the scroll clamp observable.
const SKELETON = `<!doctype html>
<html>
<head>
<meta charset="utf-8" />
<style>
  .hidden { display: none !important; }
  #chat-log { height: 600px; overflow-y: auto; }
  #chat-messages .chat { min-height: 120px; }
</style>
</head>
<body>
<div id="chat-root" data-conv="${CONV_ID}">
  <aside id="chat-rail">
    <div id="chat-rail-resize" role="separator" aria-orientation="vertical" aria-label="Resize session list"></div>
    <select id="chat-agent-filter" aria-label="Filter sessions by agent"><option value="">All agents</option></select>
    <select id="chat-origin-filter" aria-label="Filter sessions by type"><option value="">All types</option></select>
    <div id="chat-rail-list"></div>
  </aside>
  <div>
    <div><div id="chat-run-status" class="memory-run-status hidden" role="status" aria-live="polite"></div></div>
    <div id="chat-log">
      <div id="chat-empty">
        <select id="chat-agent" aria-label="Agent">
          <option value="a1" data-description="" data-icon="" data-color="" data-warn="">Tester</option>
        </select>
      </div>
      <div id="chat-todos" class="hidden"></div>
      <div id="chat-messages"></div>
    </div>
    <div>
      <div id="chat-dock" aria-live="polite" class="hidden"></div>
      <div id="chat-queue" class="memory-queue hidden" aria-live="polite"></div>
      <form id="chat-form">
        <textarea id="chat-input" rows="1" aria-label="Message"></textarea>
        <button id="chat-send" type="submit" aria-label="Send">Send</button>
        <span id="chat-stop" class="hidden shrink-0">
          <button type="button" aria-label="Stop generating">Stop</button>
        </span>
      </form>
    </div>
  </div>
</div>
</body>
</html>`;

async function bootstrap(page: Page, items: HistoryItem[]): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));

  await page.setContent(SKELETON);

  await page.evaluate(
    ({ convId, items }) => {
      const w = window as unknown as StubWindow;
      w.__eventsources = [];
      w.__historyItems = items;

      w.fetch = function (url) {
        const u = String(url);
        if (u.indexOf('/api/conversations/' + convId + '/history') !== -1) {
          return Promise.resolve(
            new Response(
              JSON.stringify({ conversation_id: convId, session_id: 's', items: w.__historyItems }),
              { status: 200, headers: { 'Content-Type': 'application/json' } },
            ),
          );
        }
        if (u.indexOf('/api/conversations/' + convId) !== -1) {
          return Promise.resolve(
            new Response(
              JSON.stringify({
                id: convId,
                title: 't',
                agentDefinitionId: 'a1',
                projectId: 'p',
                sessionId: 's',
                createdAt: '2026-09-30T10:00:00.000Z',
                updatedAt: '2026-09-30T10:00:00.000Z',
                messages: [],
              }),
              { status: 200, headers: { 'Content-Type': 'application/json' } },
            ),
          );
        }
        if (u.indexOf('/partial/') === 0) {
          return Promise.resolve(new Response('', { status: 200 }));
        }
        return Promise.resolve(new Response('[]', { status: 200 }));
      };

      function FakeEventSource(this: FakeEventSource, url: string) {
        this.url = String(url);
        this.onmessage = null;
        this.onerror = null;
        this.closed = false;
        w.__eventsources.push(this);
      }
      FakeEventSource.prototype.close = function (this: FakeEventSource) {
        this.closed = true;
      };
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      (w as any).EventSource = FakeEventSource;
    },
    { convId: CONV_ID, items },
  );

  await page.addScriptTag({ path: CHAT_TRANSPORT_JS });
  await page.addScriptTag({ path: CHAT_COMPONENTS_JS });
  await page.addScriptTag({ path: CHAT_HOST_JS });
  await page.addScriptTag({ path: CHAT_STREAM_JS });
  await page.addScriptTag({ path: CHAT_JS });

  return errors;
}

// Replace the history the stub serves on the next fetch, with distinct text so
// the test can prove the refresh actually re-rendered.
async function setHistory(page: Page, items: HistoryItem[]): Promise<void> {
  await page.evaluate((next) => {
    (window as unknown as StubWindow).__historyItems = next;
  }, items);
}

async function emitSSE(page: Page, frame: unknown): Promise<void> {
  await page.evaluate((f) => {
    const sources = (window as unknown as StubWindow).__eventsources;
    const src = sources[sources.length - 1];
    if (!src || typeof src.onmessage !== 'function') {
      throw new Error('no captured EventSource to emit on');
    }
    (src.onmessage as (ev: { data: string }) => void)({ data: JSON.stringify(f) });
  }, frame);
}

async function scrollState(page: Page) {
  return page.evaluate(() => {
    const log = document.getElementById('chat-log')!;
    return {
      top: log.scrollTop,
      height: log.scrollHeight,
      client: log.clientHeight,
      atBottom: log.scrollHeight - log.scrollTop - log.clientHeight < 90,
    };
  });
}

// Wait until the rendered transcript contains a row for the given marker text.
async function waitForText(page: Page, text: string): Promise<void> {
  await expect
    .poll(() =>
      page.evaluate((t) => {
        const m = document.getElementById('chat-messages');
        return !!m && (m.textContent || '').indexOf(t) !== -1;
      }, text),
    )
    .toBe(true);
}

test.describe('chat.js scroll preservation across refresh re-renders (#1369)', () => {
  test('keeps the view pinned to the bottom when a refresh rebuilds the transcript', async ({ page }) => {
    const errors = await bootstrap(page, longHistory());

    // Opening the conversation lands the viewport at the latest message.
    await waitForText(page, 'Answer 23');
    await expect.poll(async () => (await scrollState(page)).atBottom).toBe(true);

    const before = await scrollState(page);
    // Sanity: the transcript is genuinely overflowing and scrollable.
    expect(before.height).toBeGreaterThan(before.client + 1000);
    expect(before.top).toBeGreaterThan(0);

    // The live channel delivers a refresh; chat.js re-fetches and rebuilds the
    // transcript. The rebuild must not reset the viewport to the top. The
    // marker proves the new history actually rendered before we assert.
    await setHistory(page, longHistory('REBUILT-A'));
    await emitSSE(page, { type: 'refresh', bucket: 'done' });
    await waitForText(page, 'REBUILT-A');

    await expect.poll(async () => (await scrollState(page)).atBottom).toBe(true);
    const after = await scrollState(page);
    expect(after.top).toBeGreaterThan(0);

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('preserves the reading offset when a refresh rebuilds while scrolled up', async ({ page }) => {
    const errors = await bootstrap(page, longHistory());
    await waitForText(page, 'Answer 23');

    // The user scrolls up to read an earlier part of the transcript.
    const target = 800;
    await page.evaluate((top) => {
      document.getElementById('chat-log')!.scrollTop = top;
    }, target);
    const before = await scrollState(page);
    expect(before.atBottom).toBe(false);
    expect(before.top).toBe(target);

    // A refresh rebuilds the transcript (marker proves the new render landed).
    await setHistory(page, longHistory('REBUILT-B'));
    await emitSSE(page, { type: 'refresh', bucket: 'done' });
    await waitForText(page, 'REBUILT-B');

    const after = await scrollState(page);
    // Neither yanked to the top nor to the bottom: the offset survives.
    expect(after.atBottom).toBe(false);
    expect(after.top).toBe(target);

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });
});
