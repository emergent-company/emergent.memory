import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Hermetic spec for the chat composer's model/thinking controls: the model
// select is populated from /api/chat/models grouped by provider, and a stored
// selection is restored (localStorage per conversation + default slot).
const CHAT_TRANSPORT_JS = path.resolve(__dirname, '../../../../gateway/webui/static/js/chat-transport.js');
const CHAT_COMPONENTS_JS = path.resolve(__dirname, '../../../../gateway/webui/static/js/chat-components.js');
const CHAT_HOST_JS = path.resolve(__dirname, '../../../../gateway/webui/static/js/chat-host.js');
const CHAT_STREAM_JS = path.resolve(__dirname, '../../../../gateway/webui/static/js/chat-stream.js');
const CHAT_JS = path.resolve(__dirname, '../../../../gateway/webui/static/js/chat.js');

const SKELETON = `<!doctype html>
<html><head><meta charset="utf-8" /><style>.hidden { display: none !important; }</style></head>
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
      <div id="chat-empty"><select id="chat-agent" aria-label="Agent"><option value="a1">Tester</option></select></div>
      <div id="chat-messages"></div>
    </div>
    <div>
      <div id="chat-queue" class="memory-queue hidden" aria-live="polite"></div>
      <form id="chat-form">
        <select id="chat-model" aria-label="Model"><option value="">Auto (agent default)</option></select>
        <select id="chat-thinking" aria-label="Thinking">
          <option value="">Auto</option><option value="minimal">Minimal</option><option value="low">Low</option>
          <option value="medium">Medium</option><option value="high">High</option><option value="max">Max</option>
        </select>
        <textarea id="chat-input" rows="1" aria-label="Message"></textarea>
        <button id="chat-send" type="submit" aria-label="Send">Send</button>
        <span id="chat-stop" class="hidden shrink-0"><button type="button" aria-label="Stop generating">Stop</button></span>
      </form>
    </div>
  </div>
</div>
</body></html>`;

const MODELS = [
  { provider: 'openai', modelName: 'deepseek-v4-flash', displayName: 'DeepSeek V4 Flash', modelType: 'generative' },
  { provider: 'openai', modelName: 'deepseek-v4-pro', displayName: 'DeepSeek V4 Pro', modelType: 'generative' },
];

async function boot(page: Page, seed: Record<string, string>): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));
  await page.route('http://memory.test/**', (route) =>
    route.fulfill({ contentType: 'text/html', body: SKELETON }),
  );
  await page.goto('http://memory.test/');
  await page.evaluate((entries) => {
    localStorage.clear();
    Object.keys(entries).forEach((k) => localStorage.setItem(k, entries[k]));
    const w = window as unknown as { fetch: typeof fetch };
    w.fetch = function (url: RequestInfo | URL) {
      const u = String(url);
      if (u.indexOf('/api/chat/models') === 0) {
        return Promise.resolve(new Response(JSON.stringify(MODELS), { status: 200, headers: { 'Content-Type': 'application/json' } }));
      }
      if (u.indexOf('/partial/') === 0) return Promise.resolve(new Response('', { status: 200 }));
      return Promise.resolve(new Response('[]', { status: 200 }));
    };
  }, seed);
  await page.addScriptTag({ path: CHAT_TRANSPORT_JS });
  await page.addScriptTag({ path: CHAT_COMPONENTS_JS });
  await page.addScriptTag({ path: CHAT_HOST_JS });
  await page.addScriptTag({ path: CHAT_STREAM_JS });
  await page.addScriptTag({ path: CHAT_JS });
  return errors;
}

test('populates the model select grouped by provider', async ({ page }) => {
  const errors = await boot(page, {});
  const model = page.locator('#chat-model');
  await expect(model.locator('option[value="openai/deepseek-v4-flash"]')).toHaveCount(1);
  await expect(model.locator('option[value="openai/deepseek-v4-pro"]')).toHaveCount(1);
  await expect(model.locator('optgroup[label="openai"]')).toHaveCount(1);
  expect(errors).toEqual([]);
});

test('restores the stored selection', async ({ page }) => {
  const errors = await boot(page, {
    'memory.chat.model.default': 'openai/deepseek-v4-pro',
    'memory.chat.thinking.default': 'high',
  });
  await expect(page.locator('#chat-model')).toHaveValue('openai/deepseek-v4-pro');
  await expect(page.locator('#chat-thinking')).toHaveValue('high');
  expect(errors).toEqual([]);
});

test('persists changes to the current conversation and default slots', async ({ page }) => {
  const errors = await boot(page, {});
  await page.locator('#chat-model').selectOption('openai/deepseek-v4-pro');
  await page.locator('#chat-thinking').selectOption('medium');
  const stored = await page.evaluate(() => ({
    model: localStorage.getItem('memory.chat.model.new'),
    thinking: localStorage.getItem('memory.chat.thinking.new'),
    modelDefault: localStorage.getItem('memory.chat.model.default'),
    thinkingDefault: localStorage.getItem('memory.chat.thinking.default'),
  }));
  expect(stored.model).toBe('openai/deepseek-v4-pro');
  expect(stored.thinking).toBe('medium');
  expect(stored.modelDefault).toBe('openai/deepseek-v4-pro');
  expect(stored.thinkingDefault).toBe('medium');
  expect(errors).toEqual([]);
});
