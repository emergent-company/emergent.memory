import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';
import fs from 'node:fs';

// Hermetic browser-level guard for the work board's live refresh: the shipped
// htmx 4.0.1 bundle plus the vendored hx-sse extension, the exact SSE frame
// shape `workBoardStream` writes, and the real `<hx-partial hx-target hx-swap>`
// directives — driven in a Chromium engine with no gateway and no memory API.
//
// This is the contract the Go side cannot cover on its own: `work_stream.go`
// can prove it *writes* frames, but only a browser can prove htmx *applies*
// them. A frame shape the extension rejects (wrong tag name, a swap style htmx
// does not implement, a multi-line payload split incorrectly) fails here.
//
// Run: npx playwright test --config=js-dom.config.ts board-live

const HTMX_JS = path.resolve(__dirname, '../../../../gateway/webui/static/js/htmx.min.js');
const SSE_JS = path.resolve(__dirname, '../../../../gateway/webui/static/js/hx-sse.js');

// Markup mirroring work.templ / work_board_live.templ: the three stable swap
// regions (#work-board, #work-board-count, #work-board-health), the connection
// anchor, and the type filter outside the swapped region (a morph must not close
// an open dropdown).
const BASE_HTML = `<!doctype html>
<html><head><meta charset="utf-8"></head><body>
  <div id="work-board-count" data-testid="work-board-count">2 of 3 types</div>
  <div id="work-board-health" data-testid="work-board-health"></div>
  <div id="work-board-live" hx-sse:connect="/work/board/stream" aria-hidden="true"></div>
  <div id="work-board" data-testid="work-board" tabindex="-1" data-work-board data-board-status-map='{"ready":"ready"}'>
    <section class="memory-board__lane" data-board-column="ready"><div class="lane-body">Stale card</div></section>
  </div>
  <script src="/htmx.min.js"></script>
  <script src="/hx-sse.js"></script>
</body></html>`;

// One SSE frame set exactly as writeSSEPartial emits it: the <hx-partial>
// directive and its HTML, every line of the pair prefixed with `data: `,
// blank-line terminated. The matrices frame carries no out-of-band directive, so
// a background refresh can only replace the region it names.
const FRAMES =
  'data: <hx-partial hx-target="#work-board" hx-swap="outerMorph"><div id="work-board" data-testid="work-board" tabindex="-1" data-work-board data-board-status-map=\'{"ready":"ready"}\'><section class="memory-board__lane" data-board-column="ready"><div class="lane-body">Live card</div></section></div></hx-partial>\n' +
  '\n' +
  'data: <hx-partial hx-target="#work-board-count" hx-swap="outerMorph"><div id="work-board-count" data-testid="work-board-count">3 of 3 types</div></hx-partial>\n' +
  '\n' +
  'data: <hx-partial hx-target="#work-board-health" hx-swap="outerMorph"><div id="work-board-health" data-testid="work-board-health"><p id="health">degraded</p></div></hx-partial>\n' +
  '\n';

async function routePage(page: Page): Promise<string[]> {
  const streamRequests: string[] = [];
  await page.route('**/*', (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === '/') {
      return route.fulfill({ contentType: 'text/html', body: BASE_HTML });
    }
    if (url.pathname === '/work/board/stream') {
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

test.describe('work board live refresh (htmx hx-sse + <hx-partial> frames)', () => {
  test('the stream morphs #work-board, #work-board-count and #work-board-health in place', async ({ page }) => {
    const errors: string[] = [];
    page.on('pageerror', (err) => errors.push(err.message));
    const streamRequests = await routePage(page);

    await page.goto('http://localhost/');

    // The anchor connects once, at page load.
    await expect.poll(() => streamRequests.length, { timeout: 10_000 }).toBeGreaterThan(0);

    // #work-board is replaced by the frame's fragment (outerMorph on the same id).
    await expect(page.locator('#work-board .lane-body')).toHaveText('Live card');

    // The count and health readouts are outside #work-board; they update only if
    // their own partials are applied, which is what makes them live.
    await expect(page.locator('#health')).toHaveText('degraded');
    await expect(page.locator('#work-board-count')).toHaveText('3 of 3 types');

    // Morphing in place keeps the focusable board node, so keyboard users do not
    // lose their place on every update.
    await expect(page.locator('#work-board')).toHaveAttribute('tabindex', '-1');

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
      if (url.pathname === '/work/board/stream') {
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
    await expect(page.locator('#work-board .lane-body')).toHaveText('Stale card');
    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });
});
