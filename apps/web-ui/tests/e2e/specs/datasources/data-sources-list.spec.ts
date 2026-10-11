import { test, expect } from '@playwright/test';
import { createDriveSource, deleteDriveSource } from '../../helpers/datasources';

// Data Sources list surface (gateway /data-sources). Runs against the dev-mode
// mock harness (see datasources.config.ts). Serial + self-cleaning, so the
// source store is empty between tests.

test.describe('Data Sources list', () => {
  test('shows the empty state and the connect call to action', async ({ page }) => {
    await page.goto('/data-sources');
    await expect(page.getByTestId('ds-new-link')).toBeVisible();
    const rows = page.getByTestId('ds-source-row');
    if ((await rows.count()) === 0) {
      await expect(page.getByText('No data sources connected')).toBeVisible();
      await expect(page.getByRole('link', { name: /Connect a data source/ })).toBeVisible();
    }
  });

  test('lists a connected source with its provider and status', async ({ page }) => {
    const src = await createDriveSource(page, {
      name: `e2e-drive-${Date.now()}`,
      folder: 'folder-ok',
    });
    try {
      await page.goto('/data-sources');
      const row = page.getByTestId('ds-source-row').filter({ hasText: src.name });
      await expect(row).toBeVisible();
      await expect(row.getByText('Google Drive')).toBeVisible();
      await expect(row.getByText('Active', { exact: true })).toBeVisible();
      await expect(row.getByText('Never synced')).toBeVisible();

      // The row links into the source's detail page.
      await row.getByRole('link').click();
      await expect(page).toHaveURL(new RegExp(`/data-sources/${src.id}`));
      await expect(page.getByTestId('ds-trigger-sync')).toBeVisible();
    } finally {
      await deleteDriveSource(page, src.id);
    }
  });
});
