import { test, expect } from '@playwright/test';

// Hermetic (no-auth, mock-backed) coverage for the project Credentials surface.
// Runs against the dev-mode mock harness (see datasources.config.ts): every
// memory call is answered by mock-memory.mjs, so the table and editor flows are
// deterministic.
//
// Routes under test (gateway, datasources_ui.go):
//   /settings/credentials                    member-readable credentials table
//   /settings/credentials/:providerType      project-admin editor
//   /settings/oauth-clients                  retired URL → credentials table
//
// The mock user is a project admin, so the editor links render.

test.describe('project credentials', () => {
  test('renders the credentials table with vendor-level app labels', async ({ page }) => {
    await page.goto('/settings/credentials');

    await expect(page.getByTestId('ds-credentials-table')).toBeVisible();

    // Vendor-level labels, with the data source shown separately.
    const driveRows = page.getByTestId('ds-credential-row').filter({ hasText: 'Google Drive' });
    await expect(driveRows.first().getByText('Google OAuth app')).toBeVisible();
    await expect(page.getByTestId('ds-credential-row').filter({ hasText: 'GitHub' }).first().getByText('GitHub OAuth app')).toBeVisible();

    // One project row and one organization row per provider.
    await expect(driveRows).toHaveCount(2);
    await expect(driveRows.filter({ hasText: 'Project' })).toHaveCount(1);
    await expect(driveRows.filter({ hasText: 'Organization' })).toHaveCount(1);

    // Admins get an editor link on the project row.
    await expect(driveRows.filter({ hasText: 'Project' }).getByTestId('ds-credential-manage')).toHaveAttribute(
      'href',
      '/settings/credentials/google_drive',
    );

    // Credential configuration never appears on the connections table.
    await page.goto('/data-sources');
    await expect(page.getByTestId('ds-credentials-table')).toHaveCount(0);
    await expect(page.getByTestId('ds-source-table')).toBeVisible();
  });

  test('saves the project OAuth app and never echoes the secret', async ({ page }) => {
    await page.goto('/settings/credentials/google_drive');

    // The editor reuses the shared OAuth-client card.
    await expect(page.getByTestId('ds-credential-detail')).toBeVisible();
    await expect(page.getByTestId('ds-oauth-client-form')).toBeVisible();

    const secret = page.getByTestId('ds-field-client_secret').first();
    await expect(secret).toHaveAttribute('type', 'password');
    await expect(secret).toHaveValue('');

    await page.getByTestId('ds-field-client_id').first().fill('e2e-project-client-id');
    await secret.fill('super-secret-e2e-value');
    await page.getByTestId('ds-oauth-client-form').getByRole('button', { name: 'Save' }).click();

    await expect(page).toHaveURL(/\/settings\/credentials\/google_drive\?saved=1/);
    await expect(page.getByText('OAuth app saved.')).toBeVisible();

    // The submitted secret is never echoed back into the page.
    expect(await page.content()).not.toContain('super-secret-e2e-value');
    await expect(page.getByTestId('ds-field-client_secret').first()).toHaveValue('');

    // The table now shows the project app as the one in use.
    await page.goto('/settings/credentials');
    const projectRow = page.getByTestId('ds-credential-row').filter({ hasText: 'Google Drive' }).filter({ hasText: 'Project' });
    await expect(projectRow.getByTestId('ds-credential-client-id')).toHaveAttribute('title', 'e2e-project-client-id');
    await expect(projectRow.getByTestId('ds-credential-in-use')).toBeVisible();
  });

  test('the retired OAuth-clients URL redirects to credentials', async ({ page }) => {
    await page.goto('/settings/oauth-clients');
    await expect(page).toHaveURL(/\/settings\/credentials$/);
    await expect(page.getByTestId('ds-credentials-table')).toBeVisible();
  });
});
