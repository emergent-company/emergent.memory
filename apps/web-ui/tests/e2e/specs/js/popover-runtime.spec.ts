import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Hermetic DOM-level harness for the production go-daisy popover runtime (#1440).
// Closes the coverage gap noted in #1318: no js-dom spec loaded the real
// runtime, so regressions in popover wiring (open/close, aria-expanded, focus
// stability, and re-open after an HTMX rail refresh) could ship without a CI
// gate. `js-dom.config.ts` runs this in CI with no gateway and no credentials.
//
// The fixture is a verbatim copy of go-daisy's
// staticfs/static/js/go-daisy-popover.js (the same file ui.PopoverScript()
// serves), loaded with page.addScriptTag. The runtime delegates its listeners
// on `document`, so it re-wires automatically after the rail's markup is
// replaced iff the data-gd-popover-* attributes are preserved.
//
// Contract under test: window.goDaisy.popover.{open,close,closeAll,toggle};
// data-gd-popover-open on the content; aria-expanded on the trigger; native
// showPopover/hidePopover; the runtime does NOT move focus, so the trigger
// stays the active element across open/close.

const POPOVER_JS = path.resolve(__dirname, '../../fixtures/go-daisy-popover.js');

const RAIL_HTML = `
  <div data-gd-popover-root>
    <button type="button" data-gd-popover-trigger data-testid="popover-trigger">
      Session actions
    </button>
    <div data-gd-popover-content popover="manual" data-gd-popover-placement="bottom"
         data-testid="popover-content">
      <button type="button" data-gd-close data-testid="popover-action">Archive session</button>
    </div>
  </div>`;

const PAGE_HTML = `<!doctype html><html><body>
  <div id="rail" data-testid="rail">${RAIL_HTML}</div>
</body></html>`;

async function bootstrap(page: Page): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));

  await page.setContent(PAGE_HTML);
  await page.addScriptTag({ path: POPOVER_JS });
  return errors;
}

async function contentState(page: Page) {
  return page.evaluate(() => {
    const content = document.querySelector('[data-gd-popover-content]')!;
    const trigger = document.querySelector('[data-gd-popover-trigger]')!;
    return {
      open: content.getAttribute('data-gd-popover-open'),
      expanded: trigger.getAttribute('aria-expanded'),
      nativeOpen: content.matches(':popover-open'),
      activeTestId: document.activeElement?.getAttribute('data-testid') ?? '',
      activeTag: document.activeElement?.tagName ?? '',
    };
  });
}

// Replace the rail's markup exactly as an HTMX rail refresh does.
async function refreshRail(page: Page): Promise<void> {
  await page.evaluate((html) => {
    document.getElementById('rail')!.innerHTML = html;
  }, RAIL_HTML);
}

test.describe('go-daisy popover runtime (#1440)', () => {
  test('open -> action -> rail refresh -> re-open keeps the runtime contract', async ({ page }) => {
    const errors = await bootstrap(page);

    // Closed by default: no open attribute, no aria-expanded.
    expect((await contentState(page)).open).not.toBe('true');

    // Open: content shows natively, the open flag and aria-expanded flip, and
    // the runtime does not move focus off the trigger.
    await page.getByTestId('popover-trigger').click();
    let state = await contentState(page);
    expect(state.open).toBe('true');
    expect(state.expanded).toBe('true');
    expect(state.nativeOpen).toBe(true);
    await expect(page.getByTestId('popover-content')).toBeVisible();
    expect(state.activeTestId).toBe('popover-trigger');
    expect(state.activeTag).not.toBe('BODY');

    // An action inside the content stays open: the runtime's clickaway guards
    // content, so a control can act without dismissing its own popover.
    await page.getByTestId('popover-action').click();
    state = await contentState(page);
    expect(state.open).toBe('true');
    expect(state.expanded).toBe('true');
    expect(state.nativeOpen).toBe(true);

    // The post-action HTMX rail refresh replaces the markup; the delegated
    // document listeners must still wire the freshly-rendered trigger, and the
    // fresh content starts closed.
    await refreshRail(page);
    state = await contentState(page);
    expect(state.open).not.toBe('true');
    expect(state.nativeOpen).toBe(false);

    // Re-open after the refresh.
    await page.getByTestId('popover-trigger').click();
    state = await contentState(page);
    expect(state.open).toBe('true');
    expect(state.expanded).toBe('true');
    expect(state.nativeOpen).toBe(true);
    expect(state.activeTag).not.toBe('BODY');

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('a click outside closes an open popover', async ({ page }) => {
    const errors = await bootstrap(page);

    await page.getByTestId('popover-trigger').click();
    expect((await contentState(page)).open).toBe('true');

    // Click a target outside the popover root: the runtime's closeAll runs.
    await page.evaluate(() => {
      const outside = document.createElement('button');
      outside.id = 'outside';
      document.body.appendChild(outside);
      outside.click();
    });

    const state = await contentState(page);
    expect(state.open).toBe('false');
    expect(state.expanded).toBe('false');
    expect(state.nativeOpen).toBe(false);

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('Escape closes an open popover', async ({ page }) => {
    const errors = await bootstrap(page);

    await page.getByTestId('popover-trigger').click();
    expect((await contentState(page)).open).toBe('true');

    await page.keyboard.press('Escape');
    const state = await contentState(page);
    expect(state.open).toBe('false');
    expect(state.expanded).toBe('false');
    expect(state.nativeOpen).toBe(false);

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('exposes the goDaisy.popover programmatic API', async ({ page }) => {
    const errors = await bootstrap(page);

    const api = await page.evaluate(() => {
      const p = (window as unknown as { goDaisy?: { popover?: Record<string, unknown> } })
        .goDaisy?.popover;
      return p ? Object.keys(p).sort() : [];
    });
    expect(api).toEqual(['close', 'closeAll', 'open', 'toggle']);

    // Toggle opens, then closes; open/close drive the same state.
    await page.evaluate(() => {
      const trigger = document.querySelector('[data-gd-popover-trigger]')!;
      (window as unknown as { goDaisy: { popover: { toggle: (t: Element) => void } } }).goDaisy.popover.toggle(
        trigger,
      );
    });
    expect((await contentState(page)).open).toBe('true');

    await page.evaluate(() => {
      (window as unknown as { goDaisy: { popover: { closeAll: () => void } } }).goDaisy.popover.closeAll();
    });
    expect((await contentState(page)).open).toBe('false');

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });
});
