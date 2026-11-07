import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Hermetic spec for A2UI surface actions on the public share page (#222).
//
// The share page is the third host of the shared A2UI renderer. Its stream is
// session-scoped (`/share/api/chat` under the share cookie), so a click has to
// ride that route rather than `/api/chat`. The decided contract: `surfaceAction`
// is a field of the share turn, framed server-side with the same wording as
// `/api/chat` (see `A2UIActionMessage`), so a public visitor gets the same
// behaviour as an authenticated one and no client invents the prose.
//
// No gateway, no memory API, no Zitadel — runs in CI.

const SHARE_AGENT_JS = path.resolve(__dirname, '../../../../gateway/webui/static/js/share-agent.js');
const CHAT_TRANSPORT_JS = path.resolve(__dirname, '../../../../gateway/webui/static/js/chat-transport.js');
const CHAT_COMPONENTS_JS = path.resolve(__dirname, '../../../../gateway/webui/static/js/chat-components.js');
const CHAT_HOST_JS = path.resolve(__dirname, '../../../../gateway/webui/static/js/chat-host.js');
const CHAT_STREAM_JS = path.resolve(__dirname, '../../../../gateway/webui/static/js/chat-stream.js');

const SESSION_ID = 'sess-1';
const SURFACE_ID = 'js-dom-share-surface';
const COMPONENT_ID = 'approval-1';

const SKELETON = `<!doctype html>
<html>
<head><meta charset="utf-8" /><style>.hidden { display: none !important; }</style></head>
<body>
<div id="share-root" data-active-session="${SESSION_ID}">
  <header><span id="share-agent-name"></span><span id="share-agent-description" class="hidden"></span></header>
  <div id="share-avatar"></div>
  <div id="share-session-rail">
    <div id="share-session-list" data-sessions-partial="/share/api/partial/sessions"></div>
    <select id="share-archived-filter"><option value="active">Active</option></select>
  </div>
  <div id="share-chat-log">
    <div id="share-first-load"><div id="share-first-load-desc"></div><div id="share-first-load-avatar"></div></div>
    <div id="share-inline-terminal" class="hidden"></div>
    <div id="share-messages"></div>
  </div>
  <div id="share-email-gate" class="hidden">
    <form id="share-email-form"><input id="share-email-input" /><span id="share-email-error"></span></form>
  </div>
  <form id="share-composer">
    <textarea id="share-input" rows="1" aria-label="Message"></textarea>
    <button id="share-send" type="submit">Send</button>
    <span id="share-stop" class="hidden"><button type="button">Stop</button></span>
  </form>
</div>
</body>
</html>`;

/**
 * Boots the shipped share page against a stub `fetch` that records
 * `/share/api/chat` bodies.
 *
 * The transcript reload is held open on purpose: a resolved load re-renders
 * `#share-messages` from the (empty) server timeline, which would delete the
 * card this harness renders there.
 */
async function boot(page: Page, options: { holdStream?: boolean } = {}): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));

  await page.route('http://memory.test/**', (route) =>
    route.fulfill({ contentType: 'text/html', body: SKELETON }),
  );
  await page.goto('http://memory.test/');

  await page.evaluate((holdStream) => {
    const w = window as unknown as { fetch: typeof fetch; __shareBodies: unknown[] };
    w.__shareBodies = [];
    w.fetch = function (url: RequestInfo | URL, init?: RequestInit) {
      const u = String(url);
      if (u === '/share/api/config') {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              ok: true,
              agentName: 'Memory',
              allowEndUserApprovals: true,
              showSessionList: true,
            }),
            { status: 200, headers: { 'Content-Type': 'application/json' } },
          ),
        );
      }
      if (/^\/share\/api\/sessions\/[^/]+$/.test(u)) {
        // Held open: see the boot comment above.
        return new Promise<Response>(function () {});
      }
      if (u.indexOf('/share/api/partial/sessions') === 0) {
        return Promise.resolve(new Response('', { status: 200 }));
      }
      if (u === '/share/api/chat') {
        w.__shareBodies.push(JSON.parse(String(init?.body ?? '{}')));
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
  }, options.holdStream === true);

  await page.addScriptTag({ path: CHAT_TRANSPORT_JS });
  await page.addScriptTag({ path: CHAT_COMPONENTS_JS });
  await page.addScriptTag({ path: CHAT_HOST_JS });
  await page.addScriptTag({ path: CHAT_STREAM_JS });
  await page.addScriptTag({ path: SHARE_AGENT_JS });

  // The exchange/config pass records the approvals flag on the root before the
  // card renderer reads it.
  await expect(page.locator('#share-root')).toHaveAttribute('data-allow-approvals', 'true');
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
            renderA2UISurface: (
              id: string,
              msgs: unknown[],
              ctx: { messages: HTMLElement; approvalsEnabled: () => boolean },
            ) => void;
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
        { messages: host, approvalsEnabled: () => true },
      );
    },
    { surfaceId: SURFACE_ID, componentId: COMPONENT_ID, hostId },
  );
}

function postedBodies(page: Page) {
  return page.evaluate(() => (window as unknown as { __shareBodies: unknown[] }).__shareBodies);
}

test.describe('A2UI surface actions on the public share page (share-agent.js)', () => {
  test('an Approve click posts the action on the share chat route', async ({ page }) => {
    const errors = await boot(page);
    await renderApprovalCard(page, 'share-messages');

    await page.getByRole('button', { name: 'Approve' }).click();

    await expect.poll(async () => (await postedBodies(page)).length).toBe(1);

    const bodies = await postedBodies(page);
    expect(bodies[0]).toEqual({
      sessionId: SESSION_ID,
      message: '',
      surfaceAction: {
        surfaceId: SURFACE_ID,
        action: { componentId: COMPONENT_ID, response: 'approve' },
      },
    });
    expect(errors).toEqual([]);
  });

  test('a card outside the share transcript posts nothing', async ({ page }) => {
    const errors = await boot(page);
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
    await renderApprovalCard(page, 'share-messages');

    await page.getByRole('button', { name: 'Approve' }).click();
    await expect.poll(async () => (await postedBodies(page)).length).toBe(1);

    await page.getByRole('button', { name: 'Reject' }).click({ force: true });
    await page.waitForTimeout(150);
    expect(await postedBodies(page)).toHaveLength(1);
    expect(errors).toEqual([]);
  });
});
