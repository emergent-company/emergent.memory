import { test, expect, Page, Locator } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

// Backup details page + list cleanup (gateway/backups.templ, gateway/backups.go).
//
// The backups list dropped its Checksums column and the redundant plain-text
// "full backup" fallback: each row title now links to a dedicated details page
// (GET /backups/:id) with Overview / Contents / Settings / Archive format /
// Integrity sections, and full checksums live only on that page.
//
// These specs run in the `mutations` project (serial, self-cleaning) against the
// REAL dev gateway and the bootstrap org/project — backups are scoped to the
// active project, so no scratch tenant is created. Backups may be feature-gated
// off; when the list shows "Backups unavailable" the tests skip rather than
// fail. Any backup a test creates through the real form is deleted in `finally`
// via POST /backups/<id>/delete (the same PRG route the confirm dialog uses).

// A list/detail row title link: every `/backups/<id>` anchor except the
// per-row/preview `/download` action.
const BACKUP_ROW_LINK = 'a[href^="/backups/"]:not([href$="/download"])';

// beforeEach lands on /backups and skips the whole test when Memory has the
// backup feature gated off (the page renders the "Backups unavailable" empty
// state instead of the list).
test.beforeEach(async ({ page }) => {
  await page.goto('/backups');
  await expectAppPage(page, /Backups/);
  test.skip(
    (await page.getByText('Backups unavailable', { exact: true }).count()) > 0,
    'backups are feature-gated off in the connected Memory service',
  );
});

// section scopes a locator to the ui.Section card whose <h2> matches heading.
function section(page: Page, heading: string): Locator {
  return page.locator('section', {
    has: page.getByRole('heading', { name: heading, exact: true }),
  });
}

// rowForBackup locates the list row whose title/preview link points at the id.
function rowForBackup(page: Page, id: string): Locator {
  return page.locator('tr').filter({ has: page.locator(`a[href="/backups/${id}"]`) });
}

// firstRowBackupId reads the id of the newest backup row (the list is ordered
// most-recent-first), i.e. the one a just-submitted create produced.
async function firstRowBackupId(page: Page): Promise<string> {
  const href = await page.locator(BACKUP_ROW_LINK).first().getAttribute('href');
  if (!href) throw new Error('no backup row link found on /backups');
  return href.replace(/^\/backups\//, '').split('/')[0];
}

// startBackupViaForm submits the real inline create form (Start backup) and
// returns the new backup's id.
async function startBackupViaForm(page: Page): Promise<string> {
  await page.getByRole('button', { name: 'Start backup' }).click();
  await page.waitForURL(/\/backups\?created=1/, { timeout: 30_000 });
  await page.waitForLoadState('domcontentloaded');
  return firstRowBackupId(page);
}

// readyBackupId returns the id of an existing ready backup row, or null when the
// list has none (so the caller can create one via the form).
async function readyBackupId(page: Page): Promise<string | null> {
  const readyRows = page
    .locator('tr')
    .filter({ has: page.getByText('ready', { exact: true }) });
  for (let i = 0; i < (await readyRows.count()); i++) {
    const href = await readyRows.nth(i).locator(BACKUP_ROW_LINK).first().getAttribute('href');
    if (href) return href.replace(/^\/backups\//, '').split('/')[0];
  }
  return null;
}

// waitForBackupStatus polls the list (reloading while the backup is still
// creating) until it reaches a terminal state. Bounded to ~120s.
async function waitForBackupStatus(page: Page, id: string): Promise<'ready' | 'failed'> {
  await expect
    .poll(
      async () => {
        const row = rowForBackup(page, id);
        if ((await row.count()) > 0) {
          if ((await row.getByText('ready', { exact: true }).count()) > 0) return 'ready';
          if ((await row.getByText('failed', { exact: true }).count()) > 0) return 'failed';
        }
        await page.reload();
        await page.waitForLoadState('domcontentloaded');
        return 'creating';
      },
      { timeout: 120_000, intervals: [3_000] },
    )
    .toMatch(/^(ready|failed)$/);

  return (await rowForBackup(page, id).getByText('ready', { exact: true }).count()) > 0
    ? 'ready'
    : 'failed';
}

test.describe('Backup details + list cleanup', () => {
  test('drops the checksums column and links each row to its details page', async ({ page }) => {
    // The list no longer carries the Checksums column or the redundant
    // plain-text type fallback; the `full` type badge stays.
    await expect(page.getByRole('columnheader', { name: /checksum/i })).toHaveCount(0);
    // The removed meta-line fallback rendered the exact string "full backup";
    // an exact match avoids the delete dialog's breadcrumb sentence
    // ("removes the full backup of …"), which a loose regex would match.
    await expect(page.getByText('full backup', { exact: true })).toHaveCount(0);

    // Self-heal an empty project: the row-link assertions below need a row.
    let createdId: string | null = null;
    try {
      if ((await page.locator(BACKUP_ROW_LINK).count()) === 0) {
        createdId = await startBackupViaForm(page);
      }

      await expect(
        page.locator('tbody tr').first().getByText('full', { exact: true }),
      ).toBeVisible();

      const rowLinks = page.locator(BACKUP_ROW_LINK);
      expect(await rowLinks.count()).toBeGreaterThan(0);
      await expect(rowLinks.first()).toBeVisible();
      await rowLinks.first().click();

      await expect(page).toHaveURL(/\/backups\/[0-9a-fA-F-]{36}$/);
      await expect(page.getByRole('heading', { name: 'Overview', exact: true })).toBeVisible();
      await expect(page.getByRole('heading', { name: 'Contents', exact: true })).toBeVisible();
    } finally {
      if (createdId) {
        await page.request.post(`/backups/${createdId}/delete`).catch(() => {});
      }
    }
  });

  test('renders the details sections, settings and ready-state integrity', async ({ page }) => {
    let createdId: string | null = null;
    try {
      let id = await readyBackupId(page);
      if (!id) {
        // No ready backup yet: create one through the real form and wait for it.
        createdId = await startBackupViaForm(page);
        id = createdId;
        const status = await waitForBackupStatus(page, id);
        if (status !== 'ready') test.skip(true, `created backup ${id} ended as ${status}`);
      }

      await page.goto(`/backups/${id}`);
      await expectAppPage(page, /Backup/);

      for (const heading of ['Contents', 'Settings', 'Archive format', 'Integrity']) {
        await expect(section(page, heading)).toBeVisible();
      }

      // Contents: the graphObjects stat is labelled "Objects".
      await expect(
        section(page, 'Contents').getByText('Objects', { exact: true }),
      ).toBeVisible();

      // Settings: Documents is always captured → an Included badge on its row.
      const docsRow = section(page, 'Settings')
        .getByText('Documents', { exact: true })
        .locator('..');
      await expect(docsRow.getByText('Included', { exact: true })).toBeVisible();

      // Archive format documents the manifest.
      await expect(
        section(page, 'Archive format').getByText(/manifest\.json/).first(),
      ).toBeVisible();

      // Integrity: a ready backup presents its full Manifest checksum.
      await expect(page.getByText('Manifest checksum', { exact: true })).toBeVisible();

      // Header offers Download only for ready backups.
      await expect(page.locator('a[href$="/download"]')).toHaveCount(1);
    } finally {
      if (createdId) {
        await page.request.post(`/backups/${createdId}/delete`).catch(() => {});
      }
    }
  });

  test('renders the error state for a missing backup instead of crashing', async ({ page }) => {
    await page.goto('/backups/00000000-0000-0000-0000-000000000000');
    await expect(page).not.toHaveURL(/\/auth\/login/);
    await expect(
      page.getByRole('heading', { name: 'Backup unavailable', exact: true }),
    ).toBeVisible();
  });
});
