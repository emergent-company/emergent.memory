import { test, expect } from '@playwright/test';

// Hermetic DOM-level spec for the object-preview History timeline's change
// list: every history entry is a native <details>/<summary> (collapsed by
// default, keyboard operable, no JS) whose body reveals the changes recorded
// for that version — a recorded change_summary renders "+ /path" (added),
// "− /path" (removed), and "/path: from → to" (updated), while a version with
// no summary derives "status: from → to" from the preceding version.
//
// The summary is also where provenance v2 lives: an agent's write that a person
// ordered reads "on <name>'s order · via <agent>" (the credential is dropped —
// the agent and the run already say how), while a write with no recorded orderer
// keeps the pre-v2 "via agent MCP link" line. PROVENANCE_HTML covers that pair.
//
// No gateway, no memory API, no auth. The fixtures mirror the markup the
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

// Provenance v2 pair: v4 is an agent's write a person ordered (the orderer leads,
// the agent is the mechanism, the credential is dropped); v3 predates the
// orderer and keeps the pre-v2 line. Both are collapsed entries, so the line is
// visible without expanding anything — that is the point of putting it in the
// summary.
const PROVENANCE_HTML = `<!doctype html><html><body>
<ol class="flex flex-col gap-3" data-testid="object-preview-history">
  <li class="border-base-content/10 border-l-2">
    <details class="group" data-testid="object-preview-history-entry">
      <summary class="flex cursor-pointer list-none flex-col gap-1 py-0.5 pl-3 [&::-webkit-details-marker]:hidden">
        <div class="flex flex-wrap items-center gap-2">
          <span class="font-mono text-xs font-medium">v4</span>
          <span class="text-muted-faint ml-auto text-xs whitespace-nowrap" title="Recorded 2026-01-02T09:30:00Z · 2h ago" data-testid="object-preview-history-when">2 Jan 2026, 09:30</span>
          <span class="size-3.5 shrink-0 text-muted-faint transition-transform group-open:rotate-180"></span>
        </div>
        <div class="flex flex-wrap items-center gap-x-1.5 gap-y-0.5 text-xs" data-testid="object-preview-history-provenance">
          <span class="shrink-0" aria-hidden="true" data-testid="object-actor-avatar"><span class="size-6 rounded-full bg-base-200 text-[10px]">MK</span></span>
          <span class="text-muted-strong min-w-0 break-words" title="Ordered by Maciek Kucharz (u1)">on Maciek Kucharz's order</span>
          <span class="text-muted-faint whitespace-nowrap">via</span>
          <a href="/agents/a1" class="text-muted-strong min-w-0 truncate hover:underline" title="agent a1">researcher</a>
          <a href="/runs/0193f2a1-0000-4000-8000-000000000001" class="text-muted-faint font-mono whitespace-nowrap hover:underline" title="agent run 0193f2a1-0000-4000-8000-000000000001" data-testid="object-preview-history-run">run 0193f2a1</a>
        </div>
        <div class="text-muted min-w-0 text-xs break-words">note:pr-42</div>
      </summary>
      <div class="flex flex-col pl-3 pt-1" data-testid="object-preview-history-changes">
        <ul class="flex flex-col gap-0.5 pb-1">
          <li><span class="font-mono text-xs break-words">/status: in_progress → done</span></li>
        </ul>
      </div>
    </details>
  </li>
  <li class="border-base-content/10 border-l-2">
    <details class="group" data-testid="object-preview-history-entry">
      <summary class="flex cursor-pointer list-none flex-col gap-1 py-0.5 pl-3 [&::-webkit-details-marker]:hidden">
        <div class="flex flex-wrap items-center gap-2">
          <span class="font-mono text-xs font-medium">v3</span>
          <span class="text-muted-faint ml-auto text-xs whitespace-nowrap" title="Recorded 2026-01-01T09:30:00Z" data-testid="object-preview-history-when">1 Jan 2026, 09:30</span>
          <span class="size-3.5 shrink-0 text-muted-faint transition-transform group-open:rotate-180"></span>
        </div>
        <div class="flex flex-wrap items-center gap-x-1.5 gap-y-0.5 text-xs" data-testid="object-preview-history-provenance">
          <a href="/agents/a1" class="text-muted-strong min-w-0 truncate hover:underline" title="agent a1">researcher</a>
          <span class="text-muted-faint whitespace-nowrap">via agent MCP link</span>
          <a href="/runs/0193f2a1-0000-4000-8000-000000000002" class="text-muted-faint font-mono whitespace-nowrap hover:underline" title="agent run 0193f2a1-0000-4000-8000-000000000002" data-testid="object-preview-history-run">run 0193f2a1</a>
        </div>
        <div class="text-muted min-w-0 text-xs break-words">note:pr-42</div>
      </summary>
      <div class="flex flex-col pl-3 pt-1" data-testid="object-preview-history-changes">
        <ul class="flex flex-col gap-0.5 pb-1">
          <li><span class="text-success font-mono text-xs">+ /status</span></li>
        </ul>
      </div>
    </details>
  </li>
</ol>
</body></html>`;

test.describe('Object preview history provenance line', () => {
  test('an ordered write leads with the person, then the agent, and drops the credential', async ({ page }) => {
    await page.setContent(PROVENANCE_HTML);

    const row = page.getByTestId('object-preview-history-provenance').first();

    // The person who ordered the work leads the line, with their identity mark.
    await expect(row).toContainText("on Maciek Kucharz's order");
    await expect(row.getByTestId('object-actor-avatar')).toBeVisible();

    // The agent is the mechanism, and it links to its definition page.
    await expect(row).toContainText('via');
    const agentLink = row.getByRole('link', { name: 'researcher' });
    await expect(agentLink).toBeVisible();
    await expect(agentLink).toHaveAttribute('href', '/agents/a1');

    // The run chip still identifies the run and links to it.
    const runChip = row.getByTestId('object-preview-history-run');
    await expect(runChip).toHaveText('run 0193f2a1');
    await expect(runChip).toHaveAttribute('href', '/runs/0193f2a1-0000-4000-8000-000000000001');

    // v2 does not repeat the credential: "via <agent>" already says how.
    await expect(row).not.toContainText('MCP link');
  });

  test('a write with no recorded orderer keeps the writer-and-credential line', async ({ page }) => {
    await page.setContent(PROVENANCE_HTML);

    const row = page.getByTestId('object-preview-history-provenance').nth(1);

    // No orderer: the agent stands alone with the credential, as before v2.
    await expect(row).toContainText('researcher');
    await expect(row).toContainText('via agent MCP link');
    await expect(row).not.toContainText('order');

    // An agent-only row paints no person avatar, and its run still links.
    await expect(row.getByTestId('object-actor-avatar')).toHaveCount(0);
    await expect(row.getByTestId('object-preview-history-run')).toHaveAttribute(
      'href',
      '/runs/0193f2a1-0000-4000-8000-000000000002',
    );

    // The provenance line is in the always-visible summary, not behind the
    // disclosure.
    await expect(row).toBeVisible();
  });
});
