import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Hermetic, gateway-free wiring spec for the chat session-lifecycle rail
// (#1318): the delete confirmation (shown / confirmed / dismissed), the
// include-archived filter's server-side rail refresh, and the
// archive → unarchive round-trip on a session row.
//
// No gateway, no memory API, no Zitadel, no `.env.e2e` — the shipped scripts are
// loaded verbatim into a bare page and fed a stubbed fetch that behaves like a
// tiny server: the held archive/unarchive POSTs make each optimistic frame
// observable before it settles, the include-archived query selects which
// server-rendered rail list is returned, and the DELETE resolves like the real
// `204 No Content` contract. It runs in CI under `js-dom.config.ts`
// (`task e2e:js` / `npx playwright test --config=js-dom.config.ts`).

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

// row mirrors the server-rendered `sessionRailItem` markup that matters to
// chat.js: the resume action, the (archived) marker + muting, and the per-row
// Action menu entries for archive/unarchive/delete.
function row(id: string, title: string, archived = false): string {
  const archivedAttr = archived ? ' data-archived="true"' : '';
  const marker = archived
    ? '<span class="badge badge-ghost badge-xs" data-testid="session-archived-marker">Archived</span>'
    : '';
  const lifecycleAction = archived
    ? `<button type="button" role="menuitem" data-action="unarchive-session" data-id="${id}" data-testid="session-unarchive">Unarchive</button>`
    : `<button type="button" role="menuitem" data-action="archive-session" data-id="${id}" data-testid="session-archive">Archive</button>`;
  return `<li class="${archived ? 'list-row opacity-60' : 'list-row'}" data-action="resume-session" data-id="${id}" data-agent="" data-origin="manual" data-testid="session-row"${archivedAttr}>
  <div class="list-col-grow"><div>${marker}</div><div class="font-medium text-sm">${title}</div></div>
  ${lifecycleAction}
  <button type="button" role="menuitem" data-action="delete-session" data-id="${id}" data-title="${title}" data-testid="session-delete">Delete</button>
</li>`;
}

const ACTIVE = row('A', 'Alpha') + row('B', 'Beta');
const ARCHIVED = row('A', 'Alpha', true) + row('B', 'Beta');
const ACTIVE_B_ONLY = row('B', 'Beta');

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
    <select id="chat-origin-filter" aria-label="Filter sessions by type"><option value="">All types</option><option value="manual">Manual</option><option value="scheduled">Scheduled</option></select>
    <label><input id="chat-include-archived" data-testid="chat-include-archived" type="checkbox" aria-label="Include archived" /> Include archived</label>
    <div id="chat-rail-list" data-testid="session-list">${ACTIVE}</div>
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
        <span id="chat-stop" class="hidden shrink-0"><button type="button" aria-label="Stop generating">Stop</button></span>
      </form>
    </div>
  </div>
  <dialog id="chat-delete-confirm-modal" class="modal" hx-boost="false">
    <div class="modal-box max-w-sm">
      <h3 class="text-lg font-semibold">Delete session?</h3>
      <p class="text-muted mt-1 text-sm">This permanently deletes <span id="chat-delete-confirm-name" class="font-medium text-base-content">this session</span> and its messages. It cannot be undone.</p>
      <div class="modal-action">
        <button type="button" data-action="close-delete-session">Cancel</button>
        <button type="button" id="chat-delete-confirm-go" data-testid="chat-delete-confirm-go">Delete</button>
      </div>
    </div>
    <form method="dialog" class="modal-backdrop"><button>close</button></form>
  </dialog>
</div>
</body>
</html>`;

interface Toast {
  kind: string;
  msg: string;
}

interface FetchCall {
  url: string;
  method: string;
}

interface HeldRequest {
  url: string;
  resolve: (r: Response) => void;
}

interface StubWindow {
  __fetchCalls: FetchCall[];
  __toasts: Toast[];
  __railActive: string;
  __railArchived: string;
  __held: HeldRequest[];
  fetch: (url: unknown, opts?: { method?: string }) => Promise<Response>;
}

async function bootstrap(page: Page): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));

  await page.setContent(SKELETON);

  await page.evaluate(
    ({ active, archived }) => {
      const w = window as unknown as StubWindow;
      w.__fetchCalls = [];
      w.__toasts = [];
      w.__railActive = active;
      w.__railArchived = archived;
      w.__held = [];

      (w as unknown as { MemoryApp: unknown }).MemoryApp = {
        toast: (kind: string, msg: string) => {
          w.__toasts.push({ kind, msg });
        },
      };

      w.fetch = function (url: unknown, opts?: { method?: string }) {
        const u = String(url);
        const method = (opts && opts.method) || 'GET';
        w.__fetchCalls.push({ url: u, method });

        // Archive/unarchive POSTs are held open so the optimistic frame is
        // observable before the request settles. /unarchive is matched first
        // because its path also contains the substring "/archive".
        if (u.indexOf('/api/conversations/') === 0 && u.indexOf('/unarchive') !== -1) {
          return new Promise<Response>((resolve) => w.__held.push({ url: u, resolve }));
        }
        if (u.indexOf('/api/conversations/') === 0 && u.indexOf('/archive') !== -1) {
          return new Promise<Response>((resolve) => w.__held.push({ url: u, resolve }));
        }
        if (method === 'DELETE' && u.indexOf('/api/conversations/') === 0) {
          // 204 is a null-body status: Response must be constructed with null.
          return Promise.resolve(new Response(null, { status: 204 }));
        }
        // The include-archived filter is server-side: the query string selects
        // which authoritative rail list the server renders.
        if (u.indexOf('/partial/chat-rail') === 0) {
          const include = u.indexOf('includeArchived=true') !== -1;
          return Promise.resolve(
            new Response(include ? w.__railArchived : w.__railActive, {
              status: 200,
              headers: { 'Content-Type': 'text/html' },
            }),
          );
        }
        return Promise.resolve(new Response('[]', { status: 200 }));
      };
    },
    { active: ACTIVE, archived: ARCHIVED },
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

function dialogOpen(page: Page): Promise<boolean> {
  return page.evaluate(
    () => (document.getElementById('chat-delete-confirm-modal') as HTMLDialogElement | null)?.open === true,
  );
}

function deleteName(page: Page): Promise<string> {
  return page.evaluate(
    () => document.getElementById('chat-delete-confirm-name')?.textContent ?? '',
  );
}

function rowPresent(page: Page, id: string): Promise<boolean> {
  return page.evaluate(
    (rowId) => !!document.querySelector(`li[data-action="resume-session"][data-id="${rowId}"]`),
    id,
  );
}

function rowHidden(page: Page, id: string): Promise<boolean> {
  return page.evaluate((rowId) => {
    const el = document.querySelector(`li[data-action="resume-session"][data-id="${rowId}"]`);
    return !!el && el.classList.contains('hidden');
  }, id);
}

function rowArchived(page: Page, id: string): Promise<boolean> {
  return page.evaluate((rowId) => {
    const el = document.querySelector(`li[data-action="resume-session"][data-id="${rowId}"]`);
    return !!el && el.getAttribute('data-archived') === 'true';
  }, id);
}

function markerCount(page: Page): Promise<number> {
  return page.evaluate(
    () => document.querySelectorAll('[data-testid="session-archived-marker"]').length,
  );
}

async function setRails(
  page: Page,
  rails: { active?: string; archived?: string },
): Promise<void> {
  await page.evaluate((r) => {
    const w = window as unknown as StubWindow;
    if (r.active !== undefined) w.__railActive = r.active;
    if (r.archived !== undefined) w.__railArchived = r.archived;
  }, rails);
}

function fetchCalls(page: Page): Promise<FetchCall[]> {
  return page.evaluate(() => (window as unknown as StubWindow).__fetchCalls);
}

function heldUrls(page: Page): Promise<string[]> {
  return page.evaluate(() => (window as unknown as StubWindow).__held.map((h) => h.url));
}

function lastRailUrl(page: Page): Promise<string> {
  return page.evaluate(() => {
    const calls = (window as unknown as StubWindow).__fetchCalls.filter((c) =>
      c.url.indexOf('/partial/chat-rail') === 0,
    );
    return calls.length ? calls[calls.length - 1].url : '';
  });
}

async function releaseHeld(page: Page, id: string, route: string, status: number): Promise<void> {
  await page.evaluate(
    ({ rowId, route, code }) => {
      const w = window as unknown as StubWindow;
      const needle = '/api/conversations/' + rowId + route;
      const idx = w.__held.findIndex((h) => h.url.indexOf(needle) === 0);
      if (idx === -1) throw new Error(`no held request for ${needle}`);
      const [entry] = w.__held.splice(idx, 1);
      entry.resolve(new Response('', { status: code }));
    },
    { rowId: id, route, code: status },
  );
}

function hasDeleteCall(calls: FetchCall[]): boolean {
  return calls.some((c) => c.method === 'DELETE');
}

test.describe('session lifecycle rail (#1318)', () => {
  test('delete shows the confirmation with the session title and sends no request', async ({ page }) => {
    const errors = await bootstrap(page);

    await click(page, '[data-action="delete-session"][data-id="A"]');

    await expect.poll(() => dialogOpen(page)).toBe(true);
    expect(await deleteName(page)).toBe('Alpha');
    // Opening the confirmation only stages the delete; the row is untouched and
    // no lifecycle request may have been sent yet.
    expect(await rowPresent(page, 'A')).toBe(true);
    const calls = await fetchCalls(page);
    expect(hasDeleteCall(calls), `no DELETE before confirming: ${JSON.stringify(calls)}`).toBe(false);

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('dismissing the confirmation keeps the session and sends no request', async ({ page }) => {
    const errors = await bootstrap(page);

    await click(page, '[data-action="delete-session"][data-id="A"]');
    await expect.poll(() => dialogOpen(page)).toBe(true);

    await click(page, '[data-action="close-delete-session"]');

    await expect.poll(() => dialogOpen(page)).toBe(false);
    expect(await rowPresent(page, 'A')).toBe(true);
    const calls = await fetchCalls(page);
    expect(hasDeleteCall(calls), `dismissal must not DELETE: ${JSON.stringify(calls)}`).toBe(false);

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('confirming deletes the session and reconciles the rail', async ({ page }) => {
    const errors = await bootstrap(page);

    await click(page, '[data-action="delete-session"][data-id="A"]');
    await expect.poll(() => dialogOpen(page)).toBe(true);

    // The server list the post-delete refresh will serve: A is gone.
    await setRails(page, { active: ACTIVE_B_ONLY });

    await click(page, '#chat-delete-confirm-go');

    await expect.poll(() => dialogOpen(page)).toBe(false);
    const calls = await fetchCalls(page);
    const deletes = calls.filter((c) => c.method === 'DELETE');
    expect(deletes).toEqual([{ url: '/api/conversations/A', method: 'DELETE' }]);
    await expect.poll(() => rowPresent(page, 'A')).toBe(false);
    await expect
      .poll(() =>
        page.evaluate(() =>
          (window as unknown as StubWindow).__toasts.some((t) => t.kind === 'success'),
        ),
      )
      .toBe(true);

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('the include-archived filter toggles archived rows via a server refresh', async ({ page }) => {
    const errors = await bootstrap(page);

    // Default (filter off): the archived row is not in the rail.
    expect(await markerCount(page)).toBe(0);

    await click(page, '#chat-include-archived');
    await expect
      .poll(async () => (await lastRailUrl(page)).includes('includeArchived=true'))
      .toBe(true);
    await expect.poll(() => rowPresent(page, 'A')).toBe(true);
    await expect.poll(() => rowArchived(page, 'A')).toBe(true);
    expect(await markerCount(page)).toBe(1);

    // Turning it back off drops the archived row again.
    await click(page, '#chat-include-archived');
    await expect
      .poll(async () => (await lastRailUrl(page)).includes('includeArchived=false'))
      .toBe(true);
    await expect.poll(() => markerCount(page)).toBe(0);
    expect(await rowArchived(page, 'A')).toBe(false);

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('archive then unarchive round-trips a session through the server state', async ({ page }) => {
    const errors = await bootstrap(page);

    // Archive with the include-archived filter off: the row hides optimistically
    // while the POST is still in flight.
    await click(page, '[data-action="archive-session"][data-id="A"]');
    await expect.poll(() => rowHidden(page, 'A')).toBe(true);
    await expect.poll(async () => (await heldUrls(page)).includes('/api/conversations/A/archive')).toBe(true);

    // Settle the archive against the server list that now excludes A.
    await setRails(page, { active: ACTIVE_B_ONLY, archived: ARCHIVED });
    await releaseHeld(page, 'A', '/archive', 200);
    await expect.poll(() => rowPresent(page, 'A')).toBe(false);

    // Enabling include-archived reveals A as an archived row.
    await click(page, '#chat-include-archived');
    await expect.poll(() => rowPresent(page, 'A')).toBe(true);
    await expect.poll(() => rowArchived(page, 'A')).toBe(true);
    expect(await markerCount(page)).toBe(1);

    // Unarchive clears the archived presentation optimistically...
    await click(page, '[data-action="unarchive-session"][data-id="A"]');
    await expect.poll(() => rowArchived(page, 'A')).toBe(false);
    await expect
      .poll(async () => (await heldUrls(page)).includes('/api/conversations/A/unarchive'))
      .toBe(true);

    // ...and the settled server state agrees: A is active again.
    await setRails(page, { archived: ACTIVE });
    await releaseHeld(page, 'A', '/unarchive', 200);
    await expect.poll(() => rowArchived(page, 'A')).toBe(false);
    await expect.poll(() => markerCount(page)).toBe(0);
    expect(await rowPresent(page, 'A')).toBe(true);

    const calls = await fetchCalls(page);
    expect(calls.some((c) => c.url === '/api/conversations/A/archive')).toBe(true);
    expect(calls.some((c) => c.url === '/api/conversations/A/unarchive')).toBe(true);

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });
});
