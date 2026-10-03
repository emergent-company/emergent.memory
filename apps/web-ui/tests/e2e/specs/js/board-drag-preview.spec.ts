import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Hermetic DOM-level wiring spec for the shipped board client behaviour
// (gateway/webui/static/js/app.js). It loads the exact file the browser receives
// into a bare page and drives the document-delegated drag/drop and card-click
// handlers against board markup, with a stubbed htmx. It guards the #1379
// defect: cards were only draggable when `blocked`, the drop target hardcoded
// the `ready` lane, and the click opened the board dialog instead of the shared
// object preview. No gateway, no memory API, no Zitadel — runs in CI.

const APP_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/app.js',
);

// Markup mirroring board.templ: one lane per status, each card carrying the
// data-* hooks app.js delegates on (data-board-card / status / open-preview).
const BOARD_HTML = `<!doctype html><html><body>
  <div id="board">
    <section data-board-column="ready"><div class="lane-body"></div></section>
    <section data-board-column="in_progress"><div class="lane-body"></div></section>
    <section data-board-column="review"><div class="lane-body"></div></section>
    <section data-board-column="revision"><div class="lane-body"></div></section>
    <section data-board-column="blocked"><div class="lane-body"></div></section>
    <section data-board-column="done"><div class="lane-body"></div></section>
  </div>
  <div id="board-drawer"></div>
  <div id="card-blocked" draggable="true" data-board-card data-board-status="blocked"
       data-board-open-preview aria-haspopup="dialog" role="button" tabindex="0"
       data-canonical-id="w1">blocked card</div>
  <div id="card-ready" draggable="true" data-board-card data-board-status="ready"
       data-board-open-preview aria-haspopup="dialog" role="button" tabindex="0"
       data-canonical-id="w2">ready card</div>
  <div id="card-review" draggable="true" data-board-card data-board-status="review"
       data-board-open-preview aria-haspopup="dialog" role="button" tabindex="0"
       data-canonical-id="w3">review card</div>
</body></html>`;

interface AjaxCall {
  method: string;
  url: string;
  opts: unknown;
}

async function bootstrap(page: Page): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));

  await page.setContent(BOARD_HTML);
  await page.addScriptTag({ path: APP_JS });

  await page.evaluate(() => {
    const w = window as unknown as { __calls: AjaxCall[]; htmx: unknown };
    w.__calls = [];
    w.htmx = {
      ajax: (method: string, url: string, opts: unknown) => {
        w.__calls.push({ method, url, opts });
        return Promise.resolve();
      },
    };
  });
  return errors;
}

async function calls(page: Page): Promise<AjaxCall[]> {
  return (await page.evaluate(
    () => (window as unknown as { __calls: AjaxCall[] }).__calls,
  )) as AjaxCall[];
}

// Dispatch a drag gesture with one shared DataTransfer across the three events,
// matching how Chromium drives a real drag.
async function drag(page: Page, cardId: string, laneStatus: string): Promise<void> {
  await page.evaluate(
    ({ cardId, laneStatus }) => {
      const card = document.getElementById(cardId);
      const lane = document.querySelector(`[data-board-column="${laneStatus}"]`);
      if (!card || !lane) throw new Error(`missing drag subject or lane: ${cardId} -> ${laneStatus}`);
      const dt = new DataTransfer();
      const fire = (el: Element, type: string) =>
        el.dispatchEvent(new DragEvent(type, { bubbles: true, cancelable: true, dataTransfer: dt }));
      fire(card, 'dragstart');
      fire(lane, 'dragover');
      fire(lane, 'drop');
      fire(card, 'dragend');
    },
    { cardId, laneStatus },
  );
}

test.describe('Board drag + preview wiring (app.js)', () => {
  test('a blocked card dropped on the ready lane fires the retry action', async ({ page }) => {
    const errors = await bootstrap(page);

    await drag(page, 'card-blocked', 'ready');

    const fired = await calls(page);
    expect(fired).toHaveLength(1);
    expect(fired[0].method).toBe('POST');
    expect(fired[0].url).toBe('/board/items/w1/retry');

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('an invalid pair fires no request and adds no drop target', async ({ page }) => {
    const errors = await bootstrap(page);

    await drag(page, 'card-ready', 'in_progress');

    expect(await calls(page)).toHaveLength(0);
    const drops = await page.locator('.memory-board__lane--drop').count();
    expect(drops).toBe(0);

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  // The #1379 defect cancelled dragstart for every non-blocked card, so these
  // supported moves never fired. They fail against that guard, which is what
  // makes the regression bite.
  test('a ready card dropped on the blocked lane fires the cancel action', async ({ page }) => {
    const errors = await bootstrap(page);

    await drag(page, 'card-ready', 'blocked');

    const fired = await calls(page);
    expect(fired).toHaveLength(1);
    expect(fired[0].method).toBe('POST');
    expect(fired[0].url).toBe('/board/items/w2/cancel');

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('a review card dropped on the done lane fires the approve action', async ({ page }) => {
    const errors = await bootstrap(page);

    await drag(page, 'card-review', 'done');

    const fired = await calls(page);
    expect(fired).toHaveLength(1);
    expect(fired[0].method).toBe('POST');
    expect(fired[0].url).toBe('/board/items/w3/approve');

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('a review card dropped on the revision lane opens the feedback dialog', async ({ page }) => {
    const errors = await bootstrap(page);

    await drag(page, 'card-review', 'revision');

    const fired = await calls(page);
    expect(fired).toHaveLength(1);
    expect(fired[0].method).toBe('GET');
    expect(fired[0].url).toBe('/board/items/w3');
    expect(fired[0].opts).toMatchObject({ target: '#board-drawer' });

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('clicking a card opens the shared object preview with board actions', async ({ page }) => {
    const errors = await bootstrap(page);

    await page.evaluate(() => {
      const w = window as unknown as { __previewRef: unknown };
      w.__previewRef = null;
      (window as unknown as { MemoryObjectPreview: unknown }).MemoryObjectPreview = {
        open: (ref: unknown) => {
          w.__previewRef = ref;
        },
      };
    });

    await page.locator('#card-ready').click();

    const ref = await page.evaluate(
      () => (window as unknown as { __previewRef: unknown }).__previewRef,
    );
    expect(ref).toEqual({ id: 'w2', editHref: '/objects/w2', actions: 'board' });

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('Enter and Space activate a focused card and open the preview', async ({ page }) => {
    const errors = await bootstrap(page);

    await page.evaluate(() => {
      const w = window as unknown as { __previewRef: unknown };
      w.__previewRef = null;
      (window as unknown as { MemoryObjectPreview: unknown }).MemoryObjectPreview = {
        open: (ref: unknown) => {
          w.__previewRef = ref;
        },
      };
    });

    const card = page.locator('#card-ready');
    const ref = () =>
      page.evaluate(() => (window as unknown as { __previewRef: unknown }).__previewRef);

    await card.focus();
    await card.press('Enter');
    expect(await ref()).toEqual({ id: 'w2', editHref: '/objects/w2', actions: 'board' });

    await page.evaluate(() => {
      (window as unknown as { __previewRef: unknown }).__previewRef = null;
    });
    await card.press('Space');
    expect(await ref()).toEqual({ id: 'w2', editHref: '/objects/w2', actions: 'board' });

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });
});
