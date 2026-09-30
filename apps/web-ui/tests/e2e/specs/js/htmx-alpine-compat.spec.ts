import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';
import fs from 'node:fs';

// Hermetic DOM-level regression spec for the htmx v4 + Alpine "same-id
// attribute-restore" trap (issue #1267).
//
// htmx 4.0.0-beta6's `innerHTML` swap copies then restores attributes for
// same-`id` elements (stable-id CSS transitions). Across a swap, Alpine's
// MutationObserver auto-initialises the swapped subtree *during* the swap,
// sets the inline `display: none` that `x-show="open"` (open=false) produces,
// and then htmx's attribute-restore reverts the element to the fragment's
// original attributes — which carry no inline style — so the element ends
// `display: block` while Alpine's data still says hidden.
//
// The official `hx-alpine-compat` extension fixes this by deferring Alpine's
// mutation processing (`window.Alpine.deferMutations()`) for the duration of
// the swap and flushing it after (`flushAndStopDeferringMutations()`), so
// Alpine always initialises the swapped subtree against the FINAL (post-swap,
// post-restore) DOM.
//
// This spec serves the shipped `htmx.min.js`, the extension, and the exact
// Alpine 3.15.12 build the gateway serves at `/static/js/alpine.js` (copied
// from go-daisy's embedded staticfs — see fixtures/alpine.js). No gateway, no
// memory API, no Zitadel, no `.env.e2e` — runs in CI.

const HTMX_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/htmx.min.js',
);
const COMPAT_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/hx-alpine-compat.js',
);
const ALPINE_JS = path.resolve(__dirname, '../../fixtures/alpine.js');

// Initial content: Alpine has already initialised the <ul id="x"> and hidden it
// via x-show (open=false → inline display:none). This is the "previous DOM"
// whose attributes htmx will copy-then-restore during the swap.
const BASE_HTML = `<!doctype html>
<html><head><meta charset="utf-8"></head><body>
<div id="target">
  <div x-data="{ open: false }" data-frag="1"><ul id="x" x-show="open" x-cloak>X</ul></div>
</div>
<script src="/htmx.min.js"></script>
<script src="/hx-alpine-compat.js"></script>
<script src="/alpine.js" defer></script>
</body></html>`;

// The swapped-in fragment: identical markup, NO server-rendered inline style
// (this is what a partial response would carry). data-frag="2" proves the swap
// actually replaced the content.
const FRAGMENT = `<div x-data="{ open: false }" data-frag="2"><ul id="x" x-show="open" x-cloak>X</ul></div>`;

async function routePage(page: Page): Promise<void> {
  const scripts: Record<string, string> = {
    '/htmx.min.js': HTMX_JS,
    '/hx-alpine-compat.js': COMPAT_JS,
    '/alpine.js': ALPINE_JS,
  };
  await page.route('**/*', (route) => {
    const p = new URL(route.request().url()).pathname;
    if (p === '/') {
      return route.fulfill({ contentType: 'text/html', body: BASE_HTML });
    }
    if (p === '/fragment') {
      return route.fulfill({ contentType: 'text/html', body: FRAGMENT });
    }
    if (scripts[p]) {
      return route.fulfill({
        contentType: 'application/javascript',
        body: fs.readFileSync(scripts[p], 'utf8'),
      });
    }
    return route.fulfill({ status: 404, body: 'not found' });
  });
}

async function displayOf(page: Page): Promise<string> {
  return page.evaluate(() => {
    const el = document.getElementById('x');
    return el ? getComputedStyle(el).display : 'MISSING';
  });
}

async function swapFragment(page: Page): Promise<void> {
  await page.evaluate(() => {
    // Fire-and-forget: the swap completion is asserted via the data-frag marker.
    htmx.ajax('GET', '/fragment', { target: '#target', swap: 'innerHTML' });
  });
}

test.describe('htmx v4 + Alpine same-id attribute restore (hx-alpine-compat)', () => {
  test('an x-show element with an id stays hidden after an innerHTML swap', async ({ page }) => {
    const errors: string[] = [];
    page.on('pageerror', (err) => errors.push(err.message));
    await routePage(page);

    await page.goto('http://localhost/');

    // Deferred Alpine initialises after parse; wait until it hides the initial
    // <ul id="x"> so the "previous DOM" carries the inline display:none.
    await expect
      .poll(async () => displayOf(page), { timeout: 10_000 })
      .toBe('none');

    await swapFragment(page);

    // The fragment marker proves the swap actually replaced #target's content.
    await expect(page.locator('#target [x-data]')).toHaveAttribute('data-frag', '2');

    // Without the extension, htmx's attribute-restore strips the inline
    // display:none Alpine set, leaving the ul visible (display:block). With it,
    // Alpine re-applies the hidden state against the final DOM.
    await expect
      .poll(async () => displayOf(page), { timeout: 10_000 })
      .toBe('none');

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });
});
