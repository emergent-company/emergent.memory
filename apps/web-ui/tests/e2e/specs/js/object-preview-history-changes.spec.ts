import { test, expect } from '@playwright/test';

// Hermetic DOM-level spec for the object-preview History timeline's change
// list: every history entry is a native <details>/<summary> (collapsed by
// default, keyboard operable, no JS) whose body reveals the changes recorded
// for that version — a recorded change_summary renders "+ /path" (added),
// "− /path" (removed), and "/path: from → to" (updated), while a version with
// no summary derives "status: from → to" from the preceding version.
//
// No gateway, no memory API, no auth. The fixture mirrors the markup the
// gateway's `objectPreviewHistory` templ emits (the Go unit tests assert the
// same contract; this spec proves the native disclosure actually expands in a
// real browser engine, which a string assertion cannot).
const HISTORY_HTML = `<!doctype html><html><body>
<ol class="flex flex-col gap-3" data-testid="object-preview-history">
  <li class="border-base-content/10 border-l-2">
    <details class="group" data-testid="object-preview-history-entry">
      <summary class="flex cursor-pointer list-none flex-col gap-1 py-0.5 pl-3 [&::-webkit-details-marker]:hidden">
        <div class="flex flex-wrap items-center gap-2">
          <span class="font-mono text-xs font-medium">v2</span>
        </div>
      </summary>
      <div class="flex flex-col pl-3 pt-1" data-testid="object-preview-history-changes">
        <ul class="flex flex-col gap-0.5 pb-1">
          <li><span class="text-success font-mono text-xs">+ /summary</span></li>
          <li><span class="text-error font-mono text-xs">− /old</span></li>
          <li><span class="font-mono text-xs break-words">/title: draft → final</span></li>
        </ul>
      </div>
    </details>
  </li>
  <li class="border-base-content/10 border-l-2">
    <details class="group" data-testid="object-preview-history-entry">
      <summary class="flex cursor-pointer list-none flex-col gap-1 py-0.5 pl-3 [&::-webkit-details-marker]:hidden">
        <div class="flex flex-wrap items-center gap-2">
          <span class="font-mono text-xs font-medium">v1</span>
        </div>
      </summary>
      <div class="flex flex-col pl-3 pt-1" data-testid="object-preview-history-changes">
        <ul class="flex flex-col gap-0.5 pb-1">
          <li><span class="font-mono text-xs break-words">status: ready → in_progress</span></li>
        </ul>
      </div>
    </details>
  </li>
</ol>
</body></html>`;

test.describe('Object preview history change list', () => {
  test('entries are collapsed by default and expand to reveal their changes', async ({ page }) => {
    await page.setContent(HISTORY_HTML);

    const entries = page.getByTestId('object-preview-history-entry');
    await expect(entries).toHaveCount(2);

    // Collapsed: neither version's change list is visible until expanded.
    await expect(page.getByText('+ /summary')).toBeHidden();
    await expect(page.getByText('status: ready → in_progress')).toBeHidden();

    // Expanding the first entry reveals its change list…
    await entries.nth(0).locator('summary').click();
    await expect(page.getByText('+ /summary')).toBeVisible();
    await expect(page.getByText('− /old')).toBeVisible();
    await expect(page.getByText('/title: draft → final')).toBeVisible();

    // …while the second entry stays collapsed.
    await expect(page.getByText('status: ready → in_progress')).toBeHidden();
  });

  test('a summary is keyboard operable (Enter expands it)', async ({ page }) => {
    await page.setContent(HISTORY_HTML);

    const summary = page.getByTestId('object-preview-history-entry').nth(1).locator('summary');
    await summary.focus();
    await summary.press('Enter');

    await expect(page.getByText('status: ready → in_progress')).toBeVisible();
  });
});
