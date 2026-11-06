import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Hermetic spec for A2UI surface actions over the HTTP chat transport (product
// #66). Before this wiring, `a2uiButton` dispatched an `a2ui:action` event and
// nothing listened: on the web chat an A2UI Accept/Reject did nothing at all.
//
// The renderer stays transport-free (it only reports the click); the /chat page
// turns that event into the conversation's next turn, and the server frames the
// message the agent receives. This spec drives the real page and the real
// renderer together and asserts the request body.
//
// No gateway, no memory API, no Zitadel — runs in CI.

const JS_DIR = path.resolve(__dirname, '../../../../gateway/webui/static/js');
const CHAT_TRANSPORT_JS = path.join(JS_DIR, 'chat-transport.js');
const CHAT_COMPONENTS_JS = path.join(JS_DIR, 'chat-components.js');
const CHAT_HOST_JS = path.join(JS_DIR, 'chat-host.js');
const CHAT_STREAM_JS = path.join(JS_DIR, 'chat-stream.js');
const CHAT_JS = path.join(JS_DIR, 'chat.js');

const SURFACE_ID = 'js-dom-surface-action';
const COMPONENT_ID = 'approval-1';

const SKELETON = `<!doctype html>
<html>
<head><meta charset="utf-8" /><style>.hidden { display: none !important; }</style></head>
<body data-org-id="org-1" data-project-id="proj-A">
<div id="chat-root" data-conv="conv-1">
  <aside id="chat-rail">
    <div id="chat-rail-resize" role="separator"></div>
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

/** Boots the /chat page with a stub `fetch` that records `/api/chat` bodies. */
async function boot(page: Page, options: { holdStream?: boolean } = {}): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));

  await page.route('http://memory.test/**', (route) =>
    route.fulfill({ contentType: 'text/html', body: SKELETON }),
  );
  await page.goto('http://memory.test/');

  await page.evaluate(({ holdStream }) => {
    const w = window as unknown as {
      fetch: typeof fetch;
      __chatBodies: unknown[];
    };
    w.__chatBodies = [];
    w.fetch = function (url: RequestInfo | URL, init?: RequestInit) {
      const u = String(url);
      if (u.indexOf('/partial/') === 0) {
        return Promise.resolve(new Response('', { status: 200 }));
      }
      if (u === '/api/chat') {
        w.__chatBodies.push(JSON.parse(String(init?.body ?? '{}')));
        if (holdStream) {
          // An open turn: the reply never finishes, so `streaming` stays true.
          const body = new ReadableStream({
            start(controller) {
              controller.enqueue(new TextEncoder().encode('data: {"type":"token","token":"hi"}\n\n'));
            },
          });
          return Promise.resolve(
            new Response(body, { status: 200, headers: { 'Content-Type': 'text/event-stream' } }),
          );
        }
        return Promise.resolve(
          new Response('data: {"type":"done"}\n\n', {
            status: 200,
            headers: { 'Content-Type': 'text/event-stream' },
          }),
        );
      }
      return Promise.resolve(new Response('[]', { status: 200 }));
    };
  }, { holdStream: options.holdStream === true });

  await page.addScriptTag({ path: CHAT_TRANSPORT_JS });
  await page.addScriptTag({ path: CHAT_COMPONENTS_JS });
  await page.addScriptTag({ path: CHAT_HOST_JS });
  await page.addScriptTag({ path: CHAT_STREAM_JS });
  await page.addScriptTag({ path: CHAT_JS });

  return errors;
}

/**
 * Renders one A2UI approval surface through the real shared renderer.
 *
 * The host is a fresh element on `document.body`, not `#chat-messages`: chat.js
 * owns that container and re-renders it asynchronously, which would delete the
 * card mid-test. The renderer appends into whatever host it is given, and the
 * click still travels the shipped `a2ui:action` event on `document`.
 */
async function renderApprovalCard(page: Page): Promise<void> {
  await page.evaluate(
    ({ surfaceId, componentId }) => {
      const host = document.createElement('div');
      host.id = 'a2ui-harness';
      document.body.appendChild(host);
      const components = (
        window as unknown as {
          MemoryChatComponents: {
            renderA2UISurface: (id: string, msgs: unknown[], ctx: { messages: HTMLElement }) => void;
          };
        }
      ).MemoryChatComponents;
      components.renderA2UISurface(
        surfaceId,
        [
          {
            updateComponents: {
              surfaceId,
              components: [
                { id: componentId, component: 'approval', tool: 'deploy', input: { path: '/tmp/x' } },
              ],
            },
          },
        ],
        { messages: host },
      );
    },
    { surfaceId: SURFACE_ID, componentId: COMPONENT_ID },
  );
}

test.describe('A2UI surface actions over HTTP (chat.js)', () => {
  test('an Approve click posts the action as the next chat turn', async ({ page }) => {
    const errors = await boot(page);
    await renderApprovalCard(page);

    await page.getByRole('button', { name: 'Approve' }).click();

    await expect
      .poll(async () => page.evaluate(() => (window as unknown as { __chatBodies: unknown[] }).__chatBodies.length))
      .toBe(1);

    const bodies = await page.evaluate(
      () => (window as unknown as { __chatBodies: Record<string, unknown>[] }).__chatBodies,
    );
    // The click is the turn: no typed text, and the surface id + action payload
    // travel untouched so the server can frame the agent's message.
    expect(bodies[0]).toEqual({
      agentDefinitionId: 'a1',
      conversationId: 'conv-1',
      message: '',
      surfaceAction: { surfaceId: SURFACE_ID, action: { componentId: COMPONENT_ID, response: 'approve' } },
    });
    expect(errors).toEqual([]);
  });

  test('a click while a turn streams is ignored', async ({ page }) => {
    const errors = await boot(page, { holdStream: true });
    await renderApprovalCard(page);

    await page.getByRole('button', { name: 'Approve' }).click();
    await expect
      .poll(async () => page.evaluate(() => (window as unknown as { __chatBodies: unknown[] }).__chatBodies.length))
      .toBe(1);

    // The second click must not queue a turn behind the open one.
    await page.getByRole('button', { name: 'Reject' }).click({ force: true });
    await page.waitForTimeout(150);
    expect(
      await page.evaluate(() => (window as unknown as { __chatBodies: unknown[] }).__chatBodies.length),
    ).toBe(1);
    expect(errors).toEqual([]);
  });
});
