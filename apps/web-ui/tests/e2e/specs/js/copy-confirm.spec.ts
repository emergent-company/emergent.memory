import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Hermetic DOM-level spec for the copy confirmation on the object-preview
// "Copy object ID" button (#1964): clicking copy must show on the button itself
// — the copy glyph swaps to a check and `data-copied` drives the pop in
// app.css — then revert. Previously only aria-label/title changed, so a sighted
// user got no feedback at all.
//
// Loads the shipped object-preview.js verbatim into a bare page with a stubbed
// MemoryChatHost (resize grip). No gateway, no memory API, no clipboard
// permission: the confirmation is independent of the write result.
const OBJECT_PREVIEW_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/object-preview.js',
);

const PAGE_HTML = `<!doctype html><html><body>
  <div id="object-preview-root">
    <div id="object-preview-backdrop" class="hidden"></div>
    <aside id="object-preview-panel" role="dialog" aria-modal="true">
      <button id="object-preview-close" type="button">close</button>
      <div id="object-preview-body">
        <h3 id="object-preview-heading">Refund policy</h3>
        <span id="object-preview-id" class="sr-only">obj-123</span>
        <button id="object-preview-copy-id" type="button" data-copy-target="#object-preview-id" aria-label="Copy object ID">
          <span class="iconify lucide--copy size-4" aria-hidden="true"></span>
        </button>
      </div>
      <div id="object-preview-actions-slot"></div>
    </aside>
  </div>
</body></html>`;

async function bootstrap(page: Page): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));

  await page.setContent(PAGE_HTML);
  await page.evaluate(() => {
    (window as unknown as { MemoryChatHost: unknown }).MemoryChatHost = {
      createResizeGrip: () => ({
        handle: null,
        init: () => {},
        onPointerDown: () => {},
        onWindowResize: () => {},
        setWidth: () => {},
        getWidth: () => 512,
      }),
    };
  });
  await page.addScriptTag({ path: OBJECT_PREVIEW_JS });
  return errors;
}

test.describe('Copy button confirmation (#1964)', () => {
  test('the copy glyph becomes a check for a moment, then reverts', async ({ page }) => {
    const errors = await bootstrap(page);

    const button = page.locator('#object-preview-copy-id');
    const icon = button.locator('.iconify');
    await expect(icon).toHaveClass(/lucide--copy/);

    await button.click();

    await expect(button).toHaveAttribute('data-copied', 'true');
    await expect(button).toHaveAttribute('aria-label', 'Copied');
    await expect(icon).toHaveClass(/lucide--check/);
    await expect(icon).not.toHaveClass(/lucide--copy/);

    // Reverts on its own, so a second copy starts from the copy affordance.
    await expect(icon).toHaveClass(/lucide--copy/, { timeout: 5_000 });
    await expect(button).not.toHaveAttribute('data-copied', 'true');
    await expect(button).toHaveAttribute('aria-label', 'Copy object ID');

    expect(errors, 'no page errors').toEqual([]);
  });
});
