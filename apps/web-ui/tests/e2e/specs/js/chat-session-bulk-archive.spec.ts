import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Hermetic, gateway-free wiring spec for #1385: multi-select on the session
// rail. Entering selection mode reveals a checkbox per conversation row plus a
// footer action bar (select-all, live count, Archive); Archive applies to every
// checked row through the same per-session lifecycle route the row menu uses,
// optimistically hiding the set and exiting selection mode once it settles.
//
// No gateway, no memory API, no Zitadel, no `.env.e2e` — the shipped scripts are
// loaded verbatim into a bare page and fed a stubbed, held-open archive POST.

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

function row(id: string, title: string): string {
  return `<li class="list-row" data-action="resume-session" data-id="${id}" data-agent="" data-origin="manual" data-testid="session-row">
  <div class="flex items-center gap-1.5"><input type="checkbox" class="checkbox checkbox-sm session-select hidden" data-action="select-session" data-id="${id}" data-testid="session-select" aria-label="Select session: ${title}" /></div>
  <div class="list-col-grow"><div class="font-medium text-sm">${title}</div></div>
  <button type="button" role="menuitem" data-action="archive-session" data-id="${id}" data-testid="session-archive">Archive</button>
</li>`;
}

const ALL_ROWS = row('A', 'Alpha') + row('B', 'Beta') + row('C', 'Gamma');

const SKELETON = `<!doctype html>
<html>
<head>
<meta charset="utf-8" />
<style>.hidden { display: none !important; }</style>
</head>
<body>
<div id="chat-root" data-conv="">
  <aside id="chat-rail">
    <div id="chat-rail-resize" role="separator" aria-orientation="vertical" aria-label="Resize session list"></div>
    <div>
      <button type="button" data-action="toggle-session-select" data-testid="toggle-session-select" aria-pressed="false">Select</button>
    </div>
    <select id="chat-agent-filter" aria-label="Filter sessions by agent"><option value="">All agents</option></select>
    <select id="chat-origin-filter" aria-label="Filter sessions by type"><option value="">All types</option><option value="scheduled">Scheduled</option></select>
    <div id="chat-rail-list" data-testid="session-list">${ALL_ROWS}</div>
    <div id="chat-bulk-bar" data-testid="chat-bulk-bar" class="hidden" role="group" aria-label="Bulk session actions">
      <input id="chat-select-all" data-testid="chat-select-all" data-action="select-all-sessions" type="checkbox" aria-label="Select all sessions" />
      <span id="chat-selection-count" data-testid="chat-selection-count" aria-live="polite">0 selected</span>
      <button type="button" id="chat-bulk-archive" data-action="bulk-archive-sessions" data-testid="chat-bulk-archive" disabled>Archive</button>
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

interface Toast {
  kind: string;
  msg: string;
}

interface StubWindow {
  __fetchCalls: string[];
  __toasts: Toast[];
  __railHtml: string;
  __railStatus: number;
  __archiveReleases: Array<{ url: string; resolve: (r: Response) => void }>;
  fetch: (url: unknown) => Promise<Response>;
}

async function bootstrap(page: Page): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));

  await page.setContent(SKELETON);

  await page.evaluate(
    ({ rows }) => {
      const w = window as unknown as StubWindow;
      w.__fetchCalls = [];
      w.__toasts = [];
      w.__railHtml = rows;
      w.__railStatus = 200;
      w.__archiveReleases = [];

      (w as unknown as { MemoryApp: unknown }).MemoryApp = {
        toast: (kind: string, msg: string) => {
          w.__toasts.push({ kind, msg });
        },
      };

      w.fetch = function (url) {
        const u = String(url);
        w.__fetchCalls.push(u);
        if (u.indexOf('/archive') !== -1) {
          return new Promise<Response>((resolve) => {
            w.__archiveReleases.push({ url: u, resolve });
          });
        }
        if (u.indexOf('/partial/chat-rail') === 0) {
          return Promise.resolve(
            new Response(w.__railHtml, {
              status: w.__railStatus || 200,
              headers: { 'Content-Type': 'text/html' },
            }),
          );
        }
        return Promise.resolve(new Response('[]', { status: 200 }));
      };
    },
    { rows: ALL_ROWS },
  );

  await page.addScriptTag({ path: CHAT_TRANSPORT_JS });
  await page.addScriptTag({ path: CHAT_COMPONENTS_JS });
  await page.addScriptTag({ path: CHAT_HOST_JS });
  await page.addScriptTag({ path: CHAT_STREAM_JS });
  await page.addScriptTag({ path: CHAT_JS });

  return errors;
}

async function click(page: Page, selector: string): Promise<void> {
  await page.evaluate((sel) => {
    const el = document.querySelector(sel);
    if (!el) throw new Error(`missing element: ${sel}`);
    (el as HTMLElement).click();
  }, selector);
}

async function setFilter(page: Page, selector: string, value: string): Promise<void> {
  await page.evaluate(
    ({ sel, val }) => {
      const el = document.querySelector(sel) as HTMLSelectElement | null;
      if (!el) throw new Error(`missing element: ${sel}`);
      el.value = val;
      el.dispatchEvent(new Event('change', { bubbles: true }));
    },
    { sel: selector, val: value },
  );
}

async function setRailStatus(page: Page, status: number): Promise<void> {
  await page.evaluate((s) => {
    (window as unknown as StubWindow).__railStatus = s;
  }, status);
}

function checkboxChecked(page: Page, id: string): Promise<boolean> {
  return page.evaluate((rowId) => {
    const el = document.querySelector(`.session-select[data-id="${rowId}"]`) as HTMLInputElement | null;
    return !!el && el.checked;
  }, id);
}

function rowHidden(page: Page, id: string): Promise<boolean> {
  return page.evaluate((rowId) => {
    const el = document.querySelector(
      `li[data-action="resume-session"][data-id="${rowId}"]`,
    );
    return !!el && el.classList.contains('hidden');
  }, id);
}

function rowPresent(page: Page, id: string): Promise<boolean> {
  return page.evaluate(
    (rowId) => !!document.querySelector(`li[data-action="resume-session"][data-id="${rowId}"]`),
    id,
  );
}

function checkboxVisible(page: Page, id: string): Promise<boolean> {
  return page.evaluate((rowId) => {
    const el = document.querySelector(`.session-select[data-id="${rowId}"]`);
    return !!el && !el.classList.contains('hidden');
  }, id);
}

async function setRailHtml(page: Page, html: string): Promise<void> {
  await page.evaluate((h) => {
    (window as unknown as StubWindow).__railHtml = h;
  }, html);
}

async function releaseArchives(page: Page, status: number): Promise<void> {
  await page.evaluate((code) => {
    const w = window as unknown as StubWindow;
    const releases = w.__archiveReleases.splice(0);
    releases.forEach((entry) => entry.resolve(new Response('', { status: code })));
  }, status);
}

async function releaseArchiveFor(page: Page, id: string, status: number): Promise<void> {
  await page.evaluate(
    ({ rowId, code }) => {
      const w = window as unknown as StubWindow;
      const prefix = '/api/conversations/' + rowId + '/archive';
      const idx = w.__archiveReleases.findIndex((entry) => entry.url.indexOf(prefix) === 0);
      if (idx === -1) throw new Error(`no held archive request for ${rowId}`);
      const [entry] = w.__archiveReleases.splice(idx, 1);
      entry.resolve(new Response('', { status: code }));
    },
    { rowId: id, code: status },
  );
}

test.describe('bulk session selection and archive (#1385)', () => {
  test('selects via select-all, archives only the checked set, then exits', async ({ page }) => {
    const errors = await bootstrap(page);

    // Enter selection mode: checkboxes and the footer bar appear, aria-pressed
    // flips truthfully.
    await click(page, '[data-action="toggle-session-select"]');
    await expect.poll(() => checkboxVisible(page, 'A')).toBe(true);
    await expect
      .poll(() =>
        page.evaluate(
          () =>
            document
              .querySelector('[data-action="toggle-session-select"]')!
              .getAttribute('aria-pressed') === 'true' &&
            !document.getElementById('chat-bulk-bar')!.classList.contains('hidden'),
        ),
      )
      .toBe(true);

    // Select-all checks every selectable row.
    await click(page, '#chat-select-all');
    await expect
      .poll(() => page.evaluate(() => document.getElementById('chat-selection-count')!.textContent))
      .toBe('3 selected');
    await expect
      .poll(() => page.evaluate(() => (document.getElementById('chat-select-all') as HTMLInputElement).checked))
      .toBe(true);

    // Deselect one: count drops and select-all becomes indeterminate.
    await click(page, '.session-select[data-id="C"]');
    await expect
      .poll(() => page.evaluate(() => document.getElementById('chat-selection-count')!.textContent))
      .toBe('2 selected');
    await expect
      .poll(() =>
        page.evaluate(() => {
          const all = document.getElementById('chat-select-all') as HTMLInputElement;
          return all.indeterminate && !all.checked;
        }),
      )
      .toBe(true);
    await expect
      .poll(() => page.evaluate(() => (document.getElementById('chat-bulk-archive') as HTMLButtonElement).disabled))
      .toBe(false);

    // Archive the checked set: A + B hide immediately, C stays.
    await click(page, '[data-action="bulk-archive-sessions"]');
    await expect.poll(() => rowHidden(page, 'A')).toBe(true);
    await expect.poll(() => rowHidden(page, 'B')).toBe(true);
    expect(await rowHidden(page, 'C')).toBe(false);

    const archived = await page.evaluate(() =>
      (window as unknown as StubWindow).__fetchCalls
        .filter((u) => u.indexOf('/api/conversations/') === 0 && u.indexOf('/archive') !== -1)
        .map((u) => u.replace('/api/conversations/', '').replace('/archive', '')),
    );
    expect(new Set(archived)).toEqual(new Set(['A', 'B']));

    // Settle: the server rail now excludes A + B; selection mode exits.
    await setRailHtml(page, row('C', 'Gamma'));
    await releaseArchives(page, 200);

    await expect
      .poll(() => page.evaluate(() => document.getElementById('chat-selection-count')!.textContent))
      .toBe('0 selected');
    await expect
      .poll(() =>
        page.evaluate(
          () =>
            document
              .querySelector('[data-action="toggle-session-select"]')!
              .getAttribute('aria-pressed') === 'false' &&
            document.getElementById('chat-bulk-bar')!.classList.contains('hidden'),
        ),
      )
      .toBe(true);
    await expect
      .poll(() =>
        page.evaluate(() => (window as unknown as StubWindow).__toasts.some((t) => t.kind === 'success')),
      )
      .toBe(true);

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('a filter change never reveals a pending archive, and a failed rollback respects the filter', async ({ page }) => {
    const errors = await bootstrap(page);

    await click(page, '[data-action="toggle-session-select"]');
    await click(page, '.session-select[data-id="A"]');
    await click(page, '.session-select[data-id="B"]');

    // Filter the selected rows out, then archive them.
    await setFilter(page, '#chat-origin-filter', 'scheduled');
    await expect.poll(() => rowHidden(page, 'A')).toBe(true);
    await click(page, '[data-action="bulk-archive-sessions"]');
    await expect.poll(() => rowHidden(page, 'A')).toBe(true);

    // Back to "all": without a pending marker distinct from the filter's
    // `hidden` class the filter pass would clear the optimistic hide and expose
    // a session that is mid-archive.
    await setFilter(page, '#chat-origin-filter', '');
    await expect.poll(() => rowHidden(page, 'A')).toBe(true);
    await expect.poll(() => rowHidden(page, 'B')).toBe(true);

    // Fail the archive while the filter hides the rows again, and fail the
    // reconciling refresh too, so only the rollback's visibility logic decides.
    await setFilter(page, '#chat-origin-filter', 'scheduled');
    await setRailStatus(page, 500);
    await releaseArchives(page, 500);
    await expect
      .poll(() => page.evaluate(() => (window as unknown as StubWindow).__toasts.some((t) => t.kind === 'error')))
      .toBe(true);
    await expect.poll(() => rowHidden(page, 'A')).toBe(true);
    await expect.poll(() => rowHidden(page, 'B')).toBe(true);

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('re-applies selection state after a #chat-root shell swap', async ({ page }) => {
    const errors = await bootstrap(page);

    await click(page, '[data-action="toggle-session-select"]');
    await click(page, '.session-select[data-id="A"]');
    await expect.poll(() => checkboxChecked(page, 'A')).toBe(true);

    // Simulate an htmx shell swap: fresh server markup (checkboxes hidden,
    // footer hidden, aria-pressed=false) replacing the rail + footer, with the
    // post-swap init pass. selectionMode survives as module state.
    await page.evaluate(
      ({ rows }) => {
        const root = document.getElementById('chat-root')!;
        root.removeAttribute('data-ready');
        document.getElementById('chat-rail-list')!.innerHTML = rows;
        const bar = document.getElementById('chat-bulk-bar')!;
        bar.classList.add('hidden');
        bar.classList.remove('flex');
        document
          .querySelector('[data-action="toggle-session-select"]')!
          .setAttribute('aria-pressed', 'false');
        document.dispatchEvent(new CustomEvent('htmx:after:swap'));
      },
      { rows: ALL_ROWS },
    );

    await expect.poll(() => checkboxVisible(page, 'A')).toBe(true);
    await expect
      .poll(() =>
        page.evaluate(() => ({
          pressed: document
            .querySelector('[data-action="toggle-session-select"]')!
            .getAttribute('aria-pressed'),
          barHidden: document.getElementById('chat-bulk-bar')!.classList.contains('hidden'),
          count: document.getElementById('chat-selection-count')!.textContent,
        })),
      )
      .toEqual({ pressed: 'true', barHidden: false, count: '1 selected' });

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('a partial bulk-archive failure restores only the failed row', async ({ page }) => {
    const errors = await bootstrap(page);

    await click(page, '[data-action="toggle-session-select"]');
    await click(page, '.session-select[data-id="A"]');
    await click(page, '.session-select[data-id="B"]');
    await click(page, '[data-action="bulk-archive-sessions"]');
    await expect.poll(() => rowHidden(page, 'A')).toBe(true);
    await expect.poll(() => rowHidden(page, 'B')).toBe(true);

    // A succeeds, B fails. The reconciling refresh fails too, so only the
    // per-id rollback can reveal the failed row again.
    await setRailStatus(page, 500);
    await releaseArchiveFor(page, 'A', 200);
    await releaseArchiveFor(page, 'B', 500);

    await expect.poll(() => rowHidden(page, 'A')).toBe(true);
    await expect.poll(() => rowPresent(page, 'B')).toBe(true);
    await expect.poll(() => rowHidden(page, 'B')).toBe(false);
    await expect
      .poll(() => page.evaluate(() => (window as unknown as StubWindow).__toasts.some((t) => t.kind === 'error')))
      .toBe(true);
    await expect
      .poll(() =>
        page.evaluate(() =>
          document.querySelector('[data-action="toggle-session-select"]')!.getAttribute('aria-pressed'),
        ),
      )
      .toBe('false');

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });
});
