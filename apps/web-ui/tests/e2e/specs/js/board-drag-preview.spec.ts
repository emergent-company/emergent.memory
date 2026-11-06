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
    <section class="memory-board__lane" data-board-column="ready"><div class="lane-body"></div></section>
    <section class="memory-board__lane" data-board-column="in_progress"><div class="lane-body"></div></section>
    <section class="memory-board__lane" data-board-column="review"><div class="lane-body"></div></section>
    <section class="memory-board__lane" data-board-column="revision"><div class="lane-body"></div></section>
    <section class="memory-board__lane" data-board-column="blocked"><div class="lane-body"></div></section>
    <section class="memory-board__lane" data-board-column="done"><div class="lane-body"></div></section>
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

// A custom-mapped board (#1428): the project's agent workConfig maps the
// lifecycle phases to todo/doing/checking/rework/rejected/shipped, emitted as
// #board[data-board-status-map]. The literal statuses must no longer drive
// transitions.
const CUSTOM_MAP = {
  ready: 'todo',
  inProgress: 'doing',
  review: 'checking',
  revision: 'rework',
  blocked: 'rejected',
  done: 'shipped',
};

const CUSTOM_BOARD_HTML = `<!doctype html><html><body>
  <div id="board" data-board-status-map='${JSON.stringify(CUSTOM_MAP)}'>
    <section class="memory-board__lane" data-board-column="todo"><div class="lane-body"></div></section>
    <section class="memory-board__lane" data-board-column="doing"><div class="lane-body"></div></section>
    <section class="memory-board__lane" data-board-column="checking"><div class="lane-body"></div></section>
    <section class="memory-board__lane" data-board-column="rework"><div class="lane-body"></div></section>
    <section class="memory-board__lane" data-board-column="rejected"><div class="lane-body"></div></section>
    <section class="memory-board__lane" data-board-column="shipped"><div class="lane-body"></div></section>
  </div>
  <div id="board-drawer"></div>
  <div id="card-todo" draggable="true" data-board-card data-board-status="todo"
       data-board-open-preview aria-haspopup="dialog" role="button" tabindex="0"
       data-canonical-id="w1">todo card</div>
  <div id="card-rejected" draggable="true" data-board-card data-board-status="rejected"
       data-board-open-preview aria-haspopup="dialog" role="button" tabindex="0"
       data-canonical-id="w2">rejected card</div>
  <div id="card-checking" draggable="true" data-board-card data-board-status="checking"
       data-board-open-preview aria-haspopup="dialog" role="button" tabindex="0"
       data-canonical-id="w3">checking card</div>
  <div id="card-literal-blocked" draggable="true" data-board-card data-board-status="blocked"
       data-board-open-preview aria-haspopup="dialog" role="button" tabindex="0"
       data-canonical-id="w4">literal blocked card</div>
</body></html>`;

// Minimal fixture for the touch-only "Move" affordance: a card carrying the
// nested button that opens the item drawer (status control) instead of HTML5
// drag, which touch devices never fire.
const MOVE_CARD_HTML = `<!doctype html><html><body>
  <div id="board"><section class="memory-board__lane" data-board-column="ready"></section></div>
  <div id="board-drawer"></div>
  <div id="card-ready" draggable="true" data-board-card data-board-status="ready"
       data-board-open-preview aria-haspopup="dialog" role="button" tabindex="0"
       data-canonical-id="w2">ready card
    <button type="button" id="move-w2" data-board-open-drawer="w2" draggable="false">Move</button>
  </div>
</body></html>`;

interface AjaxCall {
  method: string;
  url: string;
  opts: unknown;
}

async function bootstrap(page: Page, html: string = BOARD_HTML): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));

  await page.setContent(html);
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

// dragStart opens a drag without dropping, so the in-flight affordance state
// (data-drop-eligible / data-drop-action) can be inspected.
async function dragStart(page: Page, cardId: string): Promise<void> {
  await page.evaluate((cardId) => {
    const card = document.getElementById(cardId);
    if (!card) throw new Error(`missing card ${cardId}`);
    card.dispatchEvent(
      new DragEvent('dragstart', { bubbles: true, cancelable: true, dataTransfer: new DataTransfer() }),
    );
  }, cardId);
}

async function dragEnd(page: Page, cardId: string): Promise<void> {
  await page.evaluate((cardId) => {
    const card = document.getElementById(cardId);
    if (!card) throw new Error(`missing card ${cardId}`);
    card.dispatchEvent(
      new DragEvent('dragend', { bubbles: true, cancelable: true, dataTransfer: new DataTransfer() }),
    );
  }, cardId);
}

// eligible reads the marks app.js sets on lanes for the in-flight drag.
async function eligible(page: Page): Promise<Record<string, string>> {
  return (await page.evaluate(() => {
    const out: Record<string, string> = {};
    document.querySelectorAll('#board [data-board-column]').forEach((lane) => {
      if (!lane.hasAttribute('data-drop-eligible')) return;
      out[lane.getAttribute('data-board-column') || ''] = lane.getAttribute('data-drop-action') || '';
    });
    return out;
  })) as Record<string, string>;
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

  test('a ready card dropped on the in-progress lane fires an exact status change', async ({ page }) => {
    const errors = await bootstrap(page);

    await drag(page, 'card-ready', 'in_progress');

    const fired = await calls(page);
    expect(fired).toHaveLength(1);
    expect(fired[0].method).toBe('POST');
    expect(fired[0].url).toBe('/board/items/w2/status');
    expect(fired[0].opts).toMatchObject({
      target: '#board',
      swap: 'outerHTML',
      values: { status: 'in_progress' },
    });

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  // The affordance defect: nothing told the user which lanes would accept the
  // card. Every lane the drop predicate accepts is marked during the drag, from
  // the same predicate — so the marked set and the accepted set cannot drift.
  test('dragstart marks every eligible lane and no others', async ({ page }) => {
    const errors = await bootstrap(page);

    await dragStart(page, 'card-ready');

    expect(await eligible(page)).toEqual({
      in_progress: 'status',
      review: 'status',
      revision: 'status',
      blocked: 'cancel',
      done: 'status',
    });

    await dragEnd(page, 'card-ready');
    expect(await eligible(page)).toEqual({});

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('dropping a card back on its own lane fires nothing', async ({ page }) => {
    const errors = await bootstrap(page);

    await drag(page, 'card-ready', 'ready');

    expect(await calls(page)).toHaveLength(0);
    expect(await eligible(page)).toEqual({});

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  // Morphing #board mid-drag would move the card out from under the pointer, so
  // live frames are cancelled while a drag is in flight and dragend replays one
  // refresh so the board cannot end up stale after a cancelled drag.
  test('live frames are deferred during a drag and replayed on dragend', async ({ page }) => {
    const errors = await bootstrap(page);

    await dragStart(page, 'card-ready');
    const cancelled = await page.evaluate(() => {
      const detail = { message: { data: '<hx-partial hx-target="#board"></hx-partial>' }, cancelled: false };
      document.dispatchEvent(new CustomEvent('htmx:sse:before:message', { detail }));
      return detail.cancelled;
    });
    expect(cancelled).toBe(true);

    await dragEnd(page, 'card-ready');

    const fired = await calls(page);
    expect(fired).toHaveLength(1);
    expect(fired[0].method).toBe('GET');
    expect(fired[0].url).toBe('/board/partial');
    expect(fired[0].opts).toMatchObject({ target: '#board', swap: 'outerHTML' });

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  // Only the columns frame can move the card under the pointer. Deferring the
  // health frame too would leave that readout stale until some later event
  // happened to arrive.
  test('health frames still apply during a drag', async ({ page }) => {
    const errors = await bootstrap(page);

    await dragStart(page, 'card-ready');
    const cancelled = await page.evaluate(() => {
      const detail = {
        message: { data: '<hx-partial hx-target="#board-health" hx-swap="outerMorph"></hx-partial>' },
        cancelled: false,
      };
      document.dispatchEvent(new CustomEvent('htmx:sse:before:message', { detail }));
      return detail.cancelled;
    });
    expect(cancelled).toBe(false);

    await dragEnd(page, 'card-ready');
    expect(await calls(page)).toHaveLength(0);

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('a live frame applies normally when no drag is in flight', async ({ page }) => {
    const errors = await bootstrap(page);

    const cancelled = await page.evaluate(() => {
      const detail = { message: { data: '<hx-partial hx-target="#board"></hx-partial>' }, cancelled: false };
      document.dispatchEvent(new CustomEvent('htmx:sse:before:message', { detail }));
      return detail.cancelled;
    });
    expect(cancelled).toBe(false);
    expect(await calls(page)).toHaveLength(0);

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

  test('the mobile Move button opens the item drawer, not the preview', async ({ page }) => {
    const errors = await bootstrap(page, MOVE_CARD_HTML);

    await page.evaluate(() => {
      const w = window as unknown as { __previewOpened: boolean };
      w.__previewOpened = false;
      (window as unknown as { MemoryObjectPreview: unknown }).MemoryObjectPreview = {
        open: () => {
          w.__previewOpened = true;
        },
      };
    });

    await page.locator('#move-w2').click();

    const fired = await calls(page);
    expect(fired).toHaveLength(1);
    expect(fired[0].method).toBe('GET');
    expect(fired[0].url).toBe('/board/items/w2');
    expect(fired[0].opts).toEqual({ target: '#board-drawer', swap: 'innerHTML' });
    expect(
      await page.evaluate(() => (window as unknown as { __previewOpened: boolean }).__previewOpened),
    ).toBe(false);

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

// #1428: drag transitions derive from the project's work-path status map, so a
// custom-mapped board exposes the same actions as the default one.
test.describe('Board drag wiring — custom-mapped statuses (app.js)', () => {
  test('a custom-blocked card dropped on the custom-ready lane fires retry', async ({ page }) => {
    const errors = await bootstrap(page, CUSTOM_BOARD_HTML);

    await drag(page, 'card-rejected', 'todo');

    const fired = await calls(page);
    expect(fired).toHaveLength(1);
    expect(fired[0].method).toBe('POST');
    expect(fired[0].url).toBe('/board/items/w2/retry');

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('a custom-review card dropped on the custom-done lane fires approve', async ({ page }) => {
    const errors = await bootstrap(page, CUSTOM_BOARD_HTML);

    await drag(page, 'card-checking', 'shipped');

    const fired = await calls(page);
    expect(fired).toHaveLength(1);
    expect(fired[0].method).toBe('POST');
    expect(fired[0].url).toBe('/board/items/w3/approve');

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('a custom-review card dropped on the custom-revision lane opens the feedback dialog', async ({ page }) => {
    const errors = await bootstrap(page, CUSTOM_BOARD_HTML);

    await drag(page, 'card-checking', 'rework');

    const fired = await calls(page);
    expect(fired).toHaveLength(1);
    expect(fired[0].method).toBe('GET');
    expect(fired[0].url).toBe('/board/items/w3');
    expect(fired[0].opts).toMatchObject({ target: '#board-drawer' });

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('a custom-ready card dropped on the custom-blocked lane fires cancel', async ({ page }) => {
    const errors = await bootstrap(page, CUSTOM_BOARD_HTML);

    await drag(page, 'card-todo', 'rejected');

    const fired = await calls(page);
    expect(fired).toHaveLength(1);
    expect(fired[0].method).toBe('POST');
    expect(fired[0].url).toBe('/board/items/w1/cancel');

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('the mapped status names still drive the canonical actions', async ({ page }) => {
    const errors = await bootstrap(page, CUSTOM_BOARD_HTML);

    await drag(page, 'card-rejected', 'todo');

    const fired = await calls(page);
    expect(fired).toHaveLength(1);
    expect(fired[0].url).toBe('/board/items/w2/retry');

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  // A custom lane pair has no canonical action, but it is still a real move: it
  // posts the exact status and lets the item's type validate it. Before, the
  // client rejected every pair outside the canonical map with no request and no
  // feedback — which made custom-status boards undraggable.
  test('an unmapped custom pair posts the exact status', async ({ page }) => {
    const errors = await bootstrap(page, CUSTOM_BOARD_HTML);

    await drag(page, 'card-todo', 'doing');

    const fired = await calls(page);
    expect(fired).toHaveLength(1);
    expect(fired[0].url).toBe('/board/items/w1/status');
    expect(fired[0].opts).toMatchObject({ target: '#board', swap: 'outerHTML', values: { status: 'doing' } });

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('a custom board marks eligible lanes from its own mapping', async ({ page }) => {
    const errors = await bootstrap(page, CUSTOM_BOARD_HTML);

    await dragStart(page, 'card-checking');

    expect(await eligible(page)).toEqual({
      todo: 'status',
      doing: 'status',
      rework: 'dialog',
      rejected: 'cancel',
      shipped: 'approve',
    });

    await dragEnd(page, 'card-checking');
    expect(await eligible(page)).toEqual({});

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });
});

// A board whose lanes are the union of several object types' statuses: the
// `suggested` lane exists for a different type, and the dragged card carries its
// own type's accepted set in data-board-allowed (#2470). Offering that lane would
// be a target the server always refuses.
const TYPE_SCOPED_BOARD_HTML = `<!doctype html><html><body>
  <div id="board">
    <section class="memory-board__lane" data-board-column="ready"><div class="lane-body"></div></section>
    <section class="memory-board__lane" data-board-column="in_progress"><div class="lane-body"></div></section>
    <section class="memory-board__lane" data-board-column="review"><div class="lane-body"></div></section>
    <section class="memory-board__lane" data-board-column="revision"><div class="lane-body"></div></section>
    <section class="memory-board__lane" data-board-column="suggested"><div class="lane-body"></div></section>
    <section class="memory-board__lane" data-board-column="done"><div class="lane-body"></div></section>
  </div>
  <div id="board-drawer"></div>
  <div id="card-scoped" draggable="true" data-board-card data-board-status="ready"
       data-board-allowed='["ready","review"]'
       data-board-open-preview aria-haspopup="dialog" role="button" tabindex="0"
       data-canonical-id="w1">scoped card</div>
  <div id="card-review-scoped" draggable="true" data-board-card data-board-status="review"
       data-board-allowed='["ready","review"]'
       data-board-open-preview aria-haspopup="dialog" role="button" tabindex="0"
       data-canonical-id="w3">review scoped card</div>
  <div id="card-unscoped" draggable="true" data-board-card data-board-status="ready"
       data-board-open-preview aria-haspopup="dialog" role="button" tabindex="0"
       data-canonical-id="w2">unscoped card</div>
</body></html>`;

test.describe('Board drag and the item type\'s allowed statuses (app.js)', () => {
  test('a lane outside the type\'s allowed set is not offered', async ({ page }) => {
    const errors = await bootstrap(page, TYPE_SCOPED_BOARD_HTML);

    await dragStart(page, 'card-scoped');

    // ready is the card's own lane (never a target) and in_progress/suggested are
    // not in its type's set, so review is the only lane the write can accept.
    expect(await eligible(page)).toEqual({ review: 'status' });

    await dragEnd(page, 'card-scoped');
    expect(await eligible(page)).toEqual({});

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('dropping on a lane the type refuses issues no request', async ({ page }) => {
    const errors = await bootstrap(page, TYPE_SCOPED_BOARD_HTML);

    await drag(page, 'card-scoped', 'suggested');

    expect(await calls(page)).toHaveLength(0);
    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('an allowed lane still moves the card', async ({ page }) => {
    const errors = await bootstrap(page, TYPE_SCOPED_BOARD_HTML);

    await drag(page, 'card-scoped', 'review');

    const fired = await calls(page);
    expect(fired).toHaveLength(1);
    expect(fired[0].url).toBe('/board/items/w1/status');
    expect(fired[0].opts).toMatchObject({ values: { status: 'review' } });

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  // retry/approve/cancel/request-changes are dedicated endpoints that move the
  // item to their own configured status; they are not subject to the type's
  // allowed-status set, so narrowing must not hide them.
  test('the platform transitions are still offered outside the allowed set', async ({ page }) => {
    const errors = await bootstrap(page, TYPE_SCOPED_BOARD_HTML);

    await dragStart(page, 'card-review-scoped');

    expect(await eligible(page)).toEqual({ ready: 'status', done: 'approve', revision: 'dialog' });

    await dragEnd(page, 'card-review-scoped');
    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('a card whose type declares no set keeps every lane', async ({ page }) => {
    const errors = await bootstrap(page, TYPE_SCOPED_BOARD_HTML);

    await dragStart(page, 'card-unscoped');

    expect(await eligible(page)).toEqual({
      in_progress: 'status',
      review: 'status',
      revision: 'status',
      suggested: 'status',
      done: 'status',
    });

    await dragEnd(page, 'card-unscoped');
    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });
});
