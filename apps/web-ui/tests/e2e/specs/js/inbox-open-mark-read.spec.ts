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
