import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Parity gate for #1286 Step 4 — the shared timeline renderer.
//
// `MemoryChatComponents.renderTimeline(items, ctx, flags)` is the ONE
// implementation behind the /chat page's renderTimelineItems (chat.js) and the
// side panel's renderHistoryItems (sidepanel.js). This spec proves, in a real
// Chromium engine with the shipped scripts loaded verbatim:
//
//   1. the renderer exists and its `flags` actually gate behaviour (a direct
//      flag matrix — flipping a flag changes the DOM);
//   2. the chat surface renders run lifecycle markers, thinking blocks, the
//      run-scope "step N · …" meta annotations, tool chips and bubbles;
//   3. the side panel renders the SAME items WITHOUT markers/thinking/meta
//      (its documented deviations), while still rendering chips and bubbles.
//
// It is non-vacuous: if sidepanel.js is wired with chat's flags (or vice versa)
// the surface assertions below fail, and if renderTimeline ignores a flag the
// flag-matrix assertions fail.
//
// No gateway, no memory API, no auth — the shipped scripts are loaded into a
// bare page and fed a stubbed `fetch` + `EventSource`.

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
const SIDEPANEL_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/sidepanel.js',
);

const CONV_ID = 'conv-1';
const RUN_ID = 'r1';
const T0 = '2026-09-30T10:00:00.000Z';
const T1 = '2026-09-30T10:00:05.000Z';
const T2 = '2026-09-30T10:00:30.000Z';

type HistoryItem = Record<string, unknown>;

// One rich, run-scoped timeline. The two surfaces render this SAME array; the
// only difference must be the flags-driven deviations (markers/thinking/meta).
//  - run_start / run_end      → chat: typed markers; sidepanel: skipped
//  - user message             → both: a "You" bubble
//  - operator thinking        → chat: a Thinking block; sidepanel: skipped
//  - tool_call                → both: a tool chip
//  - agent message (html)     → both: an assistant bubble
const ITEMS: HistoryItem[] = [
  { kind: 'run_start', run_id: RUN_ID, run_status: 'working', run_model: 'test-model', created_at: T0 },
  { kind: 'message', role: 'user', content: { text: 'do the thing' }, step_number: 1, created_at: T0 },
  {
    kind: 'message',
    role: 'operator',
    content: { text: 'planning out loud', function_calls: [{ name: 'entity-query' }] },
    step_number: 2,
    created_at: T1,
  },
  {
    kind: 'tool_call',
    id: 'c1',
    tool_name: 'entity-query',
    tool_status: 'completed',
    tool_input: {},
    tool_output: {},
    step_number: 3,
    created_at: T1,
  },
  { kind: 'message', role: 'agent', content: { html: '<p>all done</p>' }, step_number: 4, created_at: T2 },
  { kind: 'run_end', run_id: RUN_ID, run_status: 'completed', created_at: T0, completed_at: T2 },
];

// A run whose terminal status is "failed". The start marker is a divider and
// must not carry that status — app.css turns a failed status into a
// display:block alert banner, which collapsed the start row into a
// left-aligned, unspaced line (issue #1300). The end marker owns the banner.
const FAILED_ITEMS: HistoryItem[] = [
  { kind: 'run_start', run_id: RUN_ID, run_status: 'failed', run_model: 'test-model', created_at: T0 },
  { kind: 'message', role: 'user', content: { text: 'do the thing' }, step_number: 1, created_at: T0 },
  { kind: 'run_end', run_id: RUN_ID, run_status: 'failed', error_message: 'boom', created_at: T0, completed_at: T2 },
];

const CHAT_FLAGS = { showRunMarkers: true, showThinking: true, showMeta: true, silent: false };const SIDEPANEL_FLAGS = { showRunMarkers: false, showThinking: false, showMeta: false, silent: true };

interface StubWindow {
  __eventsources: unknown[];
  __historyItems: unknown[];
  __runHistoryItems: unknown[];
  fetch: (url: unknown) => Promise<Response>;
}

// ---------------------------------------------------------------------------
// 1. Direct flag matrix — renderTimeline must gate on each flag.
// ---------------------------------------------------------------------------

const BARE_SKELETON = `<!doctype html><html><head><meta charset="utf-8" /></head><body></body></html>`;

// Loads the four dependency scripts (transport → components → host → stream)
// into whatever document is already on the page. Callers set the document
// first; this never navigates.
async function loadCoreScripts(page: Page): Promise<void> {
  await page.addScriptTag({ path: CHAT_TRANSPORT_JS });
  await page.addScriptTag({ path: CHAT_COMPONENTS_JS });
  await page.addScriptTag({ path: CHAT_HOST_JS });
  await page.addScriptTag({ path: CHAT_STREAM_JS });
}

// Renders ITEMS through the shared renderer with a minimal instrumented ctx and
// returns a serialisable DOM summary. The flag matrix then compares chat vs
// sidepanel flags on identical input.
async function renderWithFlags(
  page: Page,
  flags: Record<string, boolean>,
): Promise<{
  markers: number;
  metas: number;
  thinking: number;
  users: string[];
  assistants: string[];
  tools: string[];
}> {
  return page.evaluate(
    ({ flags, items }) => {
      const c = document.createElement('div');
      document.body.appendChild(c);
      const seen = { users: [] as string[], assistants: [] as string[], tools: [] as string[], thinking: 0 };
      const ctx = {
        container: c,
        badgeCtx: { messages: c, hideEmpty() {}, scrollToBottom() {} },
        getAgentName: () => 'Memory',
        begin() {},
        addUserMessage(text: string, _silent: boolean, meta?: string) {
          const d = document.createElement('div');
          d.className = 'u';
          d.textContent = text;
          if (meta) d.appendChild(Object.assign(document.createElement('div'), { className: 'meta', textContent: meta }));
          c.appendChild(d);
          seen.users.push(text);
        },
        addAssistantMessage(html: string, _name: string, _silent: boolean, meta?: string) {
          const d = document.createElement('div');
          d.className = 'a';
          d.innerHTML = html;
          if (meta) d.appendChild(Object.assign(document.createElement('div'), { className: 'meta', textContent: meta }));
          c.appendChild(d);
          seen.assistants.push(html);
          return d;
        },
        toolChip(tool: string, _status: string, _detail: string, payload: { meta?: string }) {
          const d = document.createElement('div');
          d.className = 'tool';
          d.setAttribute('data-tool', tool);
          if (payload && payload.meta) d.appendChild(Object.assign(document.createElement('div'), { className: 'meta', textContent: payload.meta }));
          c.appendChild(d);
          seen.tools.push(tool);
        },
        renderHistoryQuestion() {},
        renderThinkingBlock() { seen.thinking += 1; },
        isThinkingItem(item: { role?: string }, content: unknown) {
          return item.role === 'operator' && (window as any).MemoryChatStream.isThinkingMessage(content);
        },
      };
      (window as any).MemoryChatComponents.renderTimeline(items, ctx, flags);
      return {
        markers: c.querySelectorAll('.memory-run-marker').length,
        metas: c.querySelectorAll('.meta').length,
        thinking: seen.thinking,
        users: seen.users,
        assistants: seen.assistants,
        tools: seen.tools,
      };
    },
    { flags, items: ITEMS },
  );
}

test.describe('shared timeline renderer — flag matrix (chat-components.js)', () => {
  test('exposes renderTimeline and gates markers/thinking/meta on the flags', async ({ page }) => {
    const errors: string[] = [];
    page.on('pageerror', (err) => errors.push(err.message));
    await page.setContent(BARE_SKELETON);
    await loadCoreScripts(page);

    await expect
      .poll(async () => page.evaluate(() => typeof (window as any).MemoryChatComponents.renderTimeline))
      .toBe('function');

    const chat = await renderWithFlags(page, CHAT_FLAGS);
    const side = await renderWithFlags(page, SIDEPANEL_FLAGS);

    // Chat flags: both lifecycle markers, the thinking block, and the
    // "step N · …" meta annotations on the user + assistant bubbles.
    expect(chat.markers).toBe(2);
    expect(chat.thinking).toBe(1);
    expect(chat.metas).toBeGreaterThanOrEqual(2);
    expect(chat.users).toEqual(['do the thing']);
    expect(chat.assistants).toEqual(['<p>all done</p>']);
    expect(chat.tools).toEqual(['entity-query']);

    // Side panel flags: no markers, no thinking, no meta — but the same bubbles
    // and chips. If a flag were ignored/mis-wired, one of these counts would
    // match chat's instead of 0.
    expect(side.markers).toBe(0);
    expect(side.thinking).toBe(0);
    expect(side.metas).toBe(0);
    expect(side.users).toEqual(['do the thing']);
    expect(side.assistants).toEqual(['<p>all done</p>']);
    expect(side.tools).toEqual(['entity-query']);

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });
});

// ---------------------------------------------------------------------------
// 2. Chat surface integration — chat.js routes through the shared renderer.
// ---------------------------------------------------------------------------

const CHAT_SKELETON = `<!doctype html>
<html>
<head><meta charset="utf-8" /><style>.hidden { display: none !important; }</style></head>
<body>
<div id="chat-root">
  <aside id="chat-rail">
    <div id="chat-rail-resize" role="separator" aria-orientation="vertical" aria-label="Resize session list"></div>
    <select id="chat-agent-filter" aria-label="Filter sessions by agent"><option value="">All agents</option></select>
    <select id="chat-origin-filter" aria-label="Filter sessions by type"><option value="">All types</option></select>
    <div id="chat-rail-list">
      <div data-action="open-run" data-id="${RUN_ID}" data-agent="" data-origin="scheduled">Scheduled run</div>
    </div>
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
        <span id="chat-stop" class="hidden shrink-0"><button type="button" aria-label="Stop generating">Stop</button></span>
      </form>
    </div>
  </div>
</div>
</body>
</html>`;

async function installStubs(page: Page, runItems: HistoryItem[]): Promise<void> {
  await page.evaluate(
    ({ convId, runId, runItems }) => {
      const w = window as unknown as StubWindow;
      w.__eventsources = [];
      w.__historyItems = [];
      w.__runHistoryItems = runItems;
      w.fetch = function (url) {
        const u = String(url);
        if (u.indexOf('/api/runs/' + runId + '/history') !== -1) {
          return Promise.resolve(
            new Response(JSON.stringify({ conversation_id: runId, items: w.__runHistoryItems }), {
              status: 200,
              headers: { 'Content-Type': 'application/json' },
            }),
          );
        }
        if (u.indexOf('/api/conversations/' + convId + '/history') !== -1) {
          return Promise.resolve(
            new Response(JSON.stringify({ conversation_id: convId, items: w.__historyItems }), {
              status: 200,
              headers: { 'Content-Type': 'application/json' },
            }),
          );
        }
        if (u.indexOf('/partial/') === 0) return Promise.resolve(new Response('', { status: 200 }));
        return Promise.resolve(new Response('[]', { status: 200 }));
      };
      function FakeEventSource(this: { close: () => void }) {
        this.close = function () {};
        w.__eventsources.push(this);
      }
      (w as any).EventSource = FakeEventSource;
    },
    { convId: CONV_ID, runId: RUN_ID, runItems },
  );
}

async function loadChatSurface(page: Page): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));
  await loadCoreScripts(page);
  await page.addScriptTag({ path: CHAT_JS });
  return errors;
}

test.describe('shared timeline renderer — /chat surface (chat.js)', () => {
  test('renders run markers, thinking, run-scope meta, chips and bubbles from the run transcript', async ({ page }) => {
    await page.setContent(CHAT_SKELETON);
    await installStubs(page, ITEMS);
    const errors = await loadChatSurface(page);

    // Open the scheduled run → activeRunId set → run scope (currentScopeIsRun).
    await page.locator(`[data-action="open-run"][data-id="${RUN_ID}"]`).click();
    await expect(page.locator(`[data-action="open-run"][data-id="${RUN_ID}"]`)).toHaveAttribute('data-active', 'true');

    const messages = page.locator('#chat-messages');
    // Run lifecycle boundaries (chat-only deviation: showRunMarkers).
    await expect(messages.locator('.memory-run-marker[data-phase="start"]')).toHaveCount(1);
    await expect(messages.locator('.memory-run-marker[data-phase="end"]')).toHaveCount(1);
    // Thinking block (chat-only deviation: showThinking).
    await expect(messages.locator('.memory-thinking')).toHaveCount(1);
    await expect(messages.locator('.memory-thinking-body')).toContainText('planning out loud');
    // Run-scope meta annotation (chat-only deviation: showMeta).
    await expect(messages.locator('.chat-footer').first()).toContainText('step 1');
    // Shared vocabulary: tool chip + user/assistant bubbles.
    await expect(messages.locator('[data-tool="entity-query"]')).toHaveCount(1);
    await expect(messages.locator('.chat-end p')).toHaveText('do the thing');
    await expect(messages.locator('.memory-md')).toContainText('all done');

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('a failed run keeps the "Run started" marker a flex divider (no terminal status) — #1300', async ({ page }) => {
    await page.setContent(CHAT_SKELETON);
    await installStubs(page, FAILED_ITEMS);
    const errors = await loadChatSurface(page);

    await page.locator(`[data-action="open-run"][data-id="${RUN_ID}"]`).click();
    await expect(page.locator(`[data-action="open-run"][data-id="${RUN_ID}"]`)).toHaveAttribute('data-active', 'true');

    const messages = page.locator('#chat-messages');
    const start = messages.locator('.memory-run-marker[data-phase="start"]');
    await expect(start).toHaveCount(1);
    // The start marker carries no terminal status, so app.css's
    // [data-phase="end"][data-status="failed"] banner rule (display:block) can
    // never collapse its flex row into a left-aligned, unspaced line.
    await expect(start).toHaveAttribute('data-status', '');
    await expect(messages.locator('.memory-run-marker[data-phase="start"][data-status="failed"]')).toHaveCount(0);

    // The end marker is the one that owns the failure banner.
    const end = messages.locator('.memory-run-marker[data-phase="end"]');
    await expect(end).toHaveCount(1);
    await expect(end).toHaveAttribute('data-status', 'failed');
    await expect(end.locator('.memory-run-marker-failure')).toHaveCount(1);

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });
});

// ---------------------------------------------------------------------------
// 3. Side panel integration — sidepanel.js routes through the shared renderer.
// ---------------------------------------------------------------------------

const SIDEPANEL_SKELETON = `<!doctype html>
<html>
<head><meta charset="utf-8" /><style>.hidden { display: none !important; }</style></head>
<body>
<div id="sidepanel-root">
  <div id="sidepanel-backdrop" class="hidden"></div>
  <aside id="sidepanel-panel" data-agent-id="assistant" class="translate-x-full" inert aria-hidden="true">
    <div id="sidepanel-resize"></div>
    <button id="sidepanel-width-toggle" aria-pressed="false"></button>
    <div id="sidepanel-session-picker"><span id="sidepanel-session-label">New session</span><ul id="sidepanel-session-menu"></ul></div>
    <div id="sidepanel-log">
      <div id="sidepanel-empty"></div>
      <div id="sidepanel-messages"></div>
    </div>
    <form id="sidepanel-form">
      <textarea id="sidepanel-input" rows="1" aria-label="Message"></textarea>
      <span id="sidepanel-stop" class="hidden"><button type="button">Stop</button></span>
      <button id="sidepanel-send" type="submit">Send</button>
    </form>
  </aside>
  <button id="sidepanel-toggle" aria-expanded="false"></button>
  <dialog id="sidepanel-modal"><div id="sidepanel-modal-box"></div></dialog>
</div>
</body>
</html>`;

test.describe('shared timeline renderer — side panel surface (sidepanel.js)', () => {
  test('renders the same items WITHOUT markers/thinking/meta, with chips and bubbles', async ({ page }) => {
    const errors: string[] = [];
    page.on('pageerror', (err) => errors.push(err.message));

    // A real HTTP origin is required: sidepanel.js persists to localStorage,
    // which throws on about:blank's opaque origin.
    await page.route('http://memory.test/**', (route) =>
      route.fulfill({ contentType: 'text/html', body: SIDEPANEL_SKELETON }),
    );
    await page.goto('http://memory.test/');

    // Seed the persisted session so sidepanel's init() → restore() →
    // loadSession() fetches the server transcript and calls renderHistoryItems.
    await page.evaluate(
      ({ convId, items }) => {
        localStorage.setItem('memory.sidepanel.v1', JSON.stringify({ conversationId: convId, history: [] }));
        localStorage.removeItem('memory.sidepanel.width.v1');
        const w = window as unknown as StubWindow;
        w.fetch = function (url) {
          const u = String(url);
          if (u.indexOf('/api/conversations/' + convId + '/history') !== -1) {
            return Promise.resolve(
              new Response(JSON.stringify({ conversation_id: convId, items }), {
                status: 200,
                headers: { 'Content-Type': 'application/json' },
              }),
            );
          }
          if (u.indexOf('/api/conversations') === 0) {
            return Promise.resolve(
              new Response(JSON.stringify({ conversations: [] }), {
                status: 200,
                headers: { 'Content-Type': 'application/json' },
              }),
            );
          }
          return Promise.resolve(new Response('[]', { status: 200 }));
        };
      },
      { convId: CONV_ID, items: ITEMS },
    );

    await loadCoreScripts(page);
    await page.addScriptTag({ path: SIDEPANEL_JS });

    const messages = page.locator('#sidepanel-messages');
    // The shared vocabulary renders: user + assistant bubbles and the tool chip.
    await expect(messages.locator('.chat-end p')).toHaveText('do the thing');
    await expect(messages.locator('.memory-md')).toContainText('all done');
    await expect(messages.locator('[data-tool="entity-query"]')).toHaveCount(1);

    // The side-panel deviations: no run lifecycle markers, no thinking, no meta.
    await expect(messages.locator('.memory-run-marker')).toHaveCount(0);
    await expect(messages.locator('.memory-thinking')).toHaveCount(0);
    await expect(messages.locator('.chat-footer')).toHaveCount(0);

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });
});
