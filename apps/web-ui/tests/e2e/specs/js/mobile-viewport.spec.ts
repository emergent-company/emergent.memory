import { test, expect, type Page } from '@playwright/test';
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
