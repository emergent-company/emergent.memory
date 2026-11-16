import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Hermetic DOM-level regression spec for the inbox "open" click (#1362).
//
// Clicking a "needs your input" notification must (a) navigate to the deep link
// that carries the pending question/approval and (b) mark the notification read
// on the way out. The shipped inbox.js is loaded verbatim into a bare page with
// EventSource + fetch stubbed; a synthetic click on the row body anchor is
// dispatched and the resulting mutation request is asserted.
//
// This fails against an inbox.js that does not special-case
// `[data-notification-open]` (no read request is issued) and passes once the
// click marks the item read before navigating.
//
// Since #756 the row is `components.LinkRow`: the anchor covers the leading
// tile/title/meta block and the badges + message sit in a details band outside
// it, while the row root carries `data-href` so a tap anywhere on the row
// navigates. A tap outside the anchor therefore has to mark the notification
// read too (app.js owns that navigation, inbox.js the read), so the last three
// tests drive the details band and a control on that shape of row.

const INBOX_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/inbox.js',
);

function row(unread: boolean): string {
  return `<div data-inbox-page data-scope="project" data-tab="action" data-project-id="p1">
    <div data-notification-id="n-input" data-notification-unread="${unread}">
      <a href="/chat?c=conv-1" data-notification-open data-notif-id="n-input">
        <p>Agent needs your input</p>
      </a>
    </div>
  </div>`;
}

// The row as components.LinkRow renders it (#756): the anchor covers the title
// block only, the badges and the message sit in the details band outside it, and
// the root carries data-href so a tap anywhere on the row navigates (app.js).
function linkRow(unread: boolean): string {
  return `<div data-inbox-page data-scope="project" data-tab="action" data-project-id="p1">
    <div class="transition-colors" data-href="/chat?c=conv-1" data-notification-id="n-input" data-notification-unread="${unread}">
      <div>
        <a class="flex" href="/chat?c=conv-1" data-notification-open data-notif-id="n-input">
          <p>Agent needs your input</p>
        </a>
        <div>
          <button type="button" data-notif-action="read" data-notif-id="n-input">Read</button>
        </div>
      </div>
      <div data-notification-details><p>Which fruit?</p></div>
    </div>
  </div>`;
}

interface ReadCall {
  url: string;
  method: string;
  keepalive: boolean;
}

async function boot(page: Page, body: string): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));
  // A concrete base origin so the row's relative deep link resolves to a valid
  // absolute URL (an about:blank base makes location.assign reject it). No
  // server is contacted: the fetch stub intercepts mutations and the test reads
  // its recorded calls synchronously before any navigation load.
  await page.setContent(
    `<!doctype html><html><head><base href="http://memory.local/"></head><body>${body}</body></html>`,
  );
  await page.evaluate(() => {
    const w = window as unknown as {
      __readCalls: ReadCall[];
      EventSource: unknown;
      fetch: unknown;
    };
    w.__readCalls = [];
    w.EventSource = function (this: Record<string, unknown>, url: string) {
      this.url = url;
      this.addEventListener = function () {};
      this.close = function () {};
    };
    w.fetch = function (url: string, opts?: RequestInit) {
      w.__readCalls.push({
        url: String(url),
        method: (opts && opts.method) || 'GET',
        keepalive: !!(opts && opts.keepalive),
      });
      return Promise.resolve({ ok: true, json: () => Promise.resolve({}) });
    };
  });
  await page.addScriptTag({ path: INBOX_JS });
  return errors;
}

async function clickOpen(page: Page): Promise<ReadCall[]> {
  return page.evaluate(() => {
    const el = document.querySelector('[data-notification-open]');
    if (!el) throw new Error('open anchor not found');
    el.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
    return (window as unknown as { __readCalls: ReadCall[] }).__readCalls;
  });
}

function readMutations(calls: ReadCall[]): ReadCall[] {
  return calls.filter((c) => c.url.includes('/read'));
}

test('clicking an unread openable notification marks it read', async ({ page }) => {
  const errors = await boot(page, row(true));
  const calls = readMutations(await clickOpen(page));
  expect(calls).toHaveLength(1);
  expect(calls[0].method).toBe('PATCH');
  expect(calls[0].url).toContain('/api/notifications/n-input/read');
  expect(calls[0].keepalive).toBe(true);
  expect(errors).toEqual([]);
});

test('clicking an already-read notification does not re-mark it', async ({ page }) => {
  const errors = await boot(page, row(false));
  const calls = readMutations(await clickOpen(page));
  expect(calls).toHaveLength(0);
  expect(errors).toEqual([]);
});

async function click(page: Page, selector: string): Promise<ReadCall[]> {
  return page.evaluate((sel) => {
    const el = document.querySelector(sel);
    if (!el) throw new Error(`${sel} not found`);
    el.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
    return (window as unknown as { __readCalls: ReadCall[] }).__readCalls;
  }, selector);
}

// #756: the row is components.LinkRow, so the anchor covers the title block and
// the message sits in the details band outside it. A tap there is a whole-row
// tap (the root's data-href, which app.js navigates on), so it must still mark
// the notification read — otherwise "opened" depends on which part of the row
// the user happened to hit.
test('a tap on the row outside the anchor marks it read', async ({ page }) => {
  const errors = await boot(page, linkRow(true));
  const calls = readMutations(await click(page, '[data-notification-details]'));
  expect(calls).toHaveLength(1);
  expect(calls[0].method).toBe('PATCH');
  expect(calls[0].url).toContain('/api/notifications/n-input/read');
  expect(calls[0].keepalive).toBe(true);
  expect(errors).toEqual([]);
});

test('a tap on the row outside the anchor leaves an already-read row alone', async ({ page }) => {
  const errors = await boot(page, linkRow(false));
  const calls = readMutations(await click(page, '[data-notification-details]'));
  expect(calls).toHaveLength(0);
  expect(errors).toEqual([]);
});

test('a row action keeps its own behaviour instead of the whole-row open', async ({ page }) => {
  const errors = await boot(page, linkRow(true));
  const calls = await click(page, '[data-notif-action="read"]');
  // The action mutates through its own verb; the row-level tap path must not
  // also fire a read for the same click.
  expect(readMutations(calls)).toHaveLength(1);
  expect(readMutations(calls)[0].url).toBe('/api/notifications/n-input/read');
  expect(calls.filter((c) => c.url.includes('/read'))).toHaveLength(1);
  expect(errors).toEqual([]);
});
