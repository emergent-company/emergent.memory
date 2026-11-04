import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Hermetic DOM-level wiring spec for the toast severity rule in
// gateway/webui/static/js/app.js → window.MemoryApp.toast, and the delegated
// `memory-toast` HX-Trigger event. Loads the exact shipped app.js into a bare
// page and drives it against a fake Alpine toast queue that records add().
//
// It guards the web-ui half of `feedback.toast`: errors must be added with
// duration 0 (persistent — the go-daisy queue then renders a close button and
// arms no timer), and every other kind with the shared auto-dismiss timeout.
// The queue's runtime persistence itself is covered by go-daisy's unit tests
// (components/alpine/toast_test.go, components/ui/toast-queue_test.go).
//
// No gateway, no memory API, no Zitadel — runs in CI via
// `npx playwright test --config=js-dom.config.ts`.

const APP_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/app.js',
);

interface AddedToast {
  type: string;
  message: string;
  duration: number;
}

const AUTO_DISMISS_MS = 4200;

// Bare page with a #toast-container and a fake Alpine queue: $data() returns an
// object whose add() records every toast. Mirrors only the surface app.js reads.
async function bootstrap(page: Page): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));
  await page.setContent(
    '<!doctype html><html><body><div id="toast-container"></div></body></html>',
  );
  await page.evaluate(() => {
    const w = window as unknown as {
      Alpine: { $data: (el: Element) => unknown };
      __added: AddedToast[];
    };
    w.__added = [];
    w.Alpine = {
      $data: () => ({
        add: (t: AddedToast) => {
          w.__added.push(t);
        },
      }),
    };
  });
  await page.addScriptTag({ path: APP_JS });
  return errors;
}

async function added(page: Page): Promise<AddedToast[]> {
  return page.evaluate(() => (window as unknown as { __added: AddedToast[] }).__added);
}

test.describe('Toast severity wiring (app.js)', () => {
  test('error toasts are persistent: added with duration 0', async ({ page }) => {
    const errors = await bootstrap(page);

    await page.evaluate(() => {
      (window as unknown as { MemoryApp: { toast: (k: string, m: string) => void } }).MemoryApp.toast(
        'error',
        'Save failed',
      );
    });

    expect(await added(page)).toEqual([{ type: 'error', message: 'Save failed', duration: 0 }]);
    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('non-error toasts auto-dismiss with the shared timeout', async ({ page }) => {
    const errors = await bootstrap(page);

    await page.evaluate(() => {
      const app = (window as unknown as { MemoryApp: { toast: (k: string, m: string) => void } }).MemoryApp;
      app.toast('success', 'Saved');
      app.toast('info', 'Heads up');
      app.toast('warning', 'Careful');
    });

    expect(await added(page)).toEqual([
      { type: 'success', message: 'Saved', duration: AUTO_DISMISS_MS },
      { type: 'info', message: 'Heads up', duration: AUTO_DISMISS_MS },
      { type: 'warning', message: 'Careful', duration: AUTO_DISMISS_MS },
    ]);
    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('the delegated memory-toast event uses the severity rule', async ({ page }) => {
    const errors = await bootstrap(page);

    await page.evaluate(() => {
      document.body.dispatchEvent(
        new CustomEvent('memory-toast', { detail: { kind: 'error', message: 'Nope' } }),
      );
      document.body.dispatchEvent(
        new CustomEvent('memory-toast', { detail: { kind: 'success', message: 'Yep' } }),
      );
    });

    expect(await added(page)).toEqual([
      { type: 'error', message: 'Nope', duration: 0 },
      { type: 'success', message: 'Yep', duration: AUTO_DISMISS_MS },
    ]);
    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('no-ops safely when the toast container is absent', async ({ page }) => {
    const errors: string[] = [];
    page.on('pageerror', (err) => errors.push(err.message));
    await page.setContent('<!doctype html><html><body></body></html>');
    await page.evaluate(() => {
      (window as unknown as { Alpine: { $data: (el: Element) => unknown } }).Alpine = {
        $data: () => ({ add: () => {} }),
      };
    });
    await page.addScriptTag({ path: APP_JS });

    await page.evaluate(() => {
      (window as unknown as { MemoryApp: { toast: (k: string, m: string) => void } }).MemoryApp.toast(
        'error',
        'x',
      );
    });

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });
});
