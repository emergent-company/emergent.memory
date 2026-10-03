import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Hermetic, gateway-free wiring spec for #1382: archiving a session must feel
// instant. chat.js hides the rail row the moment the user clicks Archive, and
// restores it (with an error toast) if the request fails. The server remains the
// source of truth: the refresh that follows a settled request rebuilds the rail
// from /partial/chat-rail.
//
// No gateway, no memory API, no Zitadel, no `.env.e2e` — the shipped scripts are
// loaded verbatim into a bare page and fed a stubbed fetch (the archive POST is
// held open so the optimistic frame is observable before it settles).

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

const ROWS = `
<li class="list-row" data-action="resume-session" data-id="A" data-agent="" data-origin="manual" data-testid="session-row">
  <div class="list-col-grow"><div class="font-medium text-sm">Alpha</div></div>
  <button type="button" role="menuitem" data-action="archive-session" data-id="A" data-testid="session-archive">Archive</button>
</li>
<li class="list-row" data-action="resume-session" data-id="B" data-agent="" data-origin="manual" data-testid="session-row">
  <div class="list-col-grow"><div class="font-medium text-sm">Beta</div></div>
  <button type="button" role="menuitem" data-action="archive-session" data-id="B" data-testid="session-archive">Archive</button>
</li>`;

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
    <select id="chat-agent-filter" aria-label="Filter sessions by agent"><option value="">All agents</option></select>
    <select id="chat-origin-filter" aria-label="Filter sessions by type"><option value="">All types</option></select>
    <div id="chat-rail-list" data-testid="session-list">${ROWS}</div>
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
        <span id="chat-stop" class="hidden shrink-0">
          <button type="button" aria-label="Stop generating">Stop</button>
        </span>
      </form>
    </div>
  </div>
</div>
</body>
</html>`;

interface Toast {
  kind: string;
  msg: string;
}

interface StubWindow {
  __fetchCalls: string[];
  __toasts: Toast[];
  __railHtml: string;
  __archiveReleases: Array<(r: Response) => void>;
  fetch: (url: unknown) => Promise<Response>;
}

async function bootstrap(page: Page): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));

  await page.setContent(SKELETON);

  await page.evaluate(
    ({ rows }) => {
      const w = window as unknown as StubWindow;
      w.__fetchCalls = [];
      w.__toasts = [];
      w.__railHtml = rows;
      w.__archiveReleases = [];

      // notify() routes through window.MemoryApp.toast.
      (w as unknown as { MemoryApp: unknown }).MemoryApp = {
        toast: (kind: string, msg: string) => {
          w.__toasts.push({ kind, msg });
        },
      };

      w.fetch = function (url) {
        const u = String(url);
        w.__fetchCalls.push(u);
        // Hold the archive POST open so the optimistic (pre-settle) frame is
        // observable; the test releases it explicitly.
        if (u.indexOf('/archive') !== -1) {
          return new Promise<Response>((resolve) => {
            w.__archiveReleases.push(resolve);
          });
        }
        if (u.indexOf('/partial/chat-rail') === 0) {
          return Promise.resolve(
            new Response(w.__railHtml, {
              status: 200,
              headers: { 'Content-Type': 'text/html' },
            }),
          );
        }
        return Promise.resolve(new Response('[]', { status: 200 }));
      };
    },
    { rows: ROWS },
  );

  await page.addScriptTag({ path: CHAT_TRANSPORT_JS });
  await page.addScriptTag({ path: CHAT_COMPONENTS_JS });
  await page.addScriptTag({ path: CHAT_HOST_JS });
  await page.addScriptTag({ path: CHAT_STREAM_JS });
  await page.addScriptTag({ path: CHAT_JS });

  return errors;
}

function rowHidden(page: Page, id: string): Promise<boolean> {
  return page.evaluate((rowId) => {
    const row = document.querySelector(
      `li[data-action="resume-session"][data-id="${rowId}"]`,
    );
    return !!row && row.classList.contains('hidden');
  }, id);
}

function rowPresent(page: Page, id: string): Promise<boolean> {
  return page.evaluate(
    (rowId) => !!document.querySelector(`li[data-action="resume-session"][data-id="${rowId}"]`),
    id,
  );
}

async function click(page: Page, selector: string): Promise<void> {
  await page.evaluate((sel) => {
    const el = document.querySelector(sel);
    if (!el) throw new Error(`missing element: ${sel}`);
    (el as HTMLElement).click();
  }, selector);
}

async function setRailHtml(page: Page, html: string): Promise<void> {
  await page.evaluate((h) => {
    (window as unknown as StubWindow).__railHtml = h;
  }, html);
}

async function releaseArchive(page: Page, status: number): Promise<void> {
  await page.evaluate((code) => {
    const w = window as unknown as StubWindow;
    const releases = w.__archiveReleases.splice(0);
    releases.forEach((resolve) => resolve(new Response('', { status: code })));
  }, status);
}

test.describe('optimistic session archive (#1382)', () => {
  test('hides the row instantly and restores it when the request fails', async ({ page }) => {
    const errors = await bootstrap(page);

    await click(page, '[data-action="archive-session"][data-id="A"]');
    // Optimistic frame: hidden before the archive POST settles.
    await expect.poll(() => rowHidden(page, 'A')).toBe(true);

    // Fail the request; the row must come back and the error must surface.
    await releaseArchive(page, 500);
    await expect.poll(() => rowPresent(page, 'A')).toBe(true);
    await expect.poll(() => rowHidden(page, 'A')).toBe(false);
    await expect
      .poll(() =>
        page.evaluate(() =>
          (window as unknown as StubWindow).__toasts.some((t) => t.kind === 'error'),
        ),
      )
      .toBe(true);

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('keeps the row hidden on success and reconciles with the server rail', async ({ page }) => {
    const errors = await bootstrap(page);

    await click(page, '[data-action="archive-session"][data-id="A"]');
    await expect.poll(() => rowHidden(page, 'A')).toBe(true);

    // The server now excludes the archived row; the post-settle refresh serves
    // that filtered list and the row is gone for good.
    await setRailHtml(
      page,
      `<li class="list-row" data-action="resume-session" data-id="B" data-agent="" data-origin="manual" data-testid="session-row"><div class="list-col-grow"><div class="font-medium text-sm">Beta</div></div></li>`,
    );
    await releaseArchive(page, 200);

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
});
