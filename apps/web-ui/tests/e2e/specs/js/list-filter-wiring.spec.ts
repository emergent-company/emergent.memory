import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Hermetic DOM-level wiring spec for the list-filter glue in app.js.
//
// Two pieces of behaviour live only in the shipped script, so nothing in a Go
// render test can see them:
//
//   1. The objects branch/type <select>s are transparent over a sibling span
//      (a native select hard-clips the chosen value). A filter refresh replaces
//      only the list, so that visible copy would keep the previous value until a
//      full reload — app.js mirrors the chosen option's text into it.
//   2. The results region is swapped away beneath a live Alpine selection model.
//      app.js drops the selection when the swap came from an element that
//      refreshes the list, and keeps it when it came from "Load more", which
//      appends rows to the list the user is still selecting from.
//
// Loads the exact shipped app.js into a bare page — no gateway, no API, no auth.

const APP_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/app.js',
);

// The shipped htmx, for the one case that cannot be synthesised: where htmx
// dispatches `htmx:after:swap` depends on which elements survived the swap.
const HTMX_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/htmx.min.js',
);

// Mirrors objectsFilterSelect (span + transparent select) and the objects
// filter form's refresh marker.
const FILTER_HTML = `<!doctype html><html><body>
  <div id="objects-stats"></div>
  <form id="objects-filters-form" data-objects-list-refresh>
    <div class="select relative flex items-center">
      <span data-mirror-label>Main branch</span>
      <select id="branch" name="branch" data-mirror-select data-stats-refresh>
        <option value="" selected>Main branch</option>
        <option value="b1">plan/next-gen</option>
      </select>
    </div>
    <div class="select relative flex items-center">
      <span data-mirror-label>All types</span>
      <select id="type" name="type" data-mirror-select>
        <option value="" selected>All types</option>
        <option value="task">task</option>
      </select>
    </div>
    <select id="plain" name="plain">
      <option value="a" selected>a</option>
      <option value="b">b</option>
    </select>
  </form>
  <div id="objects-results" data-objects-list-refresh="true">
    <div id="objects-list"></div>
    <div id="objects-load-more">
      <button id="load-more" type="button">Load more</button>
      <a id="sort" href="?order=asc" data-objects-list-refresh="true">Sort</a>
    </div>
  </div>
  <div id="selection-root" data-object-select-root></div>
</body></html>`;

async function bootstrap(page: Page, html: string = FILTER_HTML): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));
  await page.setContent(html);
  await page.addScriptTag({ path: APP_JS });
  return errors;
}

test('a filter select mirrors its chosen label and refreshes the stats strip', async ({
  page,
}) => {
  const errors = await bootstrap(page);

  await page.evaluate(() => {
    const stats = document.getElementById('objects-stats')!;
    (window as unknown as { __statsRefreshes: number }).__statsRefreshes = 0;
    stats.addEventListener('objects-stats-refresh', () => {
      (window as unknown as { __statsRefreshes: number }).__statsRefreshes += 1;
    });
  });

  await page.locator('#branch').selectOption('b1');

  await expect(page.locator('span[data-mirror-label]').first()).toHaveText(
    'plan/next-gen',
  );

  // The branch is the strip's scope, so it asked for a refetch — once.
  expect(
    await page.evaluate(
      () => (window as unknown as { __statsRefreshes: number }).__statsRefreshes,
    ),
  ).toBe(1);

  // A select that is not a mirrored control (and not the branch) does nothing.
  await page.locator('#type').selectOption('task');
  await expect(page.locator('span[data-mirror-label]').nth(1)).toHaveText('task');
  expect(
    await page.evaluate(
      () => (window as unknown as { __statsRefreshes: number }).__statsRefreshes,
    ),
  ).toBe(1);

  expect(errors).toEqual([]);
});

test('a list refresh clears the row selection, "Load more" keeps it', async ({
  page,
}) => {
  const errors = await bootstrap(page);

  await page.evaluate(() => {
    const root = document.getElementById('selection-root')!;
    const model = { sel: ['a', 'b'] };
    (window as unknown as { __model: { sel: string[] } }).__model = model;
    (window as unknown as { Alpine: unknown }).Alpine = {
      $data: (el: Element) => (el === root ? model : undefined),
    };
  });

  // A filter/sort refresh replaces the rows: the selection must go with them,
  // otherwise the bulk bar counts and mutates rows that are no longer on screen.
  await page.evaluate(() => {
    document
      .getElementById('objects-filters-form')!
      .dispatchEvent(new CustomEvent('htmx:after:swap', { bubbles: true }));
  });
  expect(
    await page.evaluate(() => (window as unknown as { __model: { sel: string[] } }).__model.sel),
  ).toEqual([]);

  // A sort link lives INSIDE the region it replaces, so htmx dispatches the
  // event on the swapped-in target instead of the (now detached) link. That
  // path must clear too — it is the difference between clearing on sort and
  // silently keeping a selection over reordered rows.
  await page.evaluate(() => {
    (window as unknown as { __model: { sel: string[] } }).__model.sel = ['a', 'b'];
    document
      .getElementById('objects-results')!
      .dispatchEvent(new CustomEvent('htmx:after:swap', { bubbles: true }));
  });
  expect(
    await page.evaluate(() => (window as unknown as { __model: { sel: string[] } }).__model.sel),
  ).toEqual([]);

  // "Load more" appends rows to the list the user is still selecting from, so
  // the selection survives. Its button sits inside the region, which is why the
  // check is an exact match on the event target rather than a closest() walk.
  await page.evaluate(() => {
    (window as unknown as { __model: { sel: string[] } }).__model.sel = ['a', 'b'];
    document
      .getElementById('load-more')!
      .dispatchEvent(new CustomEvent('htmx:after:swap', { bubbles: true }));
  });
  expect(
    await page.evaluate(() => (window as unknown as { __model: { sel: string[] } }).__model.sel),
  ).toEqual(['a', 'b']);

  expect(errors).toEqual([]);
});

// The synthesised case above proves the handler's rule; this one proves the
// rule is reachable through the real htmx. A sort link lives inside the region
// it replaces, so after the swap htmx finds the source detached and dispatches
// `htmx:after:swap` on the swapped-in target instead — the selection must still
// clear. (htmx only tells the difference by DOM liveness, so nothing but the
// real client can pin it.)
test('htmx delivers a sort swap on the new region, which still clears', async ({
  page,
}) => {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));

  const pageBody = `<!doctype html><html><body>
    <div id="selection-root" data-object-select-root></div>
    <div id="objects-results" data-objects-list-refresh="true">
      <div id="objects-list"></div>
      <a id="sort" href="?order=asc" data-objects-list-refresh="true"
         hx-get="/sorted" hx-target="#objects-results" hx-swap="outerHTML" hx-push-url="false">Sort</a>
      <button id="more" type="button" hx-get="/more" hx-target="#objects-list" hx-swap="beforeend">Load more</button>
    </div>
  </body></html>`;
  const swapped = `<div id="objects-results" data-objects-list-refresh="true">
      <div id="objects-list"></div>
      <a id="sort" href="?order=desc" data-objects-list-refresh="true"
         hx-get="/sorted" hx-target="#objects-results" hx-swap="outerHTML" hx-push-url="false">Sort</a>
      <button id="more" type="button" hx-get="/more" hx-target="#objects-list" hx-swap="beforeend">Load more</button>
    </div>`;

  await page.route('**/*', async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === '/sorted') {
      return route.fulfill({ status: 200, contentType: 'text/html', body: swapped });
    }
    if (url.pathname === '/more') {
      return route.fulfill({
        status: 200,
        contentType: 'text/html',
        body: '<div class="appended">row</div>',
      });
    }
    return route.fulfill({ status: 200, contentType: 'text/html', body: pageBody });
  });

  await page.goto('http://list-filter.test/objects');
  await page.addScriptTag({ path: HTMX_JS });
  await page.addScriptTag({ path: APP_JS });
  await page.evaluate(() => {
    const root = document.getElementById('selection-root')!;
    const model = { sel: ['a', 'b'] };
    (window as unknown as { __model: { sel: string[] } }).__model = model;
    (window as unknown as { Alpine: unknown }).Alpine = {
      $data: (el: Element) => (el === root ? model : undefined),
    };
  });

  await page.locator('#sort').click();
  await expect(page.locator('#objects-results')).toHaveAttribute(
    'data-objects-list-refresh',
    'true',
  );
  await expect
    .poll(() =>
      page.evaluate(
        () => (window as unknown as { __model: { sel: string[] } }).__model.sel,
      ),
    )
    .toEqual([]);

  // "Load more" keeps appending into #objects-list, so the selection survives.
  await page.evaluate(() => {
    (window as unknown as { __model: { sel: string[] } }).__model.sel = ['c'];
  });
  await page.locator('#more').click();
  await expect(page.locator('#objects-list .appended')).toHaveCount(1);
  expect(
    await page.evaluate(
      () => (window as unknown as { __model: { sel: string[] } }).__model.sel,
    ),
  ).toEqual(['c']);

  expect(errors).toEqual([]);
});
