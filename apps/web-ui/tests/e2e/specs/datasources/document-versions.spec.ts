import { test, expect } from '@playwright/test';

// Document version history (gateway /documents/:id/versions) — the observable
// outcome of re-syncing a source document. The mock seeds `doc-report` (two
// active versions: a changed document re-synced) and `doc-removed` (the
// deleted_from_source terminal state).

test.describe('Document version history', () => {
  test('renders every retained version, newest first', async ({ page }) => {
    await page.goto('/documents/doc-report/versions');
    await expect(page.getByTestId('document-versions')).toBeVisible();

    const rows = page.getByTestId('document-version-row');
    await expect(rows).toHaveCount(2);
    // Newest first: v2 (rev-2) tops the history, v1 (rev-1) below it.
    await expect(rows.nth(0)).toContainText('v2');
    await expect(rows.nth(0)).toContainText('rev-2');
    await expect(rows.nth(1)).toContainText('v1');
    await expect(rows.nth(1)).toContainText('rev-1');
    // Both versions are active (a normal re-sync, not a removal).
    await expect(page.getByText('Deleted from source')).toHaveCount(0);
    // The mock seeds a sourceRevisionId on every version, so the
    // "no source revision" empty-revision slot is covered by unit tests
    // (datasources_ui_test.go) rather than here.
  });

  test('shows the deleted_from_source terminal state', async ({ page }) => {
    await page.goto('/documents/doc-removed/versions');
    await expect(page.getByTestId('document-versions')).toBeVisible();

    const rows = page.getByTestId('document-version-row');
    await expect(rows).toHaveCount(2);
    await expect(rows.nth(0)).toContainText('Deleted from source');
    await expect(rows.nth(0).getByTestId('ds-version-deleted-at')).toBeVisible();
  });
});
