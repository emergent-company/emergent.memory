import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Hermetic spec for the /chat composer queue's tenancy scope (#2223).
//
// The unsaved-chat queue used to live under the browser-global
// `memory.chat.queue.v1.__new__` key, so a draft typed in project A appeared in
// project B. The key is now scoped to the body's org+project, and the retired
// global key is dropped rather than adopted.
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

const SKELETON = `<!doctype html>
<html>
<head><meta charset="utf-8" /><style>.hidden { display: none !important; }</style></head>
<body data-org-id="org-1" data-project-id="proj-A">
<div id="chat-root">
  <aside id="chat-rail">
    <div id="chat-rail-resize" role="separator" aria-orientation="vertical" aria-label="Resize session list"></div>
    <select id="chat-agent-filter" aria-label="Filter sessions by agent"><option value="">All agents</option></select>
    <select id="chat-origin-filter" aria-label="Filter sessions by type"><option value="">All types</option></select>
    <div id="chat-rail-list"></div>
  </aside>
  <div>
    <div><div id="chat-run-status" class="memory-run-status hidden" role="status" aria-live="polite"></div></div>
    <div id="chat-log">
      <div id="chat-empty">
        <select id="chat-agent" aria-label="Agent"><option value="a1">Tester</option></select>
      </div>
      <div id="chat-messages"></div>
    </div>
    <div>
      <div id="chat-queue" class="memory-queue hidden" aria-live="polite"></div>
      <form id="chat-form">
        <textarea id="chat-input" rows="1" aria-label="Message"></textarea>
        <button id="chat-send" type="submit" aria-label="Send">Send</button>
        <span id="chat-stop" class="hidden shrink-0"><button type="button" aria-label="Stop generating">Stop</button></span>
      </form>
    </div>
  </div>
</div>
</body>
</html>`;

async function boot(
  page: Page,
  projectId: string,
  seed: { key: string; value: unknown },
): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));

  await page.route('http://memory.test/**', (route) =>
    route.fulfill({ contentType: 'text/html', body: SKELETON }),
  );
  await page.goto('http://memory.test/');

  await page.evaluate(
    ({ projectId, seed }) => {
      document.body.setAttribute('data-project-id', projectId);
      localStorage.clear();
      localStorage.setItem(seed.key, JSON.stringify(seed.value));
      const w = window as unknown as { fetch: typeof fetch };
      w.fetch = function (url: RequestInfo | URL) {
        const u = String(url);
        if (u.indexOf('/partial/') === 0) {
          return Promise.resolve(new Response('', { status: 200 }));
        }
        return Promise.resolve(new Response('[]', { status: 200 }));
      };
    },
    { projectId, seed },
  );

  await page.addScriptTag({ path: CHAT_TRANSPORT_JS });
  await page.addScriptTag({ path: CHAT_COMPONENTS_JS });
  await page.addScriptTag({ path: CHAT_HOST_JS });
  await page.addScriptTag({ path: CHAT_STREAM_JS });
  await page.addScriptTag({ path: CHAT_JS });

  await page.evaluate(() =>
    (window as unknown as { MemoryChat: { renderQueue: () => void } }).MemoryChat.renderQueue(),
  );
  return errors;
}

const KEY = (project: string) => `memory.chat.queue.v1.new.org-1.${project}`;
const QUEUE = { items: [{ id: 'q1', text: 'draft from A' }], armed: false };

test('renders the queue stored for this project', async ({ page }) => {
  const errors = await boot(page, 'proj-A', { key: KEY('proj-A'), value: QUEUE });
  await expect(page.locator('#chat-queue textarea')).toHaveValue('draft from A');
  expect(errors).toEqual([]);
});

test("never renders another project's staged queue", async ({ page }) => {
  const errors = await boot(page, 'proj-B', { key: KEY('proj-A'), value: QUEUE });
  await expect(page.locator('#chat-queue')).toBeHidden();
  // The other project's payload is not ours to delete.
  expect(await page.evaluate((k) => localStorage.getItem(k), KEY('proj-A'))).not.toBeNull();
  expect(errors).toEqual([]);
});

test('drops the retired unscoped __new__ key instead of adopting it', async ({ page }) => {
  const legacy = 'memory.chat.queue.v1.__new__';
  const errors = await boot(page, 'proj-A', { key: legacy, value: QUEUE });
  await expect(page.locator('#chat-queue')).toBeHidden();
  expect(await page.evaluate((k) => localStorage.getItem(k), legacy)).toBeNull();
  expect(errors).toEqual([]);
});
