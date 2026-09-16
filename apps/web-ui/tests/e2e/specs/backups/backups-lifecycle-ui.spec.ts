import { test, expect, type Page, type Locator } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

// Backup lifecycle (gateway/backups.go + gateway/backups.templ):
//
//   POST /backups                create (Start backup) → PRG /backups?created=1
//   GET  /backups                list (row title links to the details page)
//   GET  /backups/:id            details page (header offers Download when ready)
//   GET  /backups/:id/download   302 to a pre-signed object-storage URL
//   POST /backups/:id/delete     row/detail confirm dialog → PRG /backups?deleted=1
//
// The existing `backups-details-ui.spec.ts` covers the list cleanup and the
// details sections; this spec covers the write lifecycle end to end: create
// through the real form, wait for the backup to reach a terminal status, open
// the details page, exercise the download route, delete it, and assert the row
// is gone from the list.
//
// Backups are project-scoped and shared by the bootstrap tenant, so every
// assertion targets the one id this run created (never list counts/ordering).
// A fresh backup may not reach `ready` promptly (or at all) when the dev Memory
// service has no archive storage configured; following the precedent in
// `backups-details-ui.spec.ts` / `document-extraction-ui.spec.ts`, the test
// skips with a reason instead of failing when the status does not become
// `ready`. Backups may also be feature-gated off entirely → the beforeEach
// skips on the "Backups unavailable" state.

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

// rowForBackup locates the list row whose title/preview link points at the id
// this run created.
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

// waitForBackupStatus polls the list (reloading while the backup is still
// creating) until the created id reaches a terminal state — or returns
// 'creating' when the bounded wait elapses, so the caller can skip rather than
// fail. The list itself auto-refreshes every few seconds while a backup is in
// flight; the explicit reload is a fallback and is best-effort because a
// concurrent auto-refresh can abort it.
async function waitForBackupStatus(
  page: Page,
  id: string,
  timeoutMs = 120_000,
): Promise<'ready' | 'failed' | 'creating'> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const row = rowForBackup(page, id);
    if ((await row.count().catch(() => 0)) > 0) {
      if ((await row.getByText('ready', { exact: true }).count()) > 0) return 'ready';
      if ((await row.getByText('failed', { exact: true }).count()) > 0) return 'failed';
    }
    await page.waitForTimeout(3_000);
    await page.reload().catch(() => undefined);
    await page.waitForLoadState('domcontentloaded').catch(() => undefined);
  }
  return 'creating';
}

test.describe('Backup lifecycle', () => {
  test('creates a backup, downloads it, deletes it, and removes it from the list', async ({
    page,
  }) => {
    // Generous budget: the create → terminal-status wait alone can take ~2 min.
    test.setTimeout(300_000);

    let id: string | null = null;
    try {
      // 1. CREATE through the real inline form → PRG back to the list.
      id = await startBackupViaForm(page);
      expect(id, 'create must produce a backup row id').toBeTruthy();

      // 2. WAIT for a terminal status. The whole download/delete flow below
      // needs a `ready` archive; anything else is an environment limitation.
      const status = await waitForBackupStatus(page, id);
      if (status !== 'ready') {
        test.skip(
          true,
          `created backup ${id} did not reach "ready" within 120s (status=${status}); ` +
            'the dev Memory service may not have archive storage configured, so the ' +
            'download/delete lifecycle cannot be asserted',
        );
        return;
      }

      // 3. DETAILS: the created backup's dedicated page renders its sections.
      await page.goto(`/backups/${id}`);
      await expectAppPage(page, /Backup/);
      await expect(page.getByRole('heading', { name: 'Overview', exact: true })).toBeVisible();
      await expect(page.getByRole('heading', { name: 'Contents', exact: true })).toBeVisible();

      // 4. DOWNLOAD: a ready backup offers exactly one Download link to the
      // download route. The handler answers 302 to a pre-signed object-storage
      // URL with a 1h expiry; assert that redirect through the same
      // authenticated session with redirects disabled so the headless browser
      // never chases the expiring URL off-origin.
      const downloadLink = page.getByRole('link', { name: 'Download', exact: true });
      await expect(downloadLink).toBeVisible();
      await expect(downloadLink).toHaveAttribute('href', `/backups/${id}/download`);

      const downloadResp = await page.request.get(`/backups/${id}/download`, {
        maxRedirects: 0,
      });
      expect(
        downloadResp.status(),
        `download route must 302 to a pre-signed URL, got ${downloadResp.status()}`,
      ).toBe(302);
      expect(
        downloadResp.headers()['location'] ?? '',
        'download 302 must carry a Location header',
      ).not.toBe('');

      // 5. DELETE through the details header: the Delete action opens the
      // shared confirm dialog (id backup-delete-<id>); confirming posts to
      // /backups/:id/delete and PRG-redirects to the list.
      await page.getByRole('button', { name: 'Delete', exact: true }).click();
      const dialog = page.locator(`#backup-delete-${id}`);
      await expect(dialog).toBeVisible();
      await dialog.getByRole('button', { name: 'Delete backup' }).click();
      await page.waitForURL(/\/backups\?deleted=1/, { timeout: 30_000 });
      await expectAppPage(page, /Backups/);

      // 6. RESULTING STATE: the created backup's links are gone from the list.
      await expect(page.locator(`a[href="/backups/${id}"]`)).toHaveCount(0);
      await expect(page.locator(`#backup-delete-${id}`)).toHaveCount(0);
    } finally {
      // Self-cleanup: never leave this run's backup behind, even when the UI
      // delete was interrupted (skip/assertion/auto-refresh).
      if (id) {
        await page.request.post(`/backups/${id}/delete`).catch(() => undefined);
      }
    }
  });
});
