import { test, expect, type Page } from '@playwright/test';
import { readFileSync } from 'node:fs';
import path from 'node:path';

// Hermetic DOM-level spec for the software-keyboard / pinch-zoom wiring in
// gateway/webui/static/js/app.js (`--app-vh` + the `keyboard-open` class).
//
// app.js derives both from `window.visualViewport`, which reports its height in
// CSS pixels *at the current zoom*: a pinch zoom shrinks `vv.height` exactly the
// way a software keyboard does. Reading it unscaled therefore treats zoom as a
// keyboard — the shell collapses to the visible slice and the fixed drawers go
// blank, reflowing at every zoom step. That regression shipped in #2308 and was
// caught in review, not in CI, because the only spec covering it
// (`specs/shell/mobile-shell.spec.ts`) needs a live gateway.
//
// This spec pins the arithmetic hermetically: it stubs `visualViewport` before
// loading the shipped app.js verbatim, and asserts the layout-space height and
// the keyboard flag for four combinations of zoom and keyboard. No gateway, no
// memory API, no auth — runs in CI.

const APP_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/app.js',
);

const VIEWPORT = { width: 390, height: 844 };

// `visibleLayout` is the layout-pixel height the user can actually see, so the
// stub's `height` is that divided by the zoom scale — which is what the browser
// reports.
function pageHtml(vvHeight: number, scale: number): string {
  return `<!doctype html><html><head><script>
    Object.defineProperty(window, 'visualViewport', {
      configurable: true,
      value: {
        height: ${vvHeight},
        width: ${VIEWPORT.width},
        scale: ${scale},
        offsetTop: 0,
        offsetLeft: 0,
        pageTop: 0,
        pageLeft: 0,
        addEventListener: function () {},
        removeEventListener: function () {},
      },
    });
  </script></head><body><div class="app-shell">shell</div></body></html>`;
}

type Booted = { innerHeight: number; errors: string[] };

async function boot(
  page: Page,
  scale: number,
  visibleLayout: (inner: number) => number,
): Promise<Booted> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));
  await page.setViewportSize(VIEWPORT);
  const innerHeight = await page.evaluate(() => window.innerHeight);
  const vvHeight = visibleLayout(innerHeight) / scale;
  await page.setContent(pageHtml(vvHeight, scale));
  await page.addScriptTag({ path: APP_JS });
  return { innerHeight, errors };
}

function appVh(page: Page): Promise<number> {
  return page.evaluate(() =>
    parseFloat(
      document.documentElement.style.getPropertyValue('--app-vh') || 'NaN',
    ),
  );
}

function keyboardOpen(page: Page): Promise<boolean> {
  return page.evaluate(() =>
    document.documentElement.classList.contains('keyboard-open'),
  );
}

const noKeyboard = (inner: number) => inner;
const keyboard = (inner: number) => inner - 300;

test('no keyboard, no zoom: the shell is full height', async ({ page }) => {
  const { innerHeight, errors } = await boot(page, 1, noKeyboard);
  expect(await appVh(page)).toBeCloseTo(innerHeight, 0);
  expect(await keyboardOpen(page)).toBe(false);
  expect(errors).toEqual([]);
});

test('an open keyboard publishes the reduced height and flags the shell', async ({ page }) => {
  const { innerHeight, errors } = await boot(page, 1, keyboard);
  expect(await appVh(page)).toBeCloseTo(innerHeight - 300, 0);
  expect(await keyboardOpen(page)).toBe(true);
  expect(errors).toEqual([]);
});

test('pinch zoom is not mistaken for the keyboard (#2308)', async ({ page }) => {
  // scale 2 with nothing covered: visualViewport reports half the height, the
  // layout is untouched. Reading it unscaled collapsed the shell here.
  const { innerHeight, errors } = await boot(page, 2, noKeyboard);
  expect(await appVh(page)).toBeCloseTo(innerHeight, 0);
  expect(await keyboardOpen(page)).toBe(false);
  expect(errors).toEqual([]);
});

test('a keyboard is still detected while zoomed', async ({ page }) => {
  // scale 2 with 300 layout px covered: 544/2 = 272 reported.
  const { innerHeight, errors } = await boot(page, 2, keyboard);
  expect(await appVh(page)).toBeCloseTo(innerHeight - 300, 0);
  expect(await keyboardOpen(page)).toBe(true);
  expect(errors).toEqual([]);
});

// --- Which surfaces follow the keyboard --------------------------------------
//
// `dvh` does not shrink for the software keyboard either, so a surface pinned to
// the viewport only follows it through its own `html.keyboard-open` override to
// `var(--app-vh)`. #2335 was exactly that: the shell and the side panel were
// covered and the nav drawer and the object preview panel were not, so their
// last items — the drawer's menu tail, the preview's action row — sat behind the
// keyboard. This half of the spec loads the real stylesheet into Chromium and
// reads the computed height, so dropping a rule fails here instead of on a
// phone.
//
// It reads the Tailwind *input* file (`gateway/webui/css/app.css`), the one this
// repo edits: the compiled asset is generated by `task css` and absent on a cold
// tree, and the `js` gate does not build CSS. Tailwind's own at-rules are inert
// in a browser, so the plain rules asserted below resolve as shipped.

const APP_CSS = path.resolve(
  __dirname,
  '../../../../gateway/webui/css/app.css',
);

// The reduced layout height app.js publishes while the keyboard is open.
const KEYBOARD_PX = 300;

const SURFACES = [
  {
    label: 'the app shell',
    mount: '<div class="app-shell">shell</div>',
    query: '.app-shell',
    rule: '.app-shell',
  },
  {
    label: 'the side panel',
    mount: '<div id="sidepanel-panel">panel</div>',
    query: '#sidepanel-panel',
    rule: '#sidepanel-panel:not(.sidepanel-in-modal)',
  },
  {
    label: 'the object preview panel',
    mount: '<div id="object-preview-panel">preview</div>',
    query: '#object-preview-panel',
    rule: '#object-preview-panel:not(.object-preview-in-modal)',
  },
  {
    label: 'the nav drawer',
    mount: '<div id="_layout-sidebar">nav</div>',
    query: '#_layout-sidebar',
    rule: '#_layout-sidebar',
  },
  {
    label: 'the chat rail',
    mount: '<div id="chat-rail">rail</div>',
    query: '#chat-rail',
    rule: '#chat-rail:not(.hidden)',
  },
];

function computedHeight(page: Page, selector: string): Promise<number> {
  return page.evaluate((sel) => {
    const el = document.querySelector(sel);
    return el ? parseFloat(getComputedStyle(el).height) : NaN;
  }, selector);
}

for (const surface of SURFACES) {
  test(`${surface.label} follows the keyboard height (#2335)`, async ({ page }) => {
    await page.setViewportSize(VIEWPORT);
    await page.setContent(
      `<!doctype html><html><head></head><body>${surface.mount}</body></html>`,
    );
    await page.addStyleTag({ path: APP_CSS });

    // Without a keyboard the surface fills the viewport…
    expect(await computedHeight(page, surface.query)).toBeCloseTo(
      VIEWPORT.height,
      0,
    );

    // …and with one it takes the height app.js derived from visualViewport.
    await page.evaluate((px) => {
      const root = document.documentElement;
      root.style.setProperty('--app-vh', `${px}px`);
      root.classList.add('keyboard-open');
    }, KEYBOARD_PX);
    expect(await computedHeight(page, surface.query)).toBeCloseTo(
      KEYBOARD_PX,
      0,
    );
  });
}

// The loop above only knows the surfaces it is told about, so a new
// `html.keyboard-open` rule with no row here would be silently unchecked. This
// keeps the two in step: every rule in the stylesheet must have a row, and every
// row must have a rule.
test('every keyboard-open rule has a row in SURFACES', async () => {
  const css = readFileSync(APP_CSS, 'utf8').replace(/\/\*[\s\S]*?\*\//g, '');
  const declared = [...css.matchAll(/html\.keyboard-open\s+([^{,]+?)\s*(?:,|\{)/g)]
    .map((m) => m[1].trim())
    .sort();
  expect(declared).toEqual(SURFACES.map((s) => s.rule).sort());
});
