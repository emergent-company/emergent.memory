import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Hermetic spec for the assistant drawer's LIVE thinking wiring (#2275).
//
// The history path is covered elsewhere; this drives a live `thinking` SSE
// frame through sidepanel.js's dispatcher and asserts the drawer renders one
// live badge in stream position, stops the shimmer on `done`, and clears the
// live class when the stream finishes.
const SIDEPANEL_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/sidepanel.js',
);
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

const SKELETON = `<!doctype html>
<html>
<head><meta charset="utf-8" /><style>.hidden { display: none !important; }</style></head>
<body>
<div id="sidepanel-root">
  <div id="sidepanel-backdrop" class="hidden"></div>
  <aside id="sidepanel-panel" data-agent-id="assistant" data-org-id="org-1" data-project-id="proj-1" data-user-id="user-1">
    <div id="sidepanel-resize"></div>
    <button id="sidepanel-width-toggle" aria-pressed="false"></button>
    <div id="sidepanel-session-picker"><span id="sidepanel-session-label">New session</span><ul id="sidepanel-session-menu"></ul></div>
    <div id="sidepanel-log">
      <div id="sidepanel-empty"></div>
      <div id="sidepanel-messages"></div>
    </div>
    <form id="sidepanel-form">
      <textarea id="sidepanel-input" rows="1" aria-label="Message"></textarea>
      <span id="sidepanel-stop" class="hidden"><button type="button">Stop</button></span>
      <button id="sidepanel-send" type="submit">Send</button>
    </form>
  </aside>
  <button id="sidepanel-toggle" aria-expanded="false"></button>
  <dialog id="sidepanel-modal"><div id="sidepanel-modal-box"></div></dialog>
</div>
</body>
</html>`;

interface SseControl {
  push: (frame: unknown) => void;
  close: () => void;
}

async function boot(page: Page): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));

  await page.route('http://memory.test/**', (route) =>
    route.fulfill({ contentType: 'text/html', body: SKELETON }),
  );
  await page.goto('http://memory.test/');

  await page.evaluate(() => {
    localStorage.clear();
    const w = window as unknown as {
      fetch: typeof fetch;
      __sse: SseControl | null;
    };
    w.__sse = null;
    w.fetch = function (url: RequestInfo | URL, opts?: RequestInit) {
      const u = String(url);
      if (opts && opts.method === 'POST' && u.indexOf('/api/chat') === 0) {
        const encoder = new TextEncoder();
        const body = new ReadableStream<Uint8Array>({
          start(controller) {
            w.__sse = {
              push: (frame: unknown) =>
                controller.enqueue(encoder.encode('data: ' + JSON.stringify(frame) + '\n\n')),
              close: () => {
                try {
                  controller.close();
                } catch {
                  /* already closed */
                }
              },
            };
          },
        });
        return Promise.resolve(
          new Response(body, {
            status: 200,
            headers: { 'Content-Type': 'text/event-stream' },
          }),
        );
      }
      if (u.indexOf('/api/conversations') === 0) {
        return Promise.resolve(
          new Response(JSON.stringify({ conversations: [] }), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          }),
        );
      }
      return Promise.resolve(new Response('[]', { status: 200 }));
    };
  });

  await page.addScriptTag({ path: CHAT_TRANSPORT_JS });
  await page.addScriptTag({ path: CHAT_COMPONENTS_JS });
  await page.addScriptTag({ path: CHAT_HOST_JS });
  await page.addScriptTag({ path: CHAT_STREAM_JS });
  await page.addScriptTag({ path: SIDEPANEL_JS });

  await expect(page.locator('#sidepanel-panel')).toHaveAttribute('data-ready', '1');
  return errors;
}

async function startTurn(page: Page): Promise<void> {
  await page.fill('#sidepanel-input', 'hello');
  await page.locator('#sidepanel-form').evaluate((f: HTMLFormElement) => f.requestSubmit());
  await expect
    .poll(() => page.evaluate(() => !!(window as unknown as { __sse: unknown }).__sse))
    .toBe(true);
}

async function push(page: Page, frame: unknown): Promise<void> {
  await page.evaluate((f) => (window as unknown as { __sse: SseControl }).__sse.push(f), frame);
}

test.describe('assistant drawer — live thinking wiring', () => {
  test('a thinking frame renders one live badge; done stops the shimmer', async ({ page }) => {
    const errors = await boot(page);
    await startTurn(page);

    await push(page, { type: 'meta', conversationId: 'conv-live' });
    await push(page, { type: 'thinking', id: 't1', text: 'planning', done: false });

    const thinking = page.locator('#sidepanel-messages .memory-thinking');
    await expect(thinking).toHaveCount(1);
    await expect(thinking).toHaveClass(/memory-badge-live/);
    await expect(thinking.locator('.memory-thinking-body')).toHaveText('planning');

    await push(page, { type: 'thinking', id: 't1', text: '', done: true });
    await expect(thinking).not.toHaveClass(/memory-badge-live/);
    await expect(thinking.locator('.memory-thinking-body')).toHaveText('planning');

    // Finish the stream so the drawer settles (recordHistory/persist path).
    await push(page, { type: 'done' });
    await page.evaluate(() => (window as unknown as { __sse: SseControl }).__sse.close());
    await expect(page.locator('#sidepanel-panel')).toHaveAttribute('data-ready', '1');

    expect(errors).toEqual([]);
  });

  test('a stream ending without done leaves no live badge', async ({ page }) => {
    const errors = await boot(page);
    await startTurn(page);

    await push(page, { type: 'thinking', id: 't2', text: 'still thinking', done: false });
    const thinking = page.locator('#sidepanel-messages .memory-thinking');
    await expect(thinking).toHaveClass(/memory-badge-live/);

    // Close the connection without a `done` frame.
    await page.evaluate(() => (window as unknown as { __sse: SseControl }).__sse.close());
    await expect(thinking).not.toHaveClass(/memory-badge-live/);

    expect(errors).toEqual([]);
  });
});
