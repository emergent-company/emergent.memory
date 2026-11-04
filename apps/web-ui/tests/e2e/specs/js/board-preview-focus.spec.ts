import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Hermetic DOM-level wiring spec for #1427: after a successful board action in
// the shared object preview drawer, focus must land on a sensible in-page
// element (the refreshed card, or the board container) — never <body>.
//
// Root cause: htmx v4 fires `htmx:after:request` BEFORE it swaps the response,
// so object-preview.js closed the drawer and restored focus to the card that
// opened it, then the `#board` outerHTML swap removed that card and focus fell
// to <body>. The fix defers the restore to `htmx:after:swap` and re-finds the
// card by data-canonical-id (falling back to #board, which carries tabindex="-1").
//
// Loads app.js's sibling object-preview.js verbatim into a bare page with a
// stubbed MemoryChatHost (resize grip) and htmx. No gateway, no memory API.

const OBJECT_PREVIEW_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/object-preview.js',
);

const PAGE_HTML = `<!doctype html><html><body>
  <div id="board" tabindex="-1" data-testid="board">
    <section data-board-column="ready"><div class="lane-body">
      <div id="card-w1" draggable="true" data-board-card data-board-status="ready"
           data-board-open-preview aria-haspopup="dialog" role="button" tabindex="0"
           data-canonical-id="w1">card</div>
    </div></section>
    <section data-board-column="review"><div class="lane-body"></div></section>
  </div>
  <div id="object-preview-root">
    <div id="object-preview-backdrop" class="hidden"></div>
    <aside id="object-preview-panel" class="translate-x-full" role="dialog"
           aria-modal="true" aria-hidden="true" inert>
      <button id="object-preview-close" type="button">close</button>
      <div id="object-preview-body"></div>
      <div id="object-preview-actions-slot">
        <button id="action-approve" type="button" hx-post="/board/items/w1/approve">Approve</button>
      </div>
    </aside>
  </div>
</body></html>`;

async function bootstrap(page: Page): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));

  await page.setContent(PAGE_HTML);
  await page.evaluate(() => {
    // object-preview.js constructs a resize grip at module-eval time; provide
    // the smallest viable stand-in (chat-host.js is the real implementation).
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
    (window as unknown as { htmx: unknown }).htmx = { ajax: () => Promise.resolve() };
  });
  await page.addScriptTag({ path: OBJECT_PREVIEW_JS });
  return errors;
}

// Dispatch the htmx event object-preview.js inspects: a successful board action
// whose source element lives in the preview's action slot.
async function fireSuccessfulAction(page: Page): Promise<void> {
  await page.evaluate(() => {
    document.dispatchEvent(
      new CustomEvent('htmx:after:request', {
        detail: {
          ctx: {
            sourceElement: document.getElementById('action-approve'),
            response: { raw: { status: 200 } },
          },
        },
      }),
    );
  });
}

// Replace the board with fresh markup carrying a same-canonical-id card, as the
// board action's outerHTML swap of #board does, then fire htmx:after:swap.
async function swapBoard(page: Page, inner: string): Promise<void> {
  await page.evaluate((html) => {
    const board = document.getElementById('board');
    if (!board) throw new Error('missing #board');
    board.innerHTML = html;
    board.dispatchEvent(new CustomEvent('htmx:after:swap', { bubbles: true }));
  }, inner);
}

test.describe('Board preview action focus (#1427)', () => {
  test('focus returns to the refreshed card after the board swap', async ({ page }) => {
    const errors = await bootstrap(page);

    await page.evaluate(() => {
      const card = document.getElementById('card-w1');
      card?.focus();
      (window as unknown as { MemoryObjectPreview: { open: (r: unknown) => void } }).MemoryObjectPreview.open({
        id: 'w1',
        editHref: '/objects/w1',
        actions: 'board',
      });
    });

    // The action resolves and the preview closes, but the swap is still pending.
    await fireSuccessfulAction(page);

    // The board swap re-renders #board, moving the card to another lane.
    await swapBoard(
      page,
      `<section data-board-column="review"><div class="lane-body">
         <div id="card-w1" draggable="true" data-board-card data-board-status="review"
              data-board-open-preview role="button" tabindex="0"
              data-canonical-id="w1">card</div>
       </div></section>`,
    );

    const active = await page.evaluate(() => {
      const el = document.activeElement;
      return { id: el?.id ?? '', tag: el?.tagName ?? '' };
    });
    expect(active.id).toBe('card-w1');
    expect(active.tag).not.toBe('BODY');

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('focus falls back to #board when the card is gone after the swap', async ({ page }) => {
    const errors = await bootstrap(page);

    await page.evaluate(() => {
      document.getElementById('card-w1')?.focus();
      (window as unknown as { MemoryObjectPreview: { open: (r: unknown) => void } }).MemoryObjectPreview.open({
        id: 'w1',
        editHref: '/objects/w1',
        actions: 'board',
      });
    });

    await fireSuccessfulAction(page);
    await swapBoard(page, '<section data-board-column="ready"><div class="lane-body"></div></section>');

    const active = await page.evaluate(() => ({
      id: document.activeElement?.id ?? '',
      tag: document.activeElement?.tagName ?? '',
    }));
    expect(active.id).toBe('board');
    expect(active.tag).not.toBe('BODY');

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('a manual close still restores focus immediately to the opener', async ({ page }) => {
    const errors = await bootstrap(page);

    await page.evaluate(() => {
      document.getElementById('card-w1')?.focus();
      (window as unknown as { MemoryObjectPreview: { open: (r: unknown) => void } }).MemoryObjectPreview.open({
        id: 'w1',
        editHref: '/objects/w1',
        actions: 'board',
      });
    });

    await page.locator('#object-preview-close').click();

    const activeId = await page.evaluate(() => document.activeElement?.id ?? '');
    expect(activeId).toBe('card-w1');

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });
});
