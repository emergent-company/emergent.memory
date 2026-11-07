import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';
import fs from 'node:fs';

// Hermetic browser-level guard for the board's live refresh: the shipped
// htmx 4.0.1 bundle plus the vendored hx-sse extension, the exact SSE frame
// shape `boardStream` writes, and the real `<hx-partial hx-target hx-swap>`
// directives — driven in a Chromium engine with no gateway and no memory API.
//
// This is the contract the Go side cannot cover on its own: `board_stream.go`
// can prove it *writes* frames, but only a browser can prove htmx *applies*
// them. A frame shape the extension rejects (wrong tag name, a swap style htmx
// does not implement, a multi-line payload split incorrectly) fails here.
//
// Run: npx playwright test --config=js-dom.config.ts board-live

const HTMX_JS = path.resolve(__dirname, '../../../../gateway/webui/static/js/htmx.min.js');
const SSE_JS = path.resolve(__dirname, '../../../../gateway/webui/static/js/hx-sse.js');

// Markup mirroring board.templ: the stable swap regions, the connection anchor,
// and the #board region the stream replaces. The header node is part of the
// initial render only — the stream never targets it.
const BASE_HTML = `<!doctype html>
<html><head><meta charset="utf-8"></head><body>
  <div id="board-header" data-testid="board-header"><h1>Board</h1></div>
  <div id="board-health" data-testid="board-health"></div>
  <div id="board-live" hx-sse:connect="/board/stream" aria-hidden="true"></div>
  <div id="board" data-testid="board" tabindex="-1" data-board-status-map='{"ready":"ready"}'>
    <section class="memory-board__lane" data-board-column="ready"><div class="lane-body">Stale card</div></section>
  </div>
  <div id="board-drawer" data-testid="board-drawer"></div>
  <script src="/htmx.min.js"></script>
  <script src="/hx-sse.js"></script>
</body></html>`;

// One SSE frame exactly as writeSSEPartial emits it: the <hx-partial> directive
// and its HTML, every line of the pair prefixed with `data: `, blank-line
// terminated. The board fragment must NOT carry the drawer, so a background
// refresh cannot close it.
const FRAMES =
  'data: <hx-partial hx-target="#board" hx-swap="outerMorph"><div id="board" data-testid="board" tabindex="-1" data-board-status-map=\'{"ready":"ready"}\'><section class="memory-board__lane" data-board-column="ready"><div class="lane-body">Live card</div></section></div></hx-partial>\n' +
  '\n' +
  'data: <hx-partial hx-target="#board-health" hx-swap="outerMorph"><div id="board-health" data-testid="board-health"><p id="health">degraded</p></div></hx-partial>\n' +
  '\n';

async function routePage(page: Page): Promise<string[]> {
  const streamRequests: string[] = [];
  await page.route('**/*', (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === '/') {
      return route.fulfill({ contentType: 'text/html', body: BASE_HTML });
    }
    if (url.pathname === '/board/stream') {
      streamRequests.push(url.pathname + url.search);
      return route.fulfill({ contentType: 'text/event-stream', body: FRAMES });
    }
    if (url.pathname === '/htmx.min.js') {
      return route.fulfill({
        contentType: 'application/javascript',
        body: fs.readFileSync(HTMX_JS, 'utf8'),
      });
    }
    if (url.pathname === '/hx-sse.js') {
      return route.fulfill({
        contentType: 'application/javascript',
        body: fs.readFileSync(SSE_JS, 'utf8'),
      });
    }
    return route.fulfill({ status: 404, body: 'not found' });
  });
  return streamRequests;
}

test.describe('board live refresh (htmx hx-sse + <hx-partial> frames)', () => {
  test('the stream morphs #board and #board-health in place', async ({ page }) => {
    const errors: string[] = [];
    page.on('pageerror', (err) => errors.push(err.message));
    const streamRequests = await routePage(page);

    await page.goto('http://localhost/');

    // The anchor connects once, at page load.
    await expect.poll(() => streamRequests.length, { timeout: 10_000 }).toBeGreaterThan(0);

    // #board is replaced by the frame's fragment (outerMorph on the same id).
    await expect(page.locator('#board .lane-body')).toHaveText('Live card');

    // The health readout is outside #board; it updates only if the extra partial
    // is applied, which is what makes it live. The page header is not a frame
    // target at all: it carries no count or state, so nothing about it changes.
    await expect(page.locator('#health')).toHaveText('degraded');

    // A background frame must never clear the drawer: that OOB clear belongs to
    // the action response (BoardRefresh), not the live stream.
    await expect(page.locator('#board-drawer')).toBeAttached();

    // Morphing in place keeps the focusable board node, so keyboard users do not
    // lose their place on every update.
    await expect(page.locator('#board')).toHaveAttribute('tabindex', '-1');

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  // When the upstream event stream is down the gateway still answers as an
  // event stream (a comment frame, then a normal end), so htmx's hx-sse backs
  // off and reconnects instead of the connection dying for the life of the tab.
  // The board on screen must survive: an empty frame set must not blank it.
  test('an unavailable upstream keeps the connection retrying and leaves the board intact', async ({ page }) => {
    const errors: string[] = [];
    page.on('pageerror', (err) => errors.push(err.message));
    let attempts = 0;
    await page.route('**/*', (route) => {
      const url = new URL(route.request().url());
      if (url.pathname === '/') {
        return route.fulfill({ contentType: 'text/html', body: BASE_HTML });
      }
      if (url.pathname === '/board/stream') {
        attempts += 1;
        return route.fulfill({
          contentType: 'text/event-stream',
          body: 'retry: 50\n: upstream event stream unavailable\n\n',
        });
      }
      if (url.pathname === '/htmx.min.js' || url.pathname === '/hx-sse.js') {
        const file = url.pathname === '/htmx.min.js' ? HTMX_JS : SSE_JS;
        return route.fulfill({ contentType: 'application/javascript', body: fs.readFileSync(file, 'utf8') });
      }
      return route.fulfill({ status: 404, body: 'not found' });
    });

    await page.goto('http://localhost/');

    // hx-sse ignores the `retry:` hint (it only yields frames with
    // data/id/event) and reconnects on its own backoff — the point is that it
    // does come back rather than leaving the tab without a live board.
    await expect.poll(() => attempts, { timeout: 10_000 }).toBeGreaterThan(1);

    // No frames were swapped, so the last good board is still what the user sees.
    await expect(page.locator('#board .lane-body')).toHaveText('Stale card');
    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });
});
