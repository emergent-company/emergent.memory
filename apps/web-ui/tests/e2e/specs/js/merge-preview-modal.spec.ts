import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';
import fs from 'node:fs';

// Hermetic regression spec for the extraction merge-preview modal never opening.
//
// The "Merge into main" button fetches the preview lazily into a SIBLING
// container (`hx-get=…/merge-preview`, `hx-target="#merge-preview-target-<job>"`,
// `hx-swap="innerHTML"`), and the response carries
// `data-dialog-autoopen="true"` (gateway/documents.templ mergePreviewModal).
// htmx 4 dispatches `htmx:after:swap` on the swap SOURCE element — the button,
// which does not contain the swapped-in dialog — so app.js scoping the
// auto-open scan to `ev.target` left the modal inserted but closed. This spec
// loads the shipped `htmx.min.js` + `app.js` verbatim and drives that exact
// shape: click a button whose hx-get returns a dialog into a sibling div.
//
// No gateway, no memory API, no auth — runs in CI under `js-dom.config.ts`
// (`task -d apps/web-ui e2e:js`).

const HTMX_JS = path.resolve(__dirname, '../../../../gateway/webui/static/js/htmx.min.js');
const APP_JS = path.resolve(__dirname, '../../../../gateway/webui/static/js/app.js');

const PREVIEW_URL = '/documents/d1/extractions/job1/merge-preview';
const ACTION_URL = '/documents/d1/extractions/job1/merge';

// Mirrors the extraction card's merge action: the trigger and its EMPTY sibling
// target div, which is the shape that exposed the bug.
const CARD_HTML = `<!doctype html>
<html>
<head><meta charset="utf-8" /></head>
<body>
<div class="mt-4" data-testid="extraction-merge">
  <button
    type="button"
    class="btn btn-primary btn-sm"
    data-testid="extraction-merge-button"
    hx-get="${PREVIEW_URL}"
    hx-target="#merge-preview-target-job1"
    hx-swap="innerHTML"
  >Merge into main</button>
</div>
<div id="merge-preview-target-job1"></div>
<script src="/htmx.min.js"></script>
<script src="/app.js"></script>
</body>
</html>`;

interface PreviewOpts {
  added: number;
  updated: number;
  conflicts: number;
  left: number;
  total: number;
  error?: string;
}

// Mirrors mergePreviewModal: dialog id, data-dialog-autoopen, the predicted
// split, and the confirm/cancel wiring.
function previewHTML(opts: PreviewOpts): string {
  const confirm = opts.error
    ? `<button type="submit" class="btn btn-ghost btn-sm" formmethod="dialog">Close</button>`
    : `<button type="submit" class="btn btn-primary btn-sm" data-testid="merge-preview-confirm">Merge ${opts.added + opts.updated} objects into main</button>`;
  const body = opts.error
    ? `<div class="alert alert-error" role="alert">${opts.error}</div>`
    : `<div>
        <div class="flex items-center gap-2 py-1"><span class="iconify lucide--plus"></span><span class="grow text-sm text-muted">New objects</span><span class="text-sm tabular-nums">${opts.added}</span></div>
        <div class="flex items-center gap-2 py-1"><span class="iconify lucide--refresh-cw"></span><span class="grow text-sm text-muted">Existing objects updated</span><span class="text-sm tabular-nums">${opts.updated}</span></div>
        <div class="flex items-center gap-2 py-1"><span class="iconify lucide--triangle-alert"></span><span class="grow text-sm text-muted">Conflicts</span><span class="text-sm tabular-nums">${opts.conflicts}</span></div>
        <div class="flex items-center gap-2 py-1"><span class="iconify lucide--git-branch"></span><span class="grow text-sm text-muted">Left for review</span><span class="text-sm tabular-nums">${opts.left}</span></div>
        <div class="flex items-center gap-2 py-1"><span class="iconify lucide--box"></span><span class="grow text-sm text-muted">Total</span><span class="text-sm tabular-nums">${opts.total}</span></div>
      </div>`;
  return `<dialog id="merge-preview-modal" class="modal" data-dialog-autoopen="true" data-testid="merge-preview-modal">
  <div class="modal-box max-w-md">
    <h3 class="text-lg font-semibold">Review merge</h3>
    <p class="text-muted text-xs mb-4">Clean additions and fast-forwards merge into the main graph. Anything left over stays on this branch for review.</p>
    ${body}
    <form method="post" action="${ACTION_URL}">
      <div class="mt-4 flex justify-end gap-2">
        <button type="submit" class="btn btn-ghost btn-sm" formmethod="dialog">Cancel</button>
        ${confirm}
      </div>
    </form>
  </div>
</dialog>`;
}

async function bootstrap(page: Page, preview: PreviewOpts): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));

  const htmx = fs.readFileSync(HTMX_JS, 'utf8');
  const app = fs.readFileSync(APP_JS, 'utf8');

  await page.route('**/*', (route) => {
    const req = route.request();
    const p = new URL(req.url()).pathname;
    if (p === '/') return route.fulfill({ contentType: 'text/html', body: CARD_HTML });
    if (p === PREVIEW_URL) return route.fulfill({ contentType: 'text/html', body: previewHTML(preview) });
    if (p === '/htmx.min.js') return route.fulfill({ contentType: 'application/javascript', body: htmx });
    if (p === '/app.js') return route.fulfill({ contentType: 'application/javascript', body: app });
    if (p === ACTION_URL) return route.fulfill({ status: 204, body: '' });
    return route.fulfill({ status: 404, body: 'not found' });
  });

  await page.goto('http://localhost/');
  return errors;
}

const HEALTHY: PreviewOpts = { added: 2, updated: 1, conflicts: 0, left: 0, total: 3 };

test.describe('extraction merge preview modal', () => {
  test('auto-opens after the lazy htmx swap into the sibling target', async ({ page }) => {
    const errors = await bootstrap(page, HEALTHY);
    const modal = page.getByTestId('merge-preview-modal');

    // Nothing is fetched or shown until the user asks for the preview.
    await expect(modal).toHaveCount(0);

    await page.getByTestId('extraction-merge-button').click();

    // The dialog must be swapped in AND opened — the regression was a dialog
    // inserted into the sibling target that never got showModal().
    await expect(modal).toBeVisible();
    await expect(modal).toHaveJSProperty('open', true);
    await expect(modal.getByRole('heading', { name: 'Review merge' })).toBeVisible();

    // The predicted split is rendered for every row.
    for (const [label, value] of [
      ['New objects', '2'],
      ['Existing objects updated', '1'],
      ['Conflicts', '0'],
      ['Left for review', '0'],
      ['Total', '3'],
    ]) {
      const row = modal.locator('div.flex.items-center', { hasText: label });
      await expect(row).toContainText(label);
      await expect(row).toContainText(value);
    }

    const confirm = modal.getByTestId('merge-preview-confirm');
    await expect(confirm).toBeVisible();
    await expect(confirm).toBeEnabled();
    await expect(confirm).toHaveAccessibleName('Merge 3 objects into main');

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('error-state preview still auto-opens and offers no confirm', async ({ page }) => {
    const errors = await bootstrap(page, {
      added: 0,
      updated: 0,
      conflicts: 0,
      left: 0,
      total: 0,
      error: 'preview unavailable',
    });
    const modal = page.getByTestId('merge-preview-modal');

    await page.getByTestId('extraction-merge-button').click();

    await expect(modal).toBeVisible();
    await expect(modal).toContainText('preview unavailable');
    await expect(modal.getByRole('button', { name: 'Close' })).toBeVisible();
    await expect(modal.getByTestId('merge-preview-confirm')).toHaveCount(0);

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('confirm posts the merge to the preview action URL', async ({ page }) => {
    const errors = await bootstrap(page, HEALTHY);
    const modal = page.getByTestId('merge-preview-modal');

    await page.getByTestId('extraction-merge-button').click();
    await expect(modal).toBeVisible();

    const [request] = await Promise.all([
      page.waitForRequest(
        (req) => req.method() === 'POST' && new URL(req.url()).pathname === ACTION_URL,
      ),
      modal.getByTestId('merge-preview-confirm').click(),
    ]);
    expect(request.method()).toBe('POST');
    expect(new URL(request.url()).pathname).toBe(ACTION_URL);

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });
});
