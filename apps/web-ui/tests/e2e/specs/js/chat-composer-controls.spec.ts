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
        <textarea id="chat-input" data-prompt-bar-input rows="1" aria-label="Message"></textarea>
        <div data-chat-picker="model">
          <div role="button" tabindex="0" aria-label="Select model (Auto)"><span data-chat-picker-label>Auto</span></div>
          <ul data-chat-picker-menu></ul>
          <input type="hidden" name="model" value="" />
        </div>
        <div data-chat-picker="thinking">
          <div role="button" tabindex="0" aria-label="Select thinking option (Auto)"><span data-chat-picker-label>Auto</span></div>
          <ul data-chat-picker-menu>
            <li><a data-value=""><span class="text-sm">Auto</span></a></li>
            <li><a data-value="minimal"><span class="text-sm">Minimal</span></a></li>
            <li><a data-value="low"><span class="text-sm">Low</span></a></li>
            <li><a data-value="medium"><span class="text-sm">Medium</span></a></li>
            <li><a data-value="high"><span class="text-sm">High</span></a></li>
            <li><a data-value="max"><span class="text-sm">Max</span></a></li>
          </ul>
          <input type="hidden" name="thinking" value="" />
        </div>
        <button id="chat-send" type="submit" aria-label="Send">Send</button>
        <span id="chat-stop" class="hidden"><button type="button" aria-label="Stop generating">Stop</button></span>
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
  await page.evaluate(
    ({ entries, models }) => {
      localStorage.clear();
      Object.keys(entries).forEach((k) => localStorage.setItem(k, entries[k]));
      const w = window as unknown as { fetch: typeof fetch };
      w.fetch = function (url: RequestInfo | URL) {
        const u = String(url);
        if (u.indexOf('/api/chat/models') === 0) {
          return Promise.resolve(new Response(JSON.stringify(models), { status: 200, headers: { 'Content-Type': 'application/json' } }));
        }
        if (u.indexOf('/partial/') === 0) return Promise.resolve(new Response('', { status: 200 }));
        return Promise.resolve(new Response('[]', { status: 200 }));
      };
    },
    { entries: seed, models: MODELS },
  );
  await page.addScriptTag({ path: CHAT_TRANSPORT_JS });
  await page.addScriptTag({ path: CHAT_COMPONENTS_JS });
  await page.addScriptTag({ path: CHAT_HOST_JS });
  await page.addScriptTag({ path: CHAT_STREAM_JS });
  await page.addScriptTag({ path: CHAT_JS });
  return errors;
}

test('populates the model menu grouped by provider', async ({ page }) => {
  const errors = await boot(page, {});
  const model = page.locator('[data-chat-picker="model"]');
  await expect(model.locator('a[data-value="openai/deepseek-v4-flash"]')).toHaveCount(1);
  await expect(model.locator('a[data-value="openai/deepseek-v4-pro"]')).toHaveCount(1);
  await expect(model.locator('li.menu-title')).toHaveCount(1);
  await expect(model.locator('li.menu-title')).toHaveText('openai');
  await expect(model.locator('[data-chat-picker-label]')).toHaveText('Auto');
  expect(errors).toEqual([]);
});

test('restores the stored selection', async ({ page }) => {
  const errors = await boot(page, {
    'memory.chat.model.default': 'openai/deepseek-v4-pro',
    'memory.chat.thinking.default': 'high',
  });
  const model = page.locator('[data-chat-picker="model"]');
  const thinking = page.locator('[data-chat-picker="thinking"]');
  await expect(page.locator('#chat-form input[name="model"]')).toHaveValue('openai/deepseek-v4-pro');
  await expect(page.locator('#chat-form input[name="thinking"]')).toHaveValue('high');
  await expect(model.locator('[data-chat-picker-label]')).toHaveText('DeepSeek V4 Pro');
  await expect(thinking.locator('[data-chat-picker-label]')).toHaveText('High');
  expect(errors).toEqual([]);
});

test('persists changes to the current conversation and default slots', async ({ page }) => {
  const errors = await boot(page, {});
  const model = page.locator('[data-chat-picker="model"]');
  await expect(model.locator('a[data-value="openai/deepseek-v4-pro"]')).toHaveCount(1);
  await model.locator('a[data-value="openai/deepseek-v4-pro"]').dispatchEvent('click');
  await page.locator('[data-chat-picker="thinking"] a[data-value="medium"]').dispatchEvent('click');

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
  await expect(page.locator('#chat-form input[name="model"]')).toHaveValue('openai/deepseek-v4-pro');
  await expect(page.locator('#chat-form input[name="thinking"]')).toHaveValue('medium');
  await expect(model.locator('[data-chat-picker-label]')).toHaveText('DeepSeek V4 Pro');
  expect(errors).toEqual([]);
});
