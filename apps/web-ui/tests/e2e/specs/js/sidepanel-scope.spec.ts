import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Regression gate for the assistant drawer's persisted-conversation scope.
//
// The drawer stored its in-flight conversation under ONE browser-global
// localStorage key (`memory.sidepanel.v1`) with no org/project/user in it, and
// `restore()` painted that transcript back on every page load. Because the
// drawer only mounts once a project has an assistant agent, a brand-new project
// that had just been pointed at an assistant rehydrated the *previous*
// project's conversation — a fresh project appearing to already contain a
// session (and, on a shared browser, one account's transcript appearing to
// another).
//
// The contract pinned here, against the shipped scripts loaded verbatim in a
// real Chromium engine (same hermetic harness as timeline-render.spec.ts — no
// gateway, no memory API, no auth):
//
//   1. a transcript persisted for THIS org+project+user+assistant is restored;
//   2. a transcript persisted for any other scope is never rendered, whatever
//      scope field differs (project, org, user, assistant agent);
//   3. the retired unscoped key is dropped rather than adopted;
//   4. a payload whose stored scope disagrees with the live panel is discarded
//      and deleted, even when it sits under the live key.
//
// Non-vacuous: with the old global key each cross-scope case renders the
// foreign transcript and fails.

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

// The panel's live scope. The skeleton renders it the way sidepanel.templ does
// from the request session.
const LIVE_SCOPE = {
  orgId: 'org-1',
  projectId: 'proj-1',
  userId: 'user-1',
  agentId: 'assistant',
};

const FOREIGN_TEXT = 'transcript from another project';

function skeleton(scope: typeof LIVE_SCOPE): string {
  return `<!doctype html>
<html>
<head><meta charset="utf-8" /><style>.hidden { display: none !important; }</style></head>
<body>
<div id="sidepanel-root">
  <div id="sidepanel-backdrop" class="hidden"></div>
  <aside id="sidepanel-panel" data-agent-id="${scope.agentId}" data-org-id="${scope.orgId}" data-project-id="${scope.projectId}" data-user-id="${scope.userId}" class="translate-x-full" inert aria-hidden="true">
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
}

async function loadCoreScripts(page: Page): Promise<void> {
  await page.addScriptTag({ path: CHAT_TRANSPORT_JS });
  await page.addScriptTag({ path: CHAT_COMPONENTS_JS });
  await page.addScriptTag({ path: CHAT_HOST_JS });
  await page.addScriptTag({ path: CHAT_STREAM_JS });
}

// bootPanel opens a real HTTP origin (localStorage needs one), seeds storage,
// stubs the panel's two backend calls, then boots the shipped scripts. Seeding
// happens before sidepanel.js loads because restore() runs at init.
async function bootPanel(
  page: Page,
  seed: { key: string; value: unknown } | null,
): Promise<void> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));

  await page.route('http://memory.test/**', (route) =>
    route.fulfill({ contentType: 'text/html', body: skeleton(LIVE_SCOPE) }),
  );
  await page.goto('http://memory.test/');

  await page.evaluate((payload) => {
    localStorage.clear();
    if (payload) {
      localStorage.setItem(payload.key, JSON.stringify(payload.value));
    }
    const w = window as unknown as { fetch: typeof fetch };
    w.fetch = function (url: RequestInfo | URL) {
      const u = String(url);
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
  }, seed);

  await loadCoreScripts(page);
  await page.addScriptTag({ path: SIDEPANEL_JS });

  // init() is synchronous at load (readyState is "complete" after goto), so a
  // failure to boot would surface here rather than as an empty log.
  await expect(page.locator('#sidepanel-panel')).toHaveAttribute('data-ready', '1');
  expect(errors).toEqual([]);
}

// transcriptFor builds the persisted payload exactly as sidepanel.js does.
function transcriptFor(scope: typeof LIVE_SCOPE, text: string) {
  return {
    scope,
    conversationId: '',
    history: [
      { role: 'user', text },
      { role: 'assistant', html: `<p>answer to ${text}</p>` },
    ],
  };
}

// storageKey mirrors sidepanel.js's storageKey(): prefix plus the four encoded
// scope fields.
function storageKey(scope: typeof LIVE_SCOPE): string {
  return ['memory.sidepanel.v2', scope.orgId, scope.projectId, scope.userId, scope.agentId]
    .map((part, i) => (i === 0 ? part : encodeURIComponent(part)))
    .join('.');
}

test.describe('assistant drawer — persisted transcript is scoped', () => {
  test('restores the transcript stored for this org+project+user+assistant', async ({ page }) => {
    await bootPanel(page, {
      key: storageKey(LIVE_SCOPE),
      value: transcriptFor(LIVE_SCOPE, 'hello from this project'),
    });

    const messages = page.locator('#sidepanel-messages');
    await expect(messages).toContainText('hello from this project');
    await expect(messages).toContainText('answer to hello from this project');
  });

  // Every scope field is load-bearing: dropping any one of them re-opens a
  // cross-tenancy path (another project, another org, another account on a
  // shared browser, or another assistant agent after an admin swap).
  for (const [label, other] of [
    ['another project', { ...LIVE_SCOPE, projectId: 'proj-2' }],
    ['another org', { ...LIVE_SCOPE, orgId: 'org-2' }],
    ['another user', { ...LIVE_SCOPE, userId: 'user-2' }],
    ['another assistant agent', { ...LIVE_SCOPE, agentId: 'other-agent' }],
  ] as const) {
    test(`never renders a transcript stored for ${label}`, async ({ page }) => {
      await bootPanel(page, {
        key: storageKey(other),
        value: transcriptFor(other, FOREIGN_TEXT),
      });

      await expect(page.locator('#sidepanel-messages')).toBeEmpty();
      // The foreign payload is not ours to touch — it stays available to the
      // tenancy that owns it.
      expect(await page.evaluate((key) => localStorage.getItem(key), storageKey(other))).not.toBeNull();
    });
  }

  test('drops the retired unscoped key instead of adopting it', async ({ page }) => {
    await bootPanel(page, {
      key: 'memory.sidepanel.v1',
      value: { conversationId: '', history: [{ role: 'user', text: FOREIGN_TEXT }] },
    });

    await expect(page.locator('#sidepanel-messages')).toBeEmpty();
    expect(await page.evaluate(() => localStorage.getItem('memory.sidepanel.v1'))).toBeNull();
  });

  test('discards a payload whose stored scope disagrees with the live panel', async ({ page }) => {
    // Same key (so it *is* read), but the envelope claims another project — the
    // second line of defence for a hand-edited or migrated payload.
    await bootPanel(page, {
      key: storageKey(LIVE_SCOPE),
      value: transcriptFor({ ...LIVE_SCOPE, projectId: 'proj-2' }, FOREIGN_TEXT),
    });

    await expect(page.locator('#sidepanel-messages')).toBeEmpty();
    expect(await page.evaluate((key) => localStorage.getItem(key), storageKey(LIVE_SCOPE))).toBeNull();
  });
});
