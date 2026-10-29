import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Hermetic DOM-level wiring spec for the inline object-reference preview drawer
// (web-object-reference-preview): the chrome and click-routing contract that the
// Go unit tests cannot exercise because it lives in object-preview.js.
//
// Covers, in a real Chromium engine:
//   - a plain left click on a chat object reference opens the drawer, points the
//     header edit action at the reference, shows the loading state, and asks
//     HTMX for GET /objects/<id>/preview (nothing navigates);
//   - modified clicks, `target`/`download` links, multi-segment (human key)
//     refs, non-object links, and object links outside chat content are left to
//     native navigation;
//   - a source row inside the in-bubble Sources footer opens the drawer, as does
//     a reference in the assistant side panel;
//   - Escape and a backdrop click close the drawer and restore focus to the
//     opener, re-inerting the closed panel;
//   - a failed preview request renders the "Object unavailable" state instead of
//     leaving the spinner in place;
//   - Tab is trapped inside the open drawer.
//
// Every surface below is markup the app actually renders: the sources footer
// chain is chat-components.js sourcesFooter()/sourceRow() (details[data-testid=
// chat-sources] > summary > .collapse-content > li[data-testid=chat-source] > a),
// mounted inside the assistant bubble. object-preview.js also has a branch keying
// on data-testid="citation-link", which no template or renderer has emitted since
// #1228; it is dead (issue #2251) and deliberately not exercised here — a spec
// against it would pass against a surface that does not exist.
//
// Loads the shipped object-preview.js verbatim into a bare page with the same
// shell markup objectPreviewDrawer() emits, a stubbed MemoryChatHost (resize
// grip) and a recording stub for htmx.ajax. No gateway, no memory API, no auth.

const OBJECT_PREVIEW_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/object-preview.js',
);

const OBJECT_ID = 'edbfbfe7-1331-405a-818f-791491c161b8';

// Mirrors gateway/object_preview.templ objectPreviewDrawer(): the aside is
// closed (translate-x-full + aria-hidden + inert) until object-preview.js opens
// it, and the edit action starts at href="#" (pointed at the reference on open).
const PAGE_HTML = `<!doctype html><html><head><style>
  /* The only utilities this harness needs: .hidden must actually hide (the
     drawer toggles it on the backdrop and the aside keeps its own class-based
     open state), and the drawer chrome needs real geometry so a mouse click can
     land on the backdrop. Tailwind is not loaded here. */
  .hidden { display: none !important; }
  #object-preview-backdrop { position: fixed; inset: 0; background: rgba(0, 0, 0, 0.4); }
  #object-preview-panel { position: fixed; top: 0; right: 0; width: 512px; height: 100vh; }
</style></head><body>
  <main id="main-content">
    <div id="chat-messages" data-testid="chat-messages">
      <div class="chat chat-start">
        <div class="chat-bubble">
          <div class="memory-md">
            <p>See <a id="chat-ref" href="/objects/${OBJECT_ID}">the scenario</a> for details.</p>
            <p><a id="chat-multi" href="/objects/my-scenario/notes">nested</a></p>
            <p><a id="chat-target" href="/objects/${OBJECT_ID}" target="_blank">new tab</a></p>
            <p><a id="chat-download" href="/objects/${OBJECT_ID}" download>download</a></p>
            <p><a id="chat-plain" href="/projects">projects</a></p>
          </div>
          <!-- In-bubble Sources footer: the shape chat-components.js renders. -->
          <details class="collapse collapse-arrow" data-testid="chat-sources">
            <summary class="collapse-title" data-testid="chat-sources-toggle">
              <span data-testid="chat-sources-count">1 source</span>
            </summary>
            <div class="collapse-content">
              <ul>
                <li data-testid="chat-source">
                  <a id="source-ref" class="memory-source" href="/objects/${OBJECT_ID}">Scenario one</a>
                </li>
                <li data-testid="chat-source">
                  <span class="memory-source">a source without a link</span>
                </li>
              </ul>
            </div>
          </details>
        </div>
      </div>
    </div>
    <div id="sidepanel-messages">
      <div class="memory-md">
        <p><a id="sidepanel-ref" href="/objects/${OBJECT_ID}">side panel reference</a></p>
      </div>
    </div>
    <!-- The excluded surface: markdown rendered outside both chat lists, e.g. the
         object-detail knowledge answer. Same .memory-md renderer, no interception. -->
    <div id="knowledge-answer" class="memory-md">
      <a id="detail-ref" href="/objects/${OBJECT_ID}">knowledge answer</a>
    </div>
  </main>
  <div id="object-preview-root">
    <div id="object-preview-backdrop" class="fixed inset-0 z-40 hidden bg-black/40"></div>
    <aside id="object-preview-panel" class="translate-x-full" role="dialog"
           aria-modal="true" aria-label="Object preview" aria-hidden="true" inert>
      <div id="object-preview-resize" role="separator" aria-orientation="vertical"
           aria-label="Resize object preview" tabindex="0" aria-valuemin="320"
           aria-valuemax="1120" aria-valuenow="512"></div>
      <div class="header">
        <a id="object-preview-edit" href="#">Open</a>
        <button id="object-preview-width-toggle" type="button" aria-pressed="false">width</button>
        <button id="object-preview-open-modal" type="button">modal</button>
        <button id="object-preview-close" type="button">close</button>
      </div>
      <div id="object-preview-body"></div>
    </aside>
  </div>
</body></html>`;

interface Stubs {
  ajaxCalls: Array<{ method: string; url: string; target: string }>;
  nativeClicks: string[];
}

async function bootstrap(page: Page): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));

  // Inline handlers record clicks that reach the anchor, which only happens when
  // the drawer's capture-phase listener did NOT intercept the event. They also
  // suppress the anchor's own navigation: the harness runs on about:blank, so a
  // real navigation would tear the fixture (and the stubs) down mid-test — the
  // subject here is whether the drawer intercepts, not where the href points.
  const html = PAGE_HTML.replace(
    /<a id="([^"]+)"([^>]*)>/g,
    '<a id="$1"$2 onclick="window.__nativeClicks = (window.__nativeClicks || []).concat(this.id); ' +
      'event.preventDefault();">',
  );
  await page.setContent(html);
  await page.evaluate(() => {
    // object-preview.js constructs a resize grip at module-eval time; provide the
    // smallest viable stand-in (chat-host.js is the real implementation).
    (window as unknown as { MemoryChatHost: unknown }).MemoryChatHost = {
      createResizeGrip: () => ({
        handle: null,
        init: () => {},
        onPointerDown: () => {},
        onWindowResize: () => {},
        setWidth: () => {},
        getWidth: () => 512,
      }),
    };
    const stubs = window as unknown as {
      __ajaxCalls: Array<{ method: string; url: string; target: string }>;
      htmx: { ajax: (method: string, url: string, opts: { target?: string }) => Promise<void> };
    };
    stubs.__ajaxCalls = [];
    stubs.htmx = {
      ajax: (method, url, opts) => {
        stubs.__ajaxCalls.push({ method, url, target: opts?.target ?? '' });
        return Promise.resolve();
      },
    };
  });
  await page.addScriptTag({ path: OBJECT_PREVIEW_JS });
  return errors;
}

async function stubs(page: Page): Promise<Stubs> {
  return page.evaluate(() => ({
    ajaxCalls: (window as unknown as { __ajaxCalls: Stubs['ajaxCalls'] }).__ajaxCalls,
    nativeClicks: (window as unknown as { __nativeClicks?: string[] }).__nativeClicks ?? [],
  }));
}

async function drawerState(page: Page) {
  return page.evaluate(() => {
    const panel = document.getElementById('object-preview-panel');
    const backdrop = document.getElementById('object-preview-backdrop');
    const edit = document.getElementById('object-preview-edit');
    const body = document.getElementById('object-preview-body');
    if (!panel || !backdrop || !edit || !body) throw new Error('missing drawer chrome');
    return {
      open: !panel.classList.contains('translate-x-full'),
      ariaHidden: panel.getAttribute('aria-hidden'),
      inert: panel.hasAttribute('inert'),
      backdropVisible: !backdrop.classList.contains('hidden'),
      editHref: edit.getAttribute('href'),
      bodyText: (body.textContent ?? '').trim(),
      activeId: document.activeElement?.id ?? '',
    };
  });
}

test.describe('Object preview drawer shell', () => {
  test('a plain left click on a chat reference opens the drawer and loads the preview', async ({ page }) => {
    const errors = await bootstrap(page);

    await page.locator('#chat-ref').click();

    const state = await drawerState(page);
    expect(state.open).toBe(true);
    expect(state.ariaHidden).toBe('false');
    expect(state.inert).toBe(false);
    expect(state.backdropVisible).toBe(true);
    expect(state.editHref).toBe(`/objects/${OBJECT_ID}`);
    // The drawer takes focus so the next Tab lands inside it, and the body shows
    // the loading state until the swap arrives.
    expect(state.activeId).toBe('object-preview-close');
    expect(state.bodyText).toContain('Loading preview');

    const recorded = await stubs(page);
    expect(recorded.ajaxCalls).toEqual([
      { method: 'GET', url: `/objects/${OBJECT_ID}/preview`, target: '#object-preview-body' },
    ]);
    // Interception stops propagation, so the anchor's own handler never runs and
    // nothing navigates.
    expect(recorded.nativeClicks).toEqual([]);

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('a source row in the in-bubble Sources footer opens the drawer', async ({ page }) => {
    const errors = await bootstrap(page);

    // The footer renders collapsed, so open it the way a user does first.
    await page.locator('[data-testid="chat-sources-toggle"]').click();
    await page.locator('#source-ref').click();

    const state = await drawerState(page);
    expect(state.open).toBe(true);
    expect(state.editHref).toBe(`/objects/${OBJECT_ID}`);
    const recorded = await stubs(page);
    expect(recorded.ajaxCalls.map((c) => c.url)).toEqual([`/objects/${OBJECT_ID}/preview`]);
    expect(recorded.nativeClicks).toEqual([]);

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('a reference in the assistant side panel opens the drawer', async ({ page }) => {
    const errors = await bootstrap(page);

    await page.locator('#sidepanel-ref').click();

    const state = await drawerState(page);
    expect(state.open).toBe(true);
    expect(state.editHref).toBe(`/objects/${OBJECT_ID}`);
    const recorded = await stubs(page);
    expect(recorded.ajaxCalls.map((c) => c.url)).toEqual([`/objects/${OBJECT_ID}/preview`]);
    expect(recorded.nativeClicks).toEqual([]);

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('clicks the drawer must not intercept fall through to native navigation', async ({ page }) => {
    const errors = await bootstrap(page);

    // Modified click: cmd/ctrl-click keeps the platform's new-tab behaviour.
    await page.locator('#chat-ref').click({ modifiers: ['ControlOrMeta'] });
    // A human-key reference cannot be addressed by the single-segment preview
    // route, so it navigates.
    await page.locator('#chat-multi').click();
    // `target` and `download` opt out explicitly.
    await page.locator('#chat-target').click();
    await page.locator('#chat-download').click();
    // Non-object links are none of the drawer's business.
    await page.locator('#chat-plain').click();
    // Object links outside chat content (e.g. the object-detail knowledge
    // answer) navigate normally.
    await page.locator('#detail-ref').click();

    const recorded = await stubs(page);
    expect(recorded.ajaxCalls).toEqual([]);
    expect(recorded.nativeClicks).toEqual([
      'chat-ref',
      'chat-multi',
      'chat-target',
      'chat-download',
      'chat-plain',
      'detail-ref',
    ]);

    const state = await drawerState(page);
    expect(state.open).toBe(false);

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('Escape closes the drawer and restores focus to the opener', async ({ page }) => {
    const errors = await bootstrap(page);

    await page.locator('#chat-ref').focus();
    await page.locator('#chat-ref').click();
    expect((await drawerState(page)).open).toBe(true);

    await page.keyboard.press('Escape');

    const state = await drawerState(page);
    expect(state.open).toBe(false);
    expect(state.ariaHidden).toBe('true');
    expect(state.inert).toBe(true);
    expect(state.backdropVisible).toBe(false);
    expect(state.bodyText).toBe('');
    expect(state.activeId).toBe('chat-ref');

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('a backdrop click closes the drawer', async ({ page }) => {
    const errors = await bootstrap(page);

    await page.locator('#chat-ref').click();
    expect((await drawerState(page)).open).toBe(true);

    await page.locator('#object-preview-backdrop').click({ position: { x: 4, y: 4 } });

    const state = await drawerState(page);
    expect(state.open).toBe(false);
    expect(state.backdropVisible).toBe(false);
    expect(state.inert).toBe(true);

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('a failed preview request renders the unavailable state, not a stuck spinner', async ({ page }) => {
    const errors = await bootstrap(page);

    await page.locator('#chat-ref').click();
    await page.evaluate(() => {
      document.dispatchEvent(
        new CustomEvent('htmx:response:error', {
          detail: { ctx: { target: document.getElementById('object-preview-body') } },
        }),
      );
    });

    const state = await drawerState(page);
    expect(state.bodyText).toContain('Object unavailable');
    expect(state.bodyText).toContain('could not be loaded');
    expect(state.bodyText).not.toContain('Loading preview');
    // The drawer stays open so the failure is visible rather than vanishing.
    expect(state.open).toBe(true);

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('Tab is trapped inside the open drawer', async ({ page }) => {
    const errors = await bootstrap(page);

    await page.locator('#chat-ref').click();

    const focusedLast = await page.evaluate(() => {
      const panel = document.getElementById('object-preview-panel');
      if (!panel) throw new Error('missing panel');
      const items = Array.from(
        panel.querySelectorAll<HTMLElement>('a[href], button:not([disabled]), [tabindex]:not([tabindex="-1"])'),
      ).filter((el) => el.offsetParent !== null);
      if (!items.length) throw new Error('no focusable controls in the drawer');
      items[items.length - 1].focus();
      return { id: document.activeElement?.id ?? '', first: items[0].id };
    });
    expect(focusedLast.id).not.toBe('');

    await page.keyboard.press('Tab');

    const activeId = await page.evaluate(() => document.activeElement?.id ?? '');
    expect(activeId).toBe(focusedLast.first);

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });
});
