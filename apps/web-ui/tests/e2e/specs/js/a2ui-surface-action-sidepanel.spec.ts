import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Hermetic spec for A2UI surface actions on the assistant side panel (#222).
//
// The shared renderer (`chat-components.js`) dispatches an `a2ui:action` event
// and owns no transport. Only the /chat page listened, so an A2UI Accept/Reject
// in the assistant drawer did nothing. The drawer now turns a click on a card
// inside ITS transcript into the conversation's next `/api/chat` turn, exactly
// like the chat page, and never answers a card it does not own.
//
// No gateway, no memory API, no Zitadel — runs in CI.

const SIDEPANEL_JS = path.resolve(__dirname, '../../../../gateway/webui/static/js/sidepanel.js');
const CHAT_TRANSPORT_JS = path.resolve(__dirname, '../../../../gateway/webui/static/js/chat-transport.js');
const CHAT_COMPONENTS_JS = path.resolve(__dirname, '../../../../gateway/webui/static/js/chat-components.js');
const CHAT_HOST_JS = path.resolve(__dirname, '../../../../gateway/webui/static/js/chat-host.js');
const CHAT_STREAM_JS = path.resolve(__dirname, '../../../../gateway/webui/static/js/chat-stream.js');

const SCOPE = { orgId: 'org-1', projectId: 'proj-1', userId: 'user-1', agentId: 'assistant' };
const CONV_ID = 'conv-1';
const SURFACE_ID = 'js-dom-sidepanel-surface';
const COMPONENT_ID = 'approval-1';

const SKELETON = `<!doctype html>
<html>
<head><meta charset="utf-8" /><style>.hidden { display: none !important; }</style></head>
<body>
<div id="sidepanel-root">
  <div id="sidepanel-backdrop" class="hidden"></div>
  <aside id="sidepanel-panel" data-agent-id="${SCOPE.agentId}" data-org-id="${SCOPE.orgId}" data-project-id="${SCOPE.projectId}" data-user-id="${SCOPE.userId}">
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

function storageKey(): string {
  return ['memory.sidepanel.v2', SCOPE.orgId, SCOPE.projectId, SCOPE.userId, SCOPE.agentId]
    .map((part, i) => (i === 0 ? part : encodeURIComponent(part)))
    .join('.');
}

/**
 * Boots the shipped drawer against a stub `fetch` that records `/api/chat`
 * bodies.
 *
 * The conversation id is restored from the panel's persisted envelope, because
 * that is the only conversation a click may be posted on. The boot history load
 * is held open on purpose: a resolved load empties `#sidepanel-messages`, which
 * would delete the card this harness renders there (a real drawer re-renders its
 * own cards).
 */
async function boot(page: Page, options: { holdStream?: boolean } = {}): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));

  await page.route('http://memory.test/**', (route) =>
    route.fulfill({ contentType: 'text/html', body: SKELETON }),
  );
  await page.goto('http://memory.test/');

  await page.evaluate(
    ({ key, scope, convId, holdStream }) => {
      const w = window as unknown as { fetch: typeof fetch; __chatBodies: unknown[] };
      localStorage.clear();
      localStorage.setItem(
        key,
        JSON.stringify({ scope, conversationId: convId, history: [] }),
      );
      w.__chatBodies = [];
      w.fetch = function (url: RequestInfo | URL, init?: RequestInit) {
        const u = String(url);
        if (/^\/api\/conversations\/[^/]+\/history$/.test(u)) {
          // Held open: see the boot comment above.
          return new Promise<Response>(function () {});
        }
        if (u === '/api/conversations') {
          return Promise.resolve(
            new Response(JSON.stringify({ conversations: [] }), {
              status: 200,
              headers: { 'Content-Type': 'application/json' },
            }),
          );
        }
        if (u === '/api/chat') {
          w.__chatBodies.push(JSON.parse(String(init?.body ?? '{}')));
          if (holdStream) {
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
    },
    { key: storageKey(), scope: SCOPE, convId: CONV_ID, holdStream: options.holdStream === true },
  );

  await page.addScriptTag({ path: CHAT_TRANSPORT_JS });
  await page.addScriptTag({ path: CHAT_COMPONENTS_JS });
  await page.addScriptTag({ path: CHAT_HOST_JS });
  await page.addScriptTag({ path: CHAT_STREAM_JS });
  await page.addScriptTag({ path: SIDEPANEL_JS });

  await expect(page.locator('#sidepanel-panel')).toHaveAttribute('data-ready', '1');
  return errors;
}

/** Renders one A2UI approval surface through the real shared renderer. */
async function renderApprovalCard(page: Page, hostId: string): Promise<void> {
  await page.evaluate(
    ({ surfaceId, componentId, hostId }) => {
      const host = document.getElementById(hostId) as HTMLElement;
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
    { surfaceId: SURFACE_ID, componentId: COMPONENT_ID, hostId },
  );
}

function postedBodies(page: Page) {
  return page.evaluate(() => (window as unknown as { __chatBodies: unknown[] }).__chatBodies);
}

test.describe('A2UI surface actions in the assistant drawer (sidepanel.js)', () => {
  test('an Approve click inside the drawer posts the action on its conversation', async ({ page }) => {
    const errors = await boot(page);
    await renderApprovalCard(page, 'sidepanel-messages');

    await page.getByRole('button', { name: 'Approve' }).click();

    await expect.poll(async () => (await postedBodies(page)).length).toBe(1);

    const bodies = await postedBodies(page);
    expect(bodies[0]).toEqual({
      agentDefinitionId: 'assistant',
      conversationId: CONV_ID,
      message: '',
      surfaceAction: {
        surfaceId: SURFACE_ID,
        action: { componentId: COMPONENT_ID, response: 'approve' },
      },
    });
    expect(errors).toEqual([]);
  });

  test('a card outside the drawer transcript posts nothing', async ({ page }) => {
    const errors = await boot(page);
    // The /chat page renders the same card in its own transcript. The drawer
    // must not answer a click on a card it does not own.
    await page.evaluate(() => {
      const host = document.createElement('div');
      host.id = 'a2ui-harness';
      document.body.appendChild(host);
    });
    await renderApprovalCard(page, 'a2ui-harness');

    await page.getByRole('button', { name: 'Approve' }).click();
    await page.waitForTimeout(150);

    expect(await postedBodies(page)).toEqual([]);
    expect(errors).toEqual([]);
  });

  test('a click while a turn streams is ignored', async ({ page }) => {
    const errors = await boot(page, { holdStream: true });
    await renderApprovalCard(page, 'sidepanel-messages');

    await page.getByRole('button', { name: 'Approve' }).click();
    await expect.poll(async () => (await postedBodies(page)).length).toBe(1);

    await page.getByRole('button', { name: 'Reject' }).click({ force: true });
    await page.waitForTimeout(150);
    expect(await postedBodies(page)).toHaveLength(1);
    expect(errors).toEqual([]);
  });
});
