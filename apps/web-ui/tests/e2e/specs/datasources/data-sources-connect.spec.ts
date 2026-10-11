import { test, expect } from '@playwright/test';
import { createDriveSource, deleteDriveSource } from '../../helpers/datasources';

// Connect / configure a Drive source (gateway /data-sources/new), driven by the
// provider's config schema. Runs against the dev-mode mock harness.

test.describe('Connect a data source', () => {
  test('renders the schema-driven connect form', async ({ page }) => {
    await page.goto('/data-sources/new');
    await expect(page.getByTestId('ds-connect-form')).toBeVisible();
    await expect(page.getByTestId('ds-field-name')).toBeVisible();
    await expect(page.getByTestId('ds-field-sync-mode')).toBeVisible();
    await expect(page.getByTestId('ds-field-folder_id')).toBeVisible();
    await expect(page.getByTestId('ds-field-recursive')).toBeVisible();
    // Credentials are rendered write-only (password inputs).
    await expect(page.getByTestId('ds-field-refresh_token')).toBeVisible();
    await expect(page.getByTestId('ds-field-client_secret')).toBeVisible();
  });

  test('tests a valid config and reports success inline', async ({ page }) => {
    await page.goto('/data-sources/new');
    await page.getByTestId('ds-field-folder_id').fill('folder-ok');
    await page.getByTestId('ds-field-refresh_token').fill('e2e-refresh-token');
    await page.getByTestId('ds-test-config').click();
    await expect(page.getByTestId('ds-test-result')).toContainText('Connected to Google Drive.');
  });

  test('reports a failed config test inline', async ({ page }) => {
    await page.goto('/data-sources/new');
    await page.getByTestId('ds-field-folder_id').fill('folder-fail');
    await page.getByTestId('ds-field-refresh_token').fill('e2e-refresh-token');
    await page.getByTestId('ds-test-config').click();
    await expect(page.getByTestId('ds-test-result')).toContainText('Drive folder not found.');
  });

  test('rejects a config without a folder', async ({ page }) => {
    await page.goto('/data-sources/new');
    await page.getByTestId('ds-field-name').fill(`e2e-drive-${Date.now()}`);
    await page.getByTestId('ds-field-refresh_token').fill('e2e-refresh-token');
    await page.getByRole('button', { name: 'Connect', exact: true }).click();
    // The gateway rejects before reaching memory and re-renders the form.
    await expect(page).toHaveURL(/\/data-sources\/new/);
    await expect(page.getByTestId('ds-connect-form')).toBeVisible();
  });

  test('creates a source and lands on its detail page', async ({ page }) => {
    const src = await createDriveSource(page, {
      name: `e2e-drive-${Date.now()}`,
      folder: 'folder-ok',
    });
    try {
      await expect(page.getByTestId('ds-trigger-sync')).toBeVisible();
      await expect(page.getByTestId('ds-test-connection')).toBeVisible();
      await expect(page.getByTestId('ds-sync-history')).toBeVisible();
      await expect(page.getByTestId('ds-sync-empty')).toBeVisible();
      await expect(page.getByRole('heading', { name: src.name })).toBeVisible();
    } finally {
      await deleteDriveSource(page, src.id);
    }
  });
});
