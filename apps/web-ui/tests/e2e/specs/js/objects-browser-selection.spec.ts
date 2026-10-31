import { test, expect } from '@playwright/test';
import fs from 'node:fs';
import path from 'node:path';

// Hermetic wiring spec for the objects-browser selection model (#2237).
//
// The desktop table and the mobile card list are both in the DOM (CSS hides
// one), so every object has TWO `input[data-object-select]` boxes. The model
// must dedupe by value or the desktop select-all never reaches "all" and
// toggleAll doubles the count. Loads the shipped model literal straight from
// objects_browser.go so the test cannot drift from the implementation.
const GO_SOURCE = path.resolve(
  __dirname,
  '../../../../gateway/objects_browser.go',
);

function selectionModelSource(): string {
  const src = fs.readFileSync(GO_SOURCE, 'utf8');
  const m = src.match(/const objectsBrowserSelectionJS = `([\s\S]*?)`/);
  if (!m) throw new Error('objectsBrowserSelectionJS literal not found');
  return m[1];
}

const PAGE = `<!doctype html><html><body>
  <div id="root">
    <div id="objects-list">
      <input type="checkbox" data-object-select value="a">
      <input type="checkbox" data-object-select value="b">
    </div>
    <div id="objects-mobile-list">
      <input type="checkbox" data-object-select value="a">
      <input type="checkbox" data-object-select value="b">
    </div>
  </div>
</body></html>`;

test('objects selection dedupes the desktop + mobile checkbox copies (#2237)', async ({
  page,
}) => {
  const source = selectionModelSource();
  await page.setContent(PAGE);

  const out = await page.evaluate((modelSource) => {
    // eslint-disable-next-line no-eval
    const model = eval('(' + modelSource + ')') as {
      $root: Element;
      sel: string[];
      count: number;
      all: boolean;
      some: boolean;
      toggleAll: (checked: boolean) => void;
    };
    model.$root = document.getElementById('root')!;
    model.sel = [];

    const initial = { count: model.count, all: model.all, some: model.some };

    model.toggleAll(true);
    const allOn = {
      sel: model.sel.slice(),
      count: model.count,
      all: model.all,
      some: model.some,
    };

    model.toggleAll(false);
    const cleared = { sel: model.sel.slice(), count: model.count };

    model.sel = ['a'];
    const one = { count: model.count, all: model.all, some: model.some };

    // Defensive: even if a stale array contains duplicates, count is unique.
    model.sel = ['a', 'a', 'b'];
    const dupes = { count: model.count, all: model.all };

    return { initial, allOn, cleared, one, dupes };
  }, source);

  expect(out.initial).toEqual({ count: 0, all: false, some: false });
  // Two boxes per id (four total) must still select exactly the two ids.
  expect(out.allOn).toEqual({
    sel: ['a', 'b'],
    count: 2,
    all: true,
    some: false,
  });
  expect(out.cleared).toEqual({ sel: [], count: 0 });
  expect(out.one).toEqual({ count: 1, all: false, some: true });
  expect(out.dupes).toEqual({ count: 2, all: true });
});
