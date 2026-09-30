import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Hermetic, gateway-free wiring spec for chat.js's client state machine on a
// mid-run page open.
//
// A /chat?c=<id> page opened while an agent run is already in flight must show
// an agent "working" bubble (data-memory-working + .memory-typing), a working
// header indicator (#chat-run-status), and a busy composer (stop shown, send
// hidden) — and it must release/unfreeze the composer when the run completes.
// There is no other JS test for chat.js's resume/client state machine (the
// a2ui spec only drives chat-components.js's renderer in isolation), so this is
// the regression guard for that behavior.
//
// No gateway, no memory API, no Zitadel, no `.env.e2e` — runs in CI. The four
// shipped scripts are loaded verbatim (dependencies first) into a bare page and
// fed a stubbed fetch + EventSource.

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

const CONV_ID = 'conv-1';
const T0 = '2026-09-30T10:00:00.000Z';
const T1 = '2026-09-30T10:00:05.000Z';
const T2 = '2026-09-30T10:00:30.000Z';

// Timeline items are loosely-shaped server records; only the fields chat.js
// reads matter for the stub.
type HistoryItem = Record<string, unknown>;

// A mid-run open: run_start (working), one completed tool step, and a run_end
// that still reports "working" with no completed_at.
const WORKING_ITEMS: HistoryItem[] = [
  { kind: 'run_start', run_id: 'r1', run_status: 'working', run_model: 'm', created_at: T0 },
  {
    kind: 'tool_call',
    id: 'c1',
    tool_name: 'entity-query',
    tool_status: 'completed',
    tool_input: {},
    tool_output: {},
    step_number: 1,
    created_at: T1,
  },
  { kind: 'run_end', run_id: 'r1', run_status: 'working', created_at: T0, completed_at: null },
];

// The same run, now stopped: run_end flips to completed with a completed_at.
const COMPLETED_ITEMS: HistoryItem[] = [
  { kind: 'run_start', run_id: 'r1', run_status: 'working', run_model: 'm', created_at: T0 },
  {
    kind: 'tool_call',
    id: 'c1',
    tool_name: 'entity-query',
    tool_status: 'completed',
    tool_input: {},
    tool_output: {},
    step_number: 1,
    created_at: T1,
  },
  { kind: 'run_end', run_id: 'r1', run_status: 'completed', created_at: T0, completed_at: T2 },
];

// The stub globals chat.js interacts with on this surface.
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

// The bare DOM skeleton mirrors chat.templ's ids that chat.js init()/resume
// path depends on. The `.hidden { display: none !important; }` rule stands in
// for Tailwind's utility (absent in a bare page) so visibility assertions on
// the composer/status actually reflect the `hidden` class toggles.
const SKELETON = `<!doctype html>
<html>
<head>
<meta charset="utf-8" />
<style>.hidden { display: none !important; }</style>
</head>
<body>
<div id="chat-root" data-conv="${CONV_ID}">
  <aside id="chat-rail">
    <div id="chat-rail-resize" role="separator" aria-orientation="vertical" aria-label="Resize session list"></div>
    <select id="chat-agent-filter" aria-label="Filter sessions by agent"><option value="">All agents</option></select>
    <select id="chat-origin-filter" aria-label="Filter sessions by type"><option value="">All types</option></select>
    <div id="chat-rail-list">
      <div data-action="resume-session" data-id="conv-2" data-agent="" data-origin="manual">Idle session</div>
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
        <span id="chat-stop" class="hidden shrink-0">
          <button type="button" aria-label="Stop generating">Stop</button>
        </span>
      </form>
    </div>
  </div>
</div>
</body>
</html>`;

// Install the stubs and load the four shipped scripts. Returns the live
// page-error array so the test can assert it stayed empty (a ReferenceError in
// the resume/render path lands here, not in a console warning).
async function bootstrap(page: Page, items: HistoryItem[] = WORKING_ITEMS): Promise<string[]> {
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
              JSON.stringify({
                conversation_id: convId,
                session_id: 's',
                items: w.__historyItems,
              }),
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
        // /partial/chat-dock, /partial/chat-todos, /partial/chat-rail
        if (u.indexOf('/partial/') === 0) {
          return Promise.resolve(new Response('', { status: 200 }));
        }
        return Promise.resolve(new Response('[]', { status: 200 }));
      };

      // Fake EventSource: records every instance; chat.js assigns onmessage and
      // calls close(). The test drives frames via the newest instance.
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

  await page.addScriptTag({ path: CHAT_COMPONENTS_JS });
  await page.addScriptTag({ path: CHAT_HOST_JS });
  await page.addScriptTag({ path: CHAT_STREAM_JS });
  await page.addScriptTag({ path: CHAT_JS });

  return errors;
}

// Replace the mutable history the stub serves on the next history fetch.
async function setHistory(page: Page, items: HistoryItem[]): Promise<void> {
  await page.evaluate((next) => {
    (window as unknown as StubWindow).__historyItems = next;
  }, items);
}

// Emit one SSE frame on the newest captured EventSource, exactly as the gateway
// would (chat.js's onmessage handler JSON.parses ev.data).
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

test.describe('chat.js mid-run open (chat-components/chat-host/chat-stream/chat)', () => {
  test('shows a working bubble + indicator mid-run, releases the composer when the run completes', async ({ page }) => {
    const errors = await bootstrap(page);

    // Phase 1 — mid-run open state.
    const working = page.locator('#chat-messages [data-memory-working]');
    await expect(working).toHaveCount(1);
    await expect(working).toBeVisible();
    await expect(working.locator('.memory-typing')).toHaveCount(1);

    const runStatus = page.locator('#chat-run-status');
    await expect(runStatus).toBeVisible();
    await expect(runStatus).toContainText('Working');

    // Composer busy: stop shown, send hidden + disabled.
    await expect(page.locator('#chat-stop')).toBeVisible();
    await expect(page.locator('#chat-send')).toBeHidden();
    await expect(page.locator('#chat-send')).toBeDisabled();

    // Phase 2 — the run stops; the refresh re-renders and releases the composer.
    await setHistory(page, COMPLETED_ITEMS);
    await emitSSE(page, {
      type: 'refresh',
      bucket: 'done',
      runId: 'r1',
      runStatus: 'completed',
      pendingApprovals: 0,
      pendingQuestions: 0,
    });

    await expect(page.locator('#chat-messages [data-memory-working]')).toHaveCount(0);
    await expect(page.locator('#chat-run-status')).toBeHidden();
    await expect(page.locator('#chat-stop')).toBeHidden();
    await expect(page.locator('#chat-send')).toBeVisible();
    await expect(page.locator('#chat-send')).toBeEnabled();
    // The persisted transcript is still rendered after the re-render.
    await expect(page.locator('#chat-messages .chat')).not.toHaveCount(0);

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  // DEFECT 1 regression: a stale "working" state must not survive once the run
  // is no longer active. The bare connect/poll refresh carries no runStatus, so
  // a page that re-renders an empty (or run-less) history must release the
  // header indicator, the placeholder bubble, and the busy composer — otherwise
  // an idle conversation is stuck "Working…" with a phantom bubble and a stop
  // button.
  test('releases a stale working state when the run is absent (empty history + bare refresh)', async ({ page }) => {
    const errors = await bootstrap(page, WORKING_ITEMS);
    // Sanity: the mid-run open shows the working state first.
    await expect(page.locator('#chat-messages [data-memory-working]')).toHaveCount(1);

    // The run is gone from history; a bare refresh carries no run info.
    await setHistory(page, []);
    await emitSSE(page, { type: 'refresh' });

    await expect(page.locator('#chat-messages [data-memory-working]')).toHaveCount(0);
    await expect(page.locator('#chat-run-status')).toBeHidden();
    await expect(page.locator('#chat-stop')).toBeHidden();
    await expect(page.locator('#chat-send')).toBeVisible();
    await expect(page.locator('#chat-send')).toBeEnabled();

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  // DEFECT 1 regression: opening a different, idle conversation must not carry
  // the previous scope's working state across — resumeConversation must reset
  // it before the fresh (empty) history renders.
  test('resuming an idle conversation drops the previous scope working state', async ({ page }) => {
    const errors = await bootstrap(page, WORKING_ITEMS);
    await expect(page.locator('#chat-messages [data-memory-working]')).toHaveCount(1);

    // Switch to conv-2, whose history is empty, via the session rail.
    await setHistory(page, []);
    await page.locator('[data-action="resume-session"][data-id="conv-2"]').click();

    await expect(page.locator('#chat-messages [data-memory-working]')).toHaveCount(0);
    await expect(page.locator('#chat-run-status')).toBeHidden();
    await expect(page.locator('#chat-stop')).toBeHidden();
    await expect(page.locator('#chat-send')).toBeVisible();
    await expect(page.locator('#chat-send')).toBeEnabled();

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  // DEFECT 2 regression: a run parked on a decision reports bucket
  // "needs_input" with runStatus still "working". The header must reflect the
  // bucket ("Waiting on you") and the subsequent history re-render must not
  // clobber it with an empty-bucket "Working…".
  test('a run parked on a decision shows "Waiting on you", not "Working…"', async ({ page }) => {
    const errors = await bootstrap(page, WORKING_ITEMS);
    await expect(page.locator('#chat-run-status')).toContainText('Working');

    await emitSSE(page, {
      type: 'refresh',
      bucket: 'needs_input',
      runId: 'r1',
      runStatus: 'working',
      pendingApprovals: 1,
      pendingQuestions: 0,
    });

    const runStatus = page.locator('#chat-run-status');
    await expect(runStatus).toBeVisible();
    await expect(runStatus).toContainText('Waiting on you');
    await expect(runStatus).not.toContainText('Working');

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });
});
