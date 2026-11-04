import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Hermetic DOM-level wiring spec for the app-wide alert dialog
// (gateway/webui/static/js/app.js → window.MemoryApp.alert, opened by the
// `memory-alert` HX-Trigger event). Loads the exact shipped app.js into a bare
// page and drives the handler against markup mirroring components.AlertModal.
//
// It guards: the open/close contract (showModal, not auto-open), the kind→tone
// mapping, the event-delegation wiring, and the XSS boundary — provider/model
// test replies are untrusted and MUST be written with textContent, never
// innerHTML. No gateway, no memory API, no Zitadel — runs in CI.

const APP_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/app.js',
);

// Mirrors components.AlertModal (ids + classes app.js reads/writes).
const MODAL_HTML = `<!doctype html><html><body>
  <dialog id="memory-alert-modal" class="modal" aria-labelledby="memory-alert-title">
    <div class="modal-box max-w-md">
      <div id="memory-alert-banner" class="alert alert-info items-start gap-3" role="alert"
           data-testid="memory-alert-banner">
        <span id="memory-alert-icon" class="iconify lucide--info size-5 shrink-0" aria-hidden="true"></span>
        <div class="min-w-0">
          <h3 id="memory-alert-title" class="text-base font-semibold">Test result</h3>
          <p id="memory-alert-message" class="mt-1 text-sm break-words whitespace-pre-wrap"></p>
        </div>
      </div>
      <div class="mt-5 flex justify-end">
        <form method="dialog">
          <button type="submit" class="btn btn-primary btn-sm">Close</button>
        </form>
      </div>
    </div>
    <form method="dialog" class="modal-backdrop"><button>close</button></form>
  </dialog>
</body></html>`;

async function bootstrap(page: Page, html: string = MODAL_HTML): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));
  await page.setContent(html);
  await page.addScriptTag({ path: APP_JS });
  return errors;
}

async function openAlert(
  page: Page,
  kind: string,
  title: string,
  message: string,
): Promise<void> {
  await page.evaluate(
    ({ kind, title, message }) => {
      (window as unknown as { MemoryApp: { alert: (k: string, t: string, m: string) => void } }).MemoryApp.alert(
        kind,
        title,
        message,
      );
    },
    { kind, title, message },
  );
}

test.describe('Alert modal wiring (app.js)', () => {
  test('opens on MemoryApp.alert and maps success to the success tone', async ({ page }) => {
    const errors = await bootstrap(page);

    await openAlert(page, 'success', 'Provider test', 'ollama OK: llama3 replied "pong" in 42ms');

    const state = await page.evaluate(() => {
      const dialog = document.getElementById('memory-alert-modal') as HTMLDialogElement;
      return {
        open: dialog.open,
        banner: document.getElementById('memory-alert-banner')!.className,
        icon: document.getElementById('memory-alert-icon')!.className,
        title: document.getElementById('memory-alert-title')!.textContent,
        message: document.getElementById('memory-alert-message')!.textContent,
      };
    });

    expect(state.open).toBe(true);
    expect(state.banner).toContain('alert-success');
    expect(state.icon).toContain('lucide--check-circle');
    expect(state.title).toBe('Provider test');
    expect(state.message).toBe('ollama OK: llama3 replied "pong" in 42ms');
    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('opens from the delegated memory-alert event with the error tone', async ({ page }) => {
    const errors = await bootstrap(page);

    await page.evaluate(() => {
      document.body.dispatchEvent(
        new CustomEvent('memory-alert', {
          detail: { kind: 'error', title: 'Model test', message: 'service down' },
        }),
      );
    });

    const state = await page.evaluate(() => {
      const dialog = document.getElementById('memory-alert-modal') as HTMLDialogElement;
      return {
        open: dialog.open,
        banner: document.getElementById('memory-alert-banner')!.className,
        icon: document.getElementById('memory-alert-icon')!.className,
        title: document.getElementById('memory-alert-title')!.textContent,
        message: document.getElementById('memory-alert-message')!.textContent,
      };
    });

    expect(state.open).toBe(true);
    expect(state.banner).toContain('alert-error');
    expect(state.icon).toContain('lucide--circle-alert');
    expect(state.title).toBe('Model test');
    expect(state.message).toBe('service down');
    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('falls back to the info tone for an unknown kind', async ({ page }) => {
    await bootstrap(page);
    await openAlert(page, 'nonsense', 'T', 'M');
    const cls = await page.evaluate(() => document.getElementById('memory-alert-banner')!.className);
    expect(cls).toContain('alert-info');
  });

  test('renders untrusted provider replies as text, never markup (XSS boundary)', async ({ page }) => {
    await bootstrap(page);

    const payload = '<img src=x onerror="window.__xss=1">';
    await openAlert(page, 'error', '<b>t</b>', payload);

    const state = await page.evaluate(() => ({
      message: document.getElementById('memory-alert-message')!.textContent,
      title: document.getElementById('memory-alert-title')!.textContent,
      injectedImg: document.querySelector('#memory-alert-message img') !== null,
      xss: (window as unknown as { __xss?: number }).__xss,
    }));

    expect(state.message).toBe(payload);
    expect(state.title).toBe('<b>t</b>');
    expect(state.injectedImg).toBe(false);
    expect(state.xss).toBeUndefined();
  });

  test('closes via the Close button and Escape', async ({ page }) => {
    await bootstrap(page);
    await openAlert(page, 'success', 'Provider test', 'ok');
    expect(await page.evaluate(() => (document.getElementById('memory-alert-modal') as HTMLDialogElement).open)).toBe(true);

    await page.locator('#memory-alert-modal .modal-box button').click();
    expect(await page.evaluate(() => (document.getElementById('memory-alert-modal') as HTMLDialogElement).open)).toBe(false);

    await openAlert(page, 'success', 'Provider test', 'ok again');
    expect(await page.evaluate(() => (document.getElementById('memory-alert-modal') as HTMLDialogElement).open)).toBe(true);
    await page.keyboard.press('Escape');
    expect(await page.evaluate(() => (document.getElementById('memory-alert-modal') as HTMLDialogElement).open)).toBe(false);
  });

  test('no-ops safely when the shell modal is absent', async ({ page }) => {
    const errors = await bootstrap(page, '<!doctype html><html><body></body></html>');
    await page.evaluate(() => {
      (window as unknown as { MemoryApp: { alert: (k: string, t: string, m: string) => void } }).MemoryApp.alert(
        'error',
        'x',
        'y',
      );
    });
    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });
});
