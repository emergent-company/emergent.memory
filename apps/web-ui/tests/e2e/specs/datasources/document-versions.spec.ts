import { test, expect } from '@playwright/test';

// Document version history (gateway /documents/:id/versions) — the observable
// outcome of re-syncing a source document. The mock seeds `doc-report` (two
// active versions: a changed document re-synced) and `doc-removed` (the
// deleted_from_source terminal state).

test.describe('Document version history', () => {
  test('renders every retained version, oldest first', async ({ page }) => {
    await page.goto('/documents/doc-report/versions');
    await expect(page.getByTestId('document-versions')).toBeVisible();

    const rows = page.getByTestId('document-version-row');
    await expect(rows).toHaveCount(2);
    await expect(rows.nth(0)).toContainText('v1');
    await expect(rows.nth(0)).toContainText('rev-1');
    await expect(rows.nth(1)).toContainText('v2');
    await expect(rows.nth(1)).toContainText('rev-2');
    // Both versions are active (a normal re-sync, not a removal).
    await expect(page.getByText('Deleted from source')).toHaveCount(0);
  });

  test('shows the deleted_from_source terminal state', async ({ page }) => {
    await page.goto('/documents/doc-removed/versions');
    await expect(page.getByTestId('document-versions')).toBeVisible();

    const rows = page.getByTestId('document-version-row');
    await expect(rows).toHaveCount(2);
    await expect(rows.nth(1)).toContainText('Deleted from source');
    await expect(rows.nth(1).getByTestId('ds-version-deleted-at')).toBeVisible();
  });
});
