import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Hermetic DOM-level regression spec for the inbox action buttons (#599).
//
// Accepting a project invitation from the inbox read as "nothing happened":
// the click resolved the item, but the Action required tab filters on
// `requires_action` alone (not `action_status`), so the row stayed put, unread
// and still actionable. Accepting must record the outcome AND finish the item —
// mark it read and archive it — so it leaves that tab.
//
// The payload assertion is on the browser→gateway contract: `action`. The
// gateway's resolve route binds that field and forwards it to memory as
// `status`; apps/web-ui/gateway/notifications_handlers_test.go pins the
// translation (`{"action":"accept"}` → `resolve:n1:accept`).
//
// The shipped inbox.js is loaded verbatim into a bare page with EventSource,
// htmx and fetch stubbed; a synthetic click on the row's Accept button is
// dispatched and the resulting mutation requests are asserted.

const INBOX_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/inbox.js',
);

function row(): string {
  return `<div data-inbox-page data-scope="account" data-tab="action">
    <div id="notification-n-invite" data-notification-id="n-invite" data-notification-unread="true">
      <a href="/invites/accept?token=tok" data-notification-open data-notif-id="n-invite">
        <p>You've been invited to ACME</p>
      </a>
      <button type="button" data-notif-action="resolve" data-notif-id="n-invite" data-notif-verb="accept">Accept</button>
    </div>
  </div>`;
}

interface Call {
  url: string;
  method: string;
  body: string | null;
}

async function boot(page: Page): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));
  await page.setContent(
    `<!doctype html><html><head><base href="http://memory.local/"></head><body>${row()}</body></html>`,
  );
  await page.evaluate(() => {
    const w = window as unknown as {
      __calls: Call[];
      EventSource: unknown;
      fetch: unknown;
      htmx: unknown;
    };
    w.__calls = [];
    w.EventSource = function (this: Record<string, unknown>, url: string) {
      this.url = url;
      this.addEventListener = function () {};
      this.close = function () {};
    };
    // The list refresh goes through htmx; stub it so the spec observes only the
    // mutations under test instead of a navigation.
    w.htmx = { ajax: function () { return Promise.resolve(); } };
    w.fetch = function (url: string, opts?: RequestInit) {
      w.__calls.push({
        url: String(url),
        method: (opts && opts.method) || 'GET',
        body: (opts && typeof opts.body === 'string' && opts.body) || null,
      });
      return Promise.resolve({ ok: true, json: () => Promise.resolve({ unread: 0 }) });
    };
  });
  await page.addScriptTag({ path: INBOX_JS });
  return errors;
}

async function clickAccept(page: Page): Promise<Call[]> {
  return page.evaluate(async () => {
    const el = document.querySelector('[data-notif-action="resolve"]');
    if (!el) throw new Error('resolve button not found');
    el.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
    // The resolve -> read -> clear chain settles over microtasks.
    await new Promise((resolve) => setTimeout(resolve, 50));
    return (window as unknown as { __calls: Call[] }).__calls;
  });
}

test('accepting an invitation records the outcome and finishes the item', async ({ page }) => {
  const errors = await boot(page);
  const calls = await clickAccept(page);

  const resolve = calls.filter((c) => c.url.endsWith('/api/notifications/n-invite/resolve'));
  expect(resolve).toHaveLength(1);
  expect(resolve[0].method).toBe('POST');
  // The gateway's resolve route binds `action` (and forwards it to memory as
  // `status`). Sending `status` here binds nothing and resolves verb-less.
  expect(JSON.parse(resolve[0].body || '{}')).toEqual({ action: 'accept' });

  const read = calls.filter((c) => c.url.endsWith('/api/notifications/n-invite/read'));
  expect(read).toHaveLength(1);
  expect(read[0].method).toBe('PATCH');

  const clear = calls.filter((c) => c.url.endsWith('/api/notifications/n-invite/clear'));
  expect(clear).toHaveLength(1);
  expect(clear[0].method).toBe('POST');

  // Resolve first, then finish: the row must not be hidden before the outcome
  // is recorded.
  const order = calls.map((c) => c.url.split('/').pop());
  expect(order.indexOf('resolve')).toBeLessThan(order.indexOf('read'));
  expect(order.indexOf('read')).toBeLessThan(order.indexOf('clear'));
  expect(errors).toEqual([]);
});
