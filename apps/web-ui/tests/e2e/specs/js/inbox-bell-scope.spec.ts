import { test, expect, type Page } from '@playwright/test';
import fs from 'node:fs';
import path from 'node:path';

// Hermetic DOM-level spec for the inbox bell count and its live-update path.
//
// Two regressions live here:
//
//  1. #1342 — the server-rendered bell is re-painted client-side by inbox.js.
//     The pre-fix client reconciled the bell as `account unread + active-project
//     unread` while the inbox only ever listed one scope, so a user with 9
//     project unread and 2 account unread saw a bell of 11 over an account inbox
//     of 2.
//  2. document-lifecycle — the default scope was account, so project activity (a
//     teammate uploading a document) never ticked the bell off a project page;
//     and the client listened for an SSE event named `notification` while the
//     server emits `entity.created` with the entity in the payload, so the inbox
//     never updated live at all.
//
// The spec loads the shipped inbox.js verbatim into a bare page, stubs
// EventSource + fetch + htmx, and asserts: the bell equals the count of the scope
// on screen (Everything by default), a real `entity.created` notification frame
// refreshes the bell and the list, and a frame for another entity does not.
// No gateway, no memory API, no auth — runs in the js-dom gate.

const INBOX_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/inbox.js',
);

const BELL = `<div class="contents" data-notifications-bell data-testid="notifications-bell" data-unread="11">
    <div class="indicator">
      <a href="/inbox">bell</a>
      <span data-notifications-badge>9+</span>
    </div>
  </div>`;

const ALL_PAGE = `<div data-inbox-page data-scope="all" data-tab="all" data-project-id=""></div>`;
const ACCOUNT_PAGE = `<div data-inbox-page data-scope="account" data-tab="all" data-project-id=""></div>`;
const PROJECT_PAGE = `<div data-inbox-page data-scope="project" data-tab="all" data-project-id="p1"></div>`;

const UNREAD = { all: 6, account: 2, project: 5 };

type Harness = {
  countCalls: string[];
  refreshCalls: string[];
  streamUrls: string[];
  errors: string[];
};

// boot serves the page body, installs the EventSource/fetch/htmx stubs, then
// loads inbox.js so its init() runs against the stubs. Returns the recorded
// calls so the test can assert which scopes were counted and whether the list
// was refreshed.
async function boot(
  page: Page,
  body: string,
  opts: { rejectRefresh?: boolean } = {},
): Promise<Harness> {
  const harness: Harness = { countCalls: [], refreshCalls: [], streamUrls: [], errors: [] };
  page.on('pageerror', (err) => harness.errors.push(err.message));
  await page.setContent(`<!doctype html><html><body data-active-project-id="p1">${body}</body></html>`);
  harness.countCalls = await page.evaluate(async ({ unread, rejectRefresh }) => {
    const w = window as unknown as Record<string, unknown> & {
      __countCalls: string[];
      __refreshCalls: string[];
      __rejections: string[];
      __rejectRefresh: boolean;
      __streams: Array<{ url: string; listeners: Record<string, Array<(ev: unknown) => void>> }>;
      __unread: Record<string, number>;
    };
    w.__countCalls = [];
    w.__refreshCalls = [];
    w.__rejections = [];
    w.__rejectRefresh = rejectRefresh;
    w.__streams = [];
    w.__unread = unread as Record<string, number>;

    // Record unhandled rejections alongside pageerror: a swallowed failure must
    // leave neither. Installed before inbox.js so init() runs under the trap.
    window.addEventListener('unhandledrejection', (ev) => {
      w.__rejections.push(String((ev as PromiseRejectionEvent).reason));
    });

    // EventSource stand-in that records listeners so the test can deliver a real
    // frame through the same code path the browser would.
    w.EventSource = function (this: Record<string, unknown>, url: string) {
      const entry = { url: String(url), listeners: {} as Record<string, Array<(ev: unknown) => void>> };
      w.__streams.push(entry);
      this.url = String(url);
      this.addEventListener = function (name: string, fn: (ev: unknown) => void) {
        entry.listeners[name] = entry.listeners[name] || [];
        entry.listeners[name].push(fn);
      };
      this.close = function () {};
    };

    w.fetch = function (url: string) {
      w.__countCalls.push(String(url));
      let scope = 'all';
      try {
        const u = new URL(String(url), 'http://memory.local');
        scope = u.searchParams.get('scope') || 'all';
      } catch {
        // keep the Everything default
      }
      const unread = w.__unread[scope] ?? 0;
      return Promise.resolve({
        ok: true,
        json: function () {
          return Promise.resolve({ all: unread, unread: unread });
        },
      });
    };

    // htmx stand-in: refreshList must swap the list fragment, never reload the
    // page and never target the page shell. The swap mode is recorded because
    // innerHTML would nest a second #inbox-list inside the target.
    w.htmx = {
      ajax: function (
        method: string,
        url: string,
        opts?: { target?: string; swap?: string },
      ) {
        w.__refreshCalls.push(
          method +
            ' ' +
            url +
            ' -> ' +
            ((opts && opts.target) || '') +
            ' ' +
            ((opts && opts.swap) || ''),
        );
        // htmx.ajax returns a promise; the failure case must be handled by
        // refreshList instead of surfacing as an unhandled rejection.
        return w.__rejectRefresh
          ? Promise.reject(new Error('list refresh failed'))
          : Promise.resolve();
      },
    };

    // memory's scope helper, absent on the bare page.
    w.MemoryScope = { path: function (p: string) { return p; } };
    return w.__countCalls;
  }, { unread: UNREAD, rejectRefresh: !!opts.rejectRefresh });
  await page.addScriptTag({ path: INBOX_JS });
  return harness;
}

async function calls(page: Page): Promise<{ count: string[]; refresh: string[]; streams: string[] }> {
  return page.evaluate(() => {
    const w = window as unknown as {
      __countCalls: string[];
      __refreshCalls: string[];
      __streams: Array<{ url: string }>;
    };
    return {
      count: w.__countCalls.slice(),
      refresh: w.__refreshCalls.slice(),
      streams: w.__streams.map((s) => s.url),
    };
  });
}

async function unhandledRejections(page: Page): Promise<string[]> {
  return page.evaluate(() => {
    const w = window as unknown as { __rejections: string[] };
    return w.__rejections.slice();
  });
}

// emit delivers a frame to every open stream, exactly as memory's SSE does:
// the event name carries the mutation, the payload carries the entity.
async function emit(page: Page, eventName: string, payload: unknown): Promise<void> {
  await page.evaluate(
    ({ eventName, payload }) => {
      const w = window as unknown as {
        __streams: Array<{ listeners: Record<string, Array<(ev: unknown) => void>> }>;
      };
      w.__streams.forEach((stream) => {
        (stream.listeners[eventName] || []).forEach((fn) =>
          fn({ data: JSON.stringify(payload) }),
        );
      });
    },
    { eventName, payload },
  );
}

test('off the inbox page the bell counts the Everything scope', async ({ page }) => {
  const h = await boot(page, BELL);
  await expect(page.locator('[data-notifications-badge]')).toHaveText('6');
  const { count } = await calls(page);
  expect(count.some((u) => u.includes('scope=all'))).toBe(true);
  expect(count.some((u) => u.includes('scope=account'))).toBe(false);
  expect(count.some((u) => u.includes('scope=project'))).toBe(false);
  expect(h.errors).toEqual([]);
});

test('the Everything inbox lists and counts the same scope', async ({ page }) => {
  const h = await boot(page, `${BELL}${ALL_PAGE}`);
  await expect(page.locator('[data-notifications-badge]')).toHaveText('6');
  const { count } = await calls(page);
  expect(count.every((u) => u.includes('scope=all'))).toBe(true);
  expect(h.errors).toEqual([]);
});

test('the Account inbox still counts only account rows', async ({ page }) => {
  const h = await boot(page, `${BELL}${ACCOUNT_PAGE}`);
  await expect(page.locator('[data-notifications-badge]')).toHaveText('2');
  const { count } = await calls(page);
  expect(count.some((u) => u.includes('scope=account'))).toBe(true);
  expect(count.some((u) => u.includes('scope=all'))).toBe(false);
  expect(h.errors).toEqual([]);
});

test('the Project inbox counts only that project', async ({ page }) => {
  const h = await boot(page, `${BELL}${PROJECT_PAGE}`);
  await expect(page.locator('[data-notifications-badge]')).toHaveText('5');
  const { count } = await calls(page);
  expect(count.some((u) => u.includes('scope=project') && u.includes('project_id=p1'))).toBe(true);
  expect(h.errors).toEqual([]);
});

test('the Everything scope subscribes to the account and active-project streams', async ({ page }) => {
  const h = await boot(page, `${BELL}${ALL_PAGE}`);
  await expect(page.locator('[data-notifications-badge]')).toHaveText('6');
  const { streams } = await calls(page);
  expect(streams.some((u) => u.includes('scope=account'))).toBe(true);
  expect(streams.some((u) => u.includes('scope=project') && u.includes('project_id=p1'))).toBe(true);
  expect(h.errors).toEqual([]);
});

test('a notification entity frame refreshes the bell and the list', async ({ page }) => {
  const h = await boot(page, `${BELL}${ALL_PAGE}`);
  await expect(page.locator('[data-notifications-badge]')).toHaveText('6');
  const before = await calls(page);

  await page.evaluate(() => {
    const w = window as unknown as { __unread: Record<string, number> };
    w.__unread.all = 7;
  });
  await emit(page, 'entity.created', {
    entity: 'notification',
    id: 'n1',
    data: { scope: 'project', eventKey: 'document.uploaded' },
  });

  await expect(page.locator('[data-notifications-badge]')).toHaveText('7');
  const after = await calls(page);
  expect(after.count.length).toBeGreaterThan(before.count.length);
  expect(
    after.refresh.some(
      (r) =>
        r.startsWith('GET /partial/inbox-list') &&
        r.includes('-> #inbox-list outerHTML'),
    ),
  ).toBe(true);
  expect(after.refresh.some((r) => r.includes('#main-content'))).toBe(false);
  expect(h.errors).toEqual([]);
});

test('a failed list refresh is swallowed, not an unhandled rejection', async ({ page }) => {
  const h = await boot(page, `${BELL}${ALL_PAGE}`, { rejectRefresh: true });
  await expect(page.locator('[data-notifications-badge]')).toHaveText('6');

  await emit(page, 'entity.created', { entity: 'notification', id: 'n1' });

  // The refresh must have been attempted (so this cannot pass vacuously)…
  await expect
    .poll(async () => (await calls(page)).refresh.length)
    .toBeGreaterThan(0);
  // …and its rejected promise must have been handled, with no page error.
  await page.waitForTimeout(50);
  expect(h.errors).toEqual([]);
  expect(await unhandledRejections(page)).toEqual([]);
});

test('frames for other entities do not touch the inbox', async ({ page }) => {
  const h = await boot(page, `${BELL}${ALL_PAGE}`);
  await expect(page.locator('[data-notifications-badge]')).toHaveText('6');
  const before = await calls(page);

  await emit(page, 'entity.created', { entity: 'document', id: 'd1' });
  await emit(page, 'entity.updated', { entity: 'graph_object', id: 'o1' });
  await page.waitForTimeout(100);

  const after = await calls(page);
  expect(after.count.length).toBe(before.count.length);
  expect(after.refresh.length).toBe(0);
  expect(h.errors).toEqual([]);
});

// Static guards: the summing reconcile stays gone, and the client listens for
// the event name memory actually emits (a `notification` listener is inert).
test('inbox.js reconciles a single scope-filtered count and real event names', () => {
  const src = fs.readFileSync(INBOX_JS, 'utf8');
  expect(src).not.toContain('reduce(function (a, b)');
  expect(src).toContain('entity.created');
  expect(src).not.toContain('addEventListener("notification"');
});
