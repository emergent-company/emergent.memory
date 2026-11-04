import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Hermetic, gateway-free wiring spec for the document chunk editor + revision
// history (#1473, improve-document-chunking-and-curation task 6.2).
//
// The shipped `documents-chunks.js` is loaded verbatim into a bare page and fed
// a stubbed fetch that behaves like the four chunk-curation routes. It covers
// edit-save, failed-save (draft preserved), the revision list, rollback
// confirmation + success, and the diff render. No gateway, no memory API, no
// auth — runs in CI under `js-dom.config.ts` (`task e2e:js`).

const DOCUMENTS_CHUNKS_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/documents-chunks.js',
);

// SKELETON mirrors the data-* contract documents_chunks.templ renders: the
// chunk row, its editor, history panel, diff region, per-chunk aria-live
// status, and the shared rollback confirm dialog.
const SKELETON = `<!doctype html>
<html>
<head><meta charset="utf-8" /></head>
<body>
<div data-chunk-row data-chunk-id="c1" data-chunk-index="0" data-chunk-revision="2" data-testid="chunk-row-0">
  <span data-chunk-edited-badge hidden>edited</span>
  <span data-chunk-size>11 chars</span>
  <button type="button" data-chunk-action="edit" aria-expanded="false" aria-controls="chunk-editor-0" data-testid="chunk-edit-0">Edit</button>
  <button type="button" data-chunk-action="history" aria-expanded="false" aria-controls="chunk-history-0" data-testid="chunk-history-open-0">History</button>
  <p data-chunk-text tabindex="-1">hello world</p>
  <form id="chunk-editor-0" data-chunk-editor hidden data-testid="chunk-editor-0">
    <textarea id="chunk-input-0" name="text" data-chunk-input data-testid="chunk-input-0"></textarea>
    <button type="submit" data-chunk-action="save" data-testid="chunk-save-0">Save</button>
    <button type="button" data-chunk-action="cancel-edit" data-testid="chunk-cancel-0">Cancel</button>
  </form>
  <div id="chunk-history-0" data-chunk-history hidden data-testid="chunk-history-0">
    <h3 data-chunk-history-heading tabindex="-1">Revision history</h3>
    <button type="button" data-chunk-action="close-history" data-testid="chunk-history-close-0">Close</button>
    <p data-chunk-revisions-status role="status" aria-live="polite"></p>
    <ol data-chunk-revisions></ol>
    <div data-chunk-diff-region hidden>
      <h4 data-chunk-diff-heading>Differences</h4>
      <pre data-chunk-diff aria-live="polite"></pre>
    </div>
  </div>
  <p data-chunk-status role="status" aria-live="polite"></p>
</div>
<dialog id="chunk-rollback-confirm" data-chunk-rollback-confirm data-testid="chunk-rollback-confirm">
  <p data-chunk-rollback-message></p>
  <button type="button" data-chunk-rollback-approve data-testid="chunk-rollback-confirm-button">Roll back</button>
  <button type="button" data-chunk-rollback-dismiss data-testid="chunk-rollback-cancel">Cancel</button>
</dialog>
</body>
</html>`;

interface FetchCall {
  url: string;
  method: string;
  body: string;
}

interface StubWindow {
  __calls: FetchCall[];
  __config: { patchStatus: number; patchError: string };
  fetch: (url: unknown, opts?: { method?: string; body?: string }) => Promise<Response>;
}

async function bootstrap(page: Page): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));

  await page.setContent(SKELETON);

  await page.evaluate(() => {
    const w = window as unknown as StubWindow;
    w.__calls = [];
    w.__config = { patchStatus: 200, patchError: '' };

    const json = (body: unknown, status = 200) =>
      new Response(JSON.stringify(body), {
        status,
        headers: { 'Content-Type': 'application/json' },
      });

    w.fetch = function (url: unknown, opts?: { method?: string; body?: string }) {
      const u = String(url);
      const method = (opts && opts.method) || 'GET';
      w.__calls.push({ url: u, method, body: (opts && opts.body) || '' });

      if (u === '/api/chunks/c1' && method === 'PATCH') {
        if (w.__config.patchStatus >= 400) {
          return Promise.resolve(json({ error: w.__config.patchError }, w.__config.patchStatus));
        }
        return Promise.resolve(json({ chunkId: 'c1', revision: 3 }));
      }
      if (u.indexOf('/api/chunks/c1/revisions/diff') === 0) {
        return Promise.resolve(
          json({
            from: 1,
            to: 2,
            lines: [
              { type: 'equal', text: 'hello world' },
              { type: 'add', text: 'edited' },
            ],
          }),
        );
      }
      if (u === '/api/chunks/c1/revisions') {
        return Promise.resolve(
          json({
            data: [
              { id: 'r1', chunkId: 'c1', seq: 1, text: 'hello world', author: 'alice', createdAt: '2026-09-01T10:00:00Z' },
              { id: 'r2', chunkId: 'c1', seq: 2, text: 'hello world edited', author: 'bob', createdAt: '2026-09-02T10:00:00Z' },
            ],
            totalCount: 2,
          }),
        );
      }
      if (u === '/api/chunks/c1/rollback' && method === 'POST') {
        return Promise.resolve(json({ chunkId: 'c1', revision: 3 }));
      }
      return Promise.resolve(json({}));
    };
  });

  await page.addScriptTag({ path: DOCUMENTS_CHUNKS_JS });
  return errors;
}

async function click(page: Page, selector: string): Promise<void> {
  await page.evaluate((sel) => {
    const el = document.querySelector(sel);
    if (!el) throw new Error(`missing element: ${sel}`);
    (el as HTMLElement).click();
  }, selector);
}

async function setText(page: Page, selector: string, value: string): Promise<void> {
  await page.evaluate(
    ({ sel, val }) => {
      const el = document.querySelector(sel) as HTMLTextAreaElement | null;
      if (!el) throw new Error(`missing element: ${sel}`);
      el.value = val;
    },
    { sel: selector, val: value },
  );
}

function calls(page: Page): Promise<FetchCall[]> {
  return page.evaluate(() => (window as unknown as StubWindow).__calls);
}

function text(page: Page, selector: string): Promise<string> {
  return page.evaluate((sel) => {
    const el = document.querySelector(sel);
    return el ? (el.textContent || '').trim() : '';
  }, selector);
}

function isHidden(page: Page, selector: string): Promise<boolean> {
  return page.evaluate((sel) => {
    const el = document.querySelector(sel) as HTMLElement | null;
    return !!el && (el.hidden || el.style.display === 'none');
  }, selector);
}

function activeTestId(page: Page): Promise<string> {
  return page.evaluate(
    () => (document.activeElement as HTMLElement | null)?.getAttribute('data-testid') || '',
  );
}

function activeIsHistoryHeading(page: Page): Promise<boolean> {
  return page.evaluate(
    () => (document.activeElement as HTMLElement | null)?.hasAttribute('data-chunk-history-heading') === true,
  );
}

test.describe('document chunk editor (documents-chunks.js)', () => {
  test('edit and save updates the text, revision, and status', async ({ page }) => {
    const errors = await bootstrap(page);

    await click(page, '[data-testid="chunk-edit-0"]');
    await expect.poll(() => activeTestId(page)).toBe('chunk-input-0');
    expect(await isHidden(page, '[data-testid="chunk-editor-0"]')).toBe(false);
    // Opening the editor seeds the (server-empty) textarea from the saved text.
    expect(await page.inputValue('[data-testid="chunk-input-0"]')).toBe('hello world');

    await setText(page, '[data-testid="chunk-input-0"]', 'hello edited world');
    await click(page, '[data-testid="chunk-save-0"]');

    await expect.poll(() => calls(page)).toHaveLength(1);
    const [call] = await calls(page);
    expect(call.method).toBe('PATCH');
    expect(call.url).toBe('/api/chunks/c1');
    expect(call.body).toContain('hello edited world');

    await expect.poll(() => text(page, '[data-chunk-text]')).toBe('hello edited world');
    await expect.poll(() => text(page, '[data-chunk-status]')).toContain('Saved as revision 3');
    await expect.poll(() => text(page, '[data-chunk-size]')).toBe('18 chars');
    await expect.poll(() => page.getAttribute('[data-chunk-row]', 'data-chunk-revision')).toBe('3');
    expect(await isHidden(page, '[data-testid="chunk-editor-0"]')).toBe(true);
    expect(await isHidden(page, '[data-chunk-edited-badge]')).toBe(false);
    await expect.poll(() => activeTestId(page)).toBe('chunk-edit-0');

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('a failed save keeps the draft and shows the error', async ({ page }) => {
    const errors = await bootstrap(page);
    await page.evaluate(() => {
      (window as unknown as StubWindow).__config = { patchStatus: 500, patchError: 'boom' };
    });

    await click(page, '[data-testid="chunk-edit-0"]');
    await setText(page, '[data-testid="chunk-input-0"]', 'draft that must survive');
    await click(page, '[data-testid="chunk-save-0"]');

    await expect.poll(() => text(page, '[data-chunk-status]')).toContain('Could not save: boom');
    // Draft preserved, editor still open, saved text untouched.
    expect(
      await page.inputValue('[data-testid="chunk-input-0"]'),
    ).toBe('draft that must survive');
    expect(await isHidden(page, '[data-testid="chunk-editor-0"]')).toBe(false);
    expect(await text(page, '[data-chunk-text]')).toBe('hello world');
    expect(await page.getAttribute('[data-chunk-status]', 'role')).toBe('alert');

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('opening history lists the revisions with author and time', async ({ page }) => {
    const errors = await bootstrap(page);

    await click(page, '[data-testid="chunk-history-open-0"]');
    await expect(page.locator('[data-chunk-revision-item]')).toHaveCount(2);
    await expect.poll(() => activeIsHistoryHeading(page)).toBe(true);

    const list = page.locator('[data-chunk-revisions]');
    await expect(list).toContainText('r1');
    await expect(list).toContainText('r2');
    await expect(list).toContainText('alice');
    await expect(list).toContainText('bob');
    await expect(list).toContainText('current');

    // Non-current revisions expose diff + rollback actions; the current one
    // does not.
    await expect(page.locator('[data-testid="chunk-rollback-0-1"]')).toHaveCount(1);
    await expect(page.locator('[data-testid="chunk-rollback-0-2"]')).toHaveCount(0);
    await expect(page.locator('[data-testid="chunk-diff-0-1"]')).toHaveCount(1);

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('rollback asks for confirmation and restores the revision on confirm', async ({ page }) => {
    const errors = await bootstrap(page);

    await click(page, '[data-testid="chunk-history-open-0"]');
    await expect(page.locator('[data-chunk-revision-item]')).toHaveCount(2);

    // A rendered diff must be dropped once the mutation makes it stale.
    await click(page, '[data-testid="chunk-diff-0-1"]');
    await expect(page.locator('[data-chunk-diff-region]')).toBeVisible();

    await click(page, '[data-testid="chunk-rollback-0-1"]');
    const dialog = page.locator('#chunk-rollback-confirm');
    await expect(dialog).toHaveJSProperty('open', true);
    await expect(page.locator('[data-chunk-rollback-message]')).toContainText('revision 1');

    // Nothing is sent until the user confirms.
    expect((await calls(page)).filter((c) => c.method === 'POST')).toHaveLength(0);

    await click(page, '[data-testid="chunk-rollback-confirm-button"]');
    await expect(dialog).toHaveJSProperty('open', false);
    await expect.poll(async () => (await calls(page)).some((c) => c.method === 'POST')).toBe(true);
    const post = (await calls(page)).find((c) => c.method === 'POST');
    expect(post?.url).toBe('/api/chunks/c1/rollback');
    expect(post?.body).toContain('"seq":1');

    await expect.poll(() => text(page, '[data-chunk-text]')).toBe('hello world');
    await expect.poll(() => text(page, '[data-chunk-status]')).toContain('Rolled back to revision 1');
    await expect.poll(() => page.getAttribute('[data-chunk-row]', 'data-chunk-revision')).toBe('3');
    await expect(page.locator('[data-chunk-diff-region]')).toBeHidden();

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('dismissing the rollback confirmation sends nothing', async ({ page }) => {
    const errors = await bootstrap(page);

    await click(page, '[data-testid="chunk-history-open-0"]');
    await click(page, '[data-testid="chunk-rollback-0-1"]');
    await click(page, '[data-testid="chunk-rollback-cancel"]');

    await expect(page.locator('#chunk-rollback-confirm')).toHaveJSProperty('open', false);
    expect((await calls(page)).filter((c) => c.method === 'POST')).toHaveLength(0);
    expect(await text(page, '[data-chunk-text]')).toBe('hello world');

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('viewing a diff renders add/equal lines', async ({ page }) => {
    const errors = await bootstrap(page);

    await click(page, '[data-testid="chunk-history-open-0"]');
    await click(page, '[data-testid="chunk-diff-0-1"]');

    const diff = page.locator('[data-chunk-diff]');
    await expect(diff.locator('[data-diff-type="equal"]')).toHaveCount(1);
    await expect(diff.locator('[data-diff-type="add"]')).toHaveCount(1);
    await expect(diff).toContainText('+ edited');
    await expect(page.locator('[data-chunk-diff-region]')).toBeVisible();

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('closing history resets aria-expanded and returns focus to the History trigger', async ({ page }) => {
    const errors = await bootstrap(page);

    await click(page, '[data-testid="chunk-history-open-0"]');
    await expect(page.locator('[data-testid="chunk-history-0"]')).toBeVisible();
    await expect.poll(() => page.getAttribute('[data-testid="chunk-history-open-0"]', 'aria-expanded')).toBe('true');

    // The Close button lives inside the panel; closing must update the visible
    // History trigger's aria-expanded and move focus back to it (not strand
    // focus inside the now-hidden panel).
    await click(page, '[data-testid="chunk-history-close-0"]');
    await expect(page.locator('[data-testid="chunk-history-0"]')).toBeHidden();
    await expect.poll(() => page.getAttribute('[data-testid="chunk-history-open-0"]', 'aria-expanded')).toBe('false');
    await expect.poll(() => activeTestId(page)).toBe('chunk-history-open-0');

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });
});
