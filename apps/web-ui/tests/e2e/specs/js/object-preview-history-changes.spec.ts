import { test, expect } from '@playwright/test';
import fs from 'node:fs';
import path from 'node:path';

// Hermetic DOM-level spec for the object-preview History timeline: every history
// entry is a native <details>/<summary> (collapsed by default, keyboard
// operable, no JS) whose body reveals the changes recorded for that version.
//
// The markup is NOT hand-copied here. Both documents under specs/js/fixtures/
// are rendered from the shipping `objectPreviewHistory` templ by
// TestE2EObjectPreviewFixturesRatchet (apps/web-ui/gateway/object_preview_e2e_fixtures_test.go),
// which re-renders them on every `go test` run. A templ change that alters the
// markup therefore fails that Go test until the fixtures are regenerated with
// `-update-fixtures`, so the fixture diff lands in the same PR as the template
// change and this spec always drives what ships (#1083). The previous
// hand-maintained fixture had already silently lost the actor and status
// badges and invented the avatar internals.
//
// What this spec adds over the Go render tests is the browser behaviour: the
// native disclosure really expands, and the provenance line really is in the
// always-visible summary. No gateway, no memory API, no auth.

const FIXTURE_DIR = path.resolve(__dirname, 'fixtures');
const CHANGES_HTML = fs.readFileSync(
  path.join(FIXTURE_DIR, 'object-preview-history-changes.html'),
  'utf8',
);
const PROVENANCE_HTML = fs.readFileSync(
  path.join(FIXTURE_DIR, 'object-preview-history-provenance.html'),
  'utf8',
);

test.describe('Object preview history change list', () => {
  test('entries are collapsed by default and expand to reveal their changes', async ({ page }) => {
    await page.setContent(CHANGES_HTML);

    const entries = page.getByTestId('object-preview-history-entry');
    await expect(entries).toHaveCount(3);

    // Collapsed: no version's change list is visible until expanded.
    await expect(page.getByText('+ /summary')).toBeHidden();
    await expect(page.getByText('status: ready → in_progress')).toBeHidden();

    // Expanding the first entry reveals its recorded change_summary lines…
    await entries.nth(0).locator('summary').click();
    await expect(page.getByText('+ /summary')).toBeVisible();
    await expect(page.getByText('− /old')).toBeVisible();
    await expect(page.getByText('/title: draft → final')).toBeVisible();

    // …while the other entries stay collapsed.
    await expect(page.getByText('status: ready → in_progress')).toBeHidden();
    await expect(page.getByText('No recorded changes for this version.')).toBeHidden();
  });

  test('a summary is keyboard operable (Enter expands it)', async ({ page }) => {
    await page.setContent(CHANGES_HTML);

    const summary = page.getByTestId('object-preview-history-entry').nth(1).locator('summary');
    await summary.focus();
    await summary.press('Enter');

    // This version carries no change_summary, so its list is derived by
    // diffing the preceding version — the point is that the native summary is
    // operable without JS.
    await expect(page.getByText('status: ready → in_progress')).toBeVisible();
  });
});

test.describe('Object preview history provenance line', () => {
  test('an ordered write leads with the person, then the agent, and drops the credential', async ({
    page,
  }) => {
    await page.setContent(PROVENANCE_HTML);

    const entry = page.getByTestId('object-preview-history-entry').first();
    const row = entry.getByTestId('object-preview-history-provenance');

    // The summary chrome the template renders: the workflow status badge and
    // the absolute stamp with the raw server value in the tooltip.
    await expect(entry.locator('[data-status="done"]')).toHaveText('Done');
    const when = entry.getByTestId('object-preview-history-when');
    await expect(when).toHaveText('2 Jan 2024, 09:30');
    await expect(when).toHaveAttribute('title', 'Recorded 2024-01-02T09:30:00Z');

    // The agent badge marks the mechanism, then the person who ordered the
    // work leads the line with their identity mark.
    await expect(row.locator('.badge-ghost')).toHaveText('agent');
    await expect(row).toContainText("on Maciek Kucharz's order");

    // The mark is ui.AvatarFull, not a hand-drawn circle: .avatar wrapping an
    // initials placeholder is that component's class contract.
    const avatar = row.getByTestId('object-actor-avatar');
    await expect(avatar).toBeVisible();
    await expect(avatar.locator('.avatar > .avatar-placeholder')).toHaveText('MK');

    // The agent links to its definition page, and the run chip to its run.
    const agentLink = row.getByRole('link', { name: 'researcher' });
    await expect(agentLink).toBeVisible();
    await expect(agentLink).toHaveAttribute('href', '/agents/a1');
    const runChip = row.getByTestId('object-preview-history-run');
    await expect(runChip).toHaveText('run 0193f2a1');
    await expect(runChip).toHaveAttribute(
      'href',
      '/runs/0193f2a1-0000-4000-8000-000000000001',
    );

    // v2 does not repeat the credential: "via <agent>" already says how.
    await expect(row).not.toContainText('MCP link');

    // The provenance line is in the always-visible summary, not behind the
    // disclosure.
    await expect(row).toBeVisible();
  });

  test('a write with no recorded orderer keeps the writer-and-credential line', async ({
    page,
  }) => {
    await page.setContent(PROVENANCE_HTML);

    const entry = page.getByTestId('object-preview-history-entry').nth(1);
    const row = entry.getByTestId('object-preview-history-provenance');

    await expect(entry.locator('[data-status="in_progress"]')).toHaveText('In progress');
    await expect(entry.getByTestId('object-preview-history-when')).toHaveAttribute(
      'title',
      'Recorded 2024-01-01T09:30:00Z',
    );

    // No orderer: the agent stands alone with the credential, as before v2.
    await expect(row.locator('.badge-ghost')).toHaveText('agent');
    await expect(row).toContainText('researcher');
    await expect(row).toContainText('via agent MCP link');
    await expect(row).not.toContainText('order');

    // An agent-only row paints no person avatar, and its run still links.
    await expect(row.getByTestId('object-actor-avatar')).toHaveCount(0);
    await expect(row.getByTestId('object-preview-history-run')).toHaveAttribute(
      'href',
      '/runs/0193f2a1-0000-4000-8000-000000000002',
    );

    await expect(row).toBeVisible();
  });
});
