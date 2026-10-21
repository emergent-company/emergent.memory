import { test, expect } from '@playwright/test';
import { createDriveSource, deleteDriveSource } from '../../helpers/datasources';

// Drive connect is OAuth-only: the provider advertises no credentials method and
// declares no credential fields, so its UI never renders a pasted-credential
// surface. These specs pin that contract plus the schema-driven detail/edit
// form; the connect flow itself lives in oauth-connect.spec.ts. Runs against the
// dev-mode mock harness.

test.describe('Drive data source', () => {
  test('is OAuth-only: the credentials method renders no pasted-credential form', async ({ page }) => {
    // The provider does not advertise credentials, so a ?method=credentials
    // hint changes nothing: the page shows the OAuth panel, never a form.
    await page.goto('/data-sources/new?providerType=google_drive&method=credentials');
    await expect(page.getByTestId('ds-oauth-connect')).toBeVisible();
    await expect(page.getByTestId('ds-connect-form')).toHaveCount(0);
    await expect(page.locator('input[type="password"]')).toHaveCount(0);
  });

  test('mirrors the provider: the Drive create API needs a folder and a credential', async ({ page }) => {
    // The mock mirrors the server's ExtractCredentials: a folder alone is not a
    // connectable Drive source; the connect flow (or a legacy API client)
    // supplies the refresh-token credential. Only rejection + message are
    // asserted: the gateway maps memory's 422 onto its own error status.
    const folderOnly = await page.request.post('/api/data-source-integrations', {
      data: {
        name: `e2e-drive-${Date.now()}`,
        providerType: 'google_drive',
        sourceType: 'drive',
        config: { folder_id: 'folder-ok' },
      },
    });
    expect(folderOnly.ok(), await folderOnly.text()).toBe(false);
    expect(await folderOnly.text()).toContain('config.refresh_token is required');

    const folderless = await page.request.post('/api/data-source-integrations', {
      data: {
        name: `e2e-drive-${Date.now()}`,
        providerType: 'google_drive',
        sourceType: 'drive',
        config: { refresh_token: 'e2e-refresh-token' },
      },
    });
    expect(folderless.ok(), await folderless.text()).toBe(false);
    expect(await folderless.text()).toContain('config.folder_id is required');
  });

  test('creates a source and renders its detail page', async ({ page }) => {
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
      // The edit form renders the provider's config contract: the stored
      // folder round-trips and the optional scope fields are all present.
      await expect(page.getByTestId('ds-edit-form')).toBeVisible();
      await expect(page.getByTestId('ds-field-sync-mode')).toBeVisible();
      await expect(page.getByTestId('ds-field-folder_id')).toHaveValue('folder-ok');
      await expect(page.getByTestId('ds-field-shared_drive_id')).toBeVisible();
      await expect(page.getByTestId('ds-field-mime_types')).toBeVisible();
      await expect(page.getByTestId('ds-field-recursive')).toBeVisible();
      // Drive has no credential fields, so there is no write-only secret input.
      await expect(page.getByTestId('ds-field-refresh_token')).toHaveCount(0);
    } finally {
      await deleteDriveSource(page, src.id);
    }
  });

  test('edit form preserves stored config on save', async ({ page }) => {
    const src = await createDriveSource(page, {
      name: `e2e-drive-${Date.now()}`,
      folder: 'folder-ok',
    });
    try {
      // Change only the name; the prefilled folder is resubmitted untouched.
      await page.getByTestId('ds-field-name').fill(`${src.name}-renamed`);
      await page.getByRole('button', { name: 'Save changes' }).click();
      await page.waitForURL(/updated=1/);
      await expect(page.getByRole('heading', { name: `${src.name}-renamed` })).toBeVisible();
      // The folder survives the round-trip (no data loss).
      await expect(page.getByTestId('ds-field-folder_id')).toHaveValue('folder-ok');
      // The stored refresh-token blob is never re-rendered or blank-resubmitted.
      await expect(page.getByTestId('ds-field-refresh_token')).toHaveCount(0);
    } finally {
      await deleteDriveSource(page, src.id);
    }
  });
});
