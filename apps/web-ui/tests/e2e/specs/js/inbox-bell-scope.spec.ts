import { test, expect, type Page } from '@playwright/test';
import fs from 'node:fs';
import path from 'node:path';

// Hermetic DOM-level regression spec for the inbox bell count (#1342).
//
// The server-rendered bell is re-painted client-side by inbox.js on load and on
// every notification event. The pre-fix client reconciled the bell as
// `account unread + active-project unread` while the inbox only ever listed one
// scope, so a user with 9 project unread and 2 account unread saw a bell of 11
// over an account inbox of 2 (and every client reconcile re-applied the 11).
//
// This spec loads the shipped inbox.js verbatim into a bare page, stubs
// EventSource + fetch (the counts endpoint returns 2 for account, 9 for project
// p1), and asserts the bell equals the count of the scope on screen. It fails
// against a summing reconcile (badge 11) and passes against the scope-filtered
// one. No gateway, no memory API, no auth — runs in the js-dom gate.

const INBOX_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/inbox.js',
);

const BELL = `<!doctype html><html><body data-active-project-id="p1">
  <div class="contents" data-notifications-bell data-testid="notifications-bell" data-unread="11">
    <div class="indicator">
      <a href="/inbox">bell</a>
      <span data-notifications-badge>9+</span>
    </div>
  </div>`;

const ACCOUNT_PAGE = `<div data-inbox-page data-scope="account" data-tab="all" data-project-id=""></div>`;
const PROJECT_PAGE = `<div data-inbox-page data-scope="project" data-tab="all" data-project-id="p1"></div>`;

// boot serves the page body, installs the EventSource/fetch stubs, then loads
// inbox.js so its init() runs against the stubs. Returns the recorded page
// errors (asserted empty).
async function boot(page: Page, body: string): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));
  await page.setContent(`<!doctype html><html><body data-active-project-id="p1">${body}</body></html>`);
  await page.evaluate(() => {
    const w = window as unknown as {
      __countCalls: string[];
      EventSource: unknown;
      fetch: unknown;
    };
    w.__countCalls = [];
    // Minimal EventSource stand-in: the bell reconcile must not depend on the
    // stream, and the real one would try to open a connection.
    w.EventSource = function (this: Record<string, unknown>, url: string) {
      this.url = url;
      this.addEventListener = function () {};
      this.close = function () {};
    };
    w.fetch = function (url: string) {
      w.__countCalls.push(String(url));
      let scope = 'account';
      let pid = '';
      try {
        const u = new URL(String(url), 'http://memory.local');
        scope = u.searchParams.get('scope') || 'account';
        pid = u.searchParams.get('project_id') || '';
      } catch {
        // keep the account default
      }
      const unread = scope === 'project' ? (pid === 'p1' ? 9 : 0) : 2;
      return Promise.resolve({
        ok: true,
        json: function () {
          return Promise.resolve({ all: unread, unread: unread });
        },
      });
    };
  });
  await page.addScriptTag({ path: INBOX_JS });
  return errors;
}

test('bell counts the account inbox, never the account+project sum', async ({ page }) => {
  const errors = await boot(page, `${BELL}${ACCOUNT_PAGE}`);
  await expect(page.locator('[data-notifications-badge]')).toHaveText('2');
  const calls = await page.evaluate(() => (window as unknown as { __countCalls: string[] }).__countCalls);
  expect(calls.some((u) => u.includes('scope=account'))).toBe(true);
  expect(calls.some((u) => u.includes('scope=project'))).toBe(false);
  expect(errors).toEqual([]);
});

test('bell counts the project inbox scope', async ({ page }) => {
  const errors = await boot(page, `${BELL}${PROJECT_PAGE}`);
  await expect(page.locator('[data-notifications-badge]')).toHaveText('9');
  const calls = await page.evaluate(() => (window as unknown as { __countCalls: string[] }).__countCalls);
  expect(calls.some((u) => u.includes('scope=project') && u.includes('project_id=p1'))).toBe(true);
  expect(errors).toEqual([]);
});

test('bell defaults to the account inbox off the inbox page', async ({ page }) => {
  const errors = await boot(page, BELL);
  await expect(page.locator('[data-notifications-badge]')).toHaveText('2');
  const calls = await page.evaluate(() => (window as unknown as { __countCalls: string[] }).__countCalls);
  expect(calls.every((u) => u.includes('scope=account'))).toBe(true);
  expect(errors).toEqual([]);
});

// Static guard: the summing reconcile is gone for good — a future edit must go
// through the single scope-filtered count.
test('inbox.js reconciles a single scope-filtered count', () => {
  const src = fs.readFileSync(INBOX_JS, 'utf8');
  expect(src).not.toContain('reduce(function (a, b)');
});
