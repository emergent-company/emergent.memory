import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Hermetic DOM-level wiring spec for #1439: after a successful action from the
// board-item dialog (opened by dragging a card review -> revision), focus must
// land on a sensible in-page element (the refreshed card, or the board
// container) — never <body>.
//
// Root cause: the dialog lives in #board-drawer; its action buttons POST with
// hx-target="#board", and the BoardRefresh response OOB-clears #board-drawer.
// That removes the focused control before focus can be restored, so it falls to
// <body>. object-preview.js already defers the restore to htmx:after:swap for
// the preview drawer (#1427); #1439 extends the same pattern to requests that
// originate inside #board-drawer, re-finding the card by data-canonical-id and
// falling back to #board (which carries tabindex="-1").
//
// Loads object-preview.js verbatim into a bare page with a stubbed
// MemoryChatHost (resize grip) and htmx. No gateway, no memory API.

const OBJECT_PREVIEW_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/object-preview.js',
);

const PAGE_HTML = `<!doctype html><html><body>
  <div id="board" tabindex="-1" data-testid="board">
    <section data-board-column="review"><div class="lane-body">
      <div id="card-w1" draggable="true" data-board-card data-board-status="review"
           data-board-open-preview aria-haspopup="dialog" role="button" tabindex="0"
           data-canonical-id="w1">card</div>
    </div></section>
  </div>
  <div id="board-drawer" data-testid="board-drawer">
    <dialog id="board-item-dialog" data-dialog-autoopen data-testid="board-item-dialog"
            data-canonical-id="w1" open>
      <button id="dialog-request-changes" type="button"
              hx-post="/board/items/w1/request-changes">Request changes</button>
    </dialog>
  </div>
  <div id="object-preview-root">
    <div id="object-preview-backdrop" class="hidden"></div>
    <aside id="object-preview-panel" class="translate-x-full" role="dialog"
           aria-modal="true" aria-hidden="true" inert>
      <button id="object-preview-close" type="button">close</button>
      <div id="object-preview-body"></div>
      <div id="object-preview-actions-slot"></div>
    </aside>
  </div>
</body></html>`;

async function bootstrap(page: Page): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));

  await page.setContent(PAGE_HTML);
  await page.evaluate(() => {
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

// Dispatch the htmx event object-preview.js inspects: a successful action whose
// source element lives inside the board-item dialog in #board-drawer.
async function fireDialogAction(page: Page): Promise<void> {
  await page.evaluate(() => {
    document.dispatchEvent(
      new CustomEvent('htmx:after:request', {
        detail: {
          ctx: {
            sourceElement: document.getElementById('dialog-request-changes'),
            response: { raw: { status: 200 } },
          },
        },
      }),
    );
  });
}

// Replace #board with fresh markup, as the board action's outerHTML swap does,
// and OOB-clear #board-drawer (removing the dialog and its focused control) —
// then fire htmx:after:swap, exactly as htmx does at the end of the request.
async function swapBoardAndClearDrawer(page: Page, inner: string): Promise<void> {
  await page.evaluate((html) => {
    const board = document.getElementById('board');
    if (!board) throw new Error('missing #board');
    board.innerHTML = html;
    const drawer = document.getElementById('board-drawer');
    if (drawer) drawer.innerHTML = '';
    board.dispatchEvent(new CustomEvent('htmx:after:swap', { bubbles: true }));
  }, inner);
}

test.describe('Board-item dialog action focus (#1439)', () => {
  test('focus returns to the refreshed card after the board swap', async ({ page }) => {
    const errors = await bootstrap(page);

    await page.evaluate(() => document.getElementById('dialog-request-changes')?.focus());

    await fireDialogAction(page);

    // The board swap re-renders #board (card moves lanes); the drawer is cleared.
    await swapBoardAndClearDrawer(
      page,
      `<section data-board-column="revision"><div class="lane-body">
         <div id="card-w1" draggable="true" data-board-card data-board-status="revision"
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

    await page.evaluate(() => document.getElementById('dialog-request-changes')?.focus());
    await fireDialogAction(page);
    await swapBoardAndClearDrawer(
      page,
      '<section data-board-column="review"><div class="lane-body"></div></section>',
    );

    const active = await page.evaluate(() => ({
      id: document.activeElement?.id ?? '',
      tag: document.activeElement?.tagName ?? '',
    }));
    expect(active.id).toBe('board');
    expect(active.tag).not.toBe('BODY');

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('a failed dialog action leaves focus alone (no deferred restore)', async ({ page }) => {
    const errors = await bootstrap(page);

    await page.evaluate(() => document.getElementById('dialog-request-changes')?.focus());
    await page.evaluate(() => {
      document.dispatchEvent(
        new CustomEvent('htmx:after:request', {
          detail: {
            ctx: {
              sourceElement: document.getElementById('dialog-request-changes'),
              response: { raw: { status: 500 } },
            },
          },
        }),
      );
    });

    // No swap happened, so the focused control is still mounted and active.
    const activeId = await page.evaluate(() => document.activeElement?.id ?? '');
    expect(activeId).toBe('dialog-request-changes');

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });
});
