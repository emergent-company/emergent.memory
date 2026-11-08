import { test, expect } from '@playwright/test';

// Hermetic (no-auth, mock-backed) coverage for the project Credentials surface:
// saved, reusable credentials (personal access tokens) and the OAuth apps table.
// Runs against the dev-mode mock harness (see datasources.config.ts): every
// memory call is answered by mock-memory.mjs, so the table and modal flows are
// deterministic.
//
// Routes under test (gateway, datasources_ui.go):
//   /settings/credentials                    saved credentials + OAuth apps
//   /settings/credentials/:providerType      project-admin OAuth app editor
//   /settings/oauth-clients                  retired URL → credentials page
//
// The mock user is a project admin, so the editor links render.

test.describe('project credentials', () => {
  test('renders the saved credentials table above the OAuth apps', async ({ page }) => {
    await page.goto('/settings/credentials');

    const saved = page.getByTestId('ds-saved-credentials-table');
    await expect(saved).toBeVisible();
    await expect(saved).toContainText('E2E GitHub token');
    await expect(saved).toContainText('GitHub');
    await expect(page.getByTestId('ds-saved-credential-add')).toBeVisible();

    // Saved credentials are the reusable, per-project tokens; the OAuth apps
    // table below them is a separate section, not the same list.
    await expect(page.getByTestId('ds-oauth-apps')).toBeVisible();
    await expect(page.getByRole('heading', { name: 'OAuth apps' })).toBeVisible();
    await expect(page.getByTestId('ds-credentials-table')).toBeVisible();
    const savedBox = await saved.boundingBox();
    const oauthBox = await page.getByTestId('ds-credentials-table').boundingBox();
    expect(savedBox && oauthBox && savedBox.y < oauthBox.y).toBe(true);
  });

  test('renders the OAuth credentials table with vendor-level app labels', async ({ page }) => {
    await page.goto('/settings/credentials');

    await expect(page.getByTestId('ds-credentials-table')).toBeVisible();

    // Vendor-level labels, with the data source shown separately.
    const driveRows = page.getByTestId('ds-credential-row').filter({ hasText: 'Google Drive' });
    await expect(driveRows.first().getByText('Google OAuth app')).toBeVisible();
    await expect(page.getByTestId('ds-credential-row').filter({ hasText: 'GitHub' }).first().getByText('GitHub OAuth app')).toBeVisible();

    // One project row per provider. Organization rows are added only for a
    // provider whose org app is configured, so they never double the table.
    await expect(driveRows).toHaveCount(1);
    await expect(driveRows.filter({ hasText: 'Project' })).toHaveCount(1);
    await expect(
      page.getByTestId('ds-credential-row').filter({ hasText: 'GitHub' }).filter({ hasText: 'Project' }),
    ).toHaveCount(1);

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

  test('adds, renames, and deletes a saved credential through the shared modal', async ({ page }) => {
    const name = `e2e-cred-${Date.now()}`;
    await page.goto('/settings/credentials');

    // Create.
    await page.getByTestId('ds-saved-credential-add').click();
    const modal = page.getByTestId('ds-saved-credential-modal');
    await expect(modal).toBeVisible();
    // Opened from the page (not a provider-specific form): the provider is chosen.
    await expect(page.getByTestId('ds-saved-credential-provider')).toBeVisible();
    await page.getByTestId('ds-saved-credential-provider').selectOption('github');
    await page.getByTestId('ds-saved-credential-name').fill(name);
    await page.getByTestId('ds-saved-credential-field-access_token').fill('ghp_modal_token');
    await page.getByTestId('ds-saved-credential-submit').click();

    await expect(page).toHaveURL(/\/settings\/credentials\?savedCredential=cred-/);
    await expect(page.getByText('Credential saved.')).toBeVisible();
    const row = page.getByTestId('ds-saved-credential-row').filter({ hasText: name });
    await expect(row).toBeVisible();
    // The submitted secret is never echoed back.
    expect(await page.content()).not.toContain('ghp_modal_token');

    // Rename through the same modal, prefilled and provider-fixed.
    await row.getByTestId('ds-saved-credential-edit').click();
    await expect(modal).toBeVisible();
    await expect(page.getByTestId('ds-saved-credential-name')).toHaveValue(name);
    // The provider is prefilled and locked while editing.
    await expect(page.getByTestId('ds-saved-credential-provider')).toHaveValue('github');
    await expect(page.getByTestId('ds-saved-credential-provider')).toBeDisabled();
    const renamed = `${name}-renamed`;
    await page.getByTestId('ds-saved-credential-name').fill(renamed);
    await page.getByTestId('ds-saved-credential-submit').click();

    await expect(page).toHaveURL(/\/settings\/credentials\?savedCredentialUpdated=1/);
    await expect(page.getByText('Credential updated.')).toBeVisible();
    const renamedRow = page.getByTestId('ds-saved-credential-row').filter({ hasText: renamed });
    await expect(renamedRow).toBeVisible();

    // Delete the (unused) credential (hx-confirm, so accept the dialog).
    page.once('dialog', (dialog) => dialog.accept());
    await renamedRow.getByTestId('ds-saved-credential-delete').click();
    await expect(page).toHaveURL(/\/settings\/credentials\?savedCredentialDeleted=1/);
    await expect(page.getByText('Credential deleted.')).toBeVisible();
    await expect(page.getByTestId('ds-saved-credential-row').filter({ hasText: renamed })).toHaveCount(0);
  });

  test('refuses to delete a credential a source still uses', async ({ page }) => {
    const name = `e2e-cred-in-use-${Date.now()}`;
    await page.goto('/settings/credentials');

    // Create the credential through the modal, then point a source at it.
    await page.getByTestId('ds-saved-credential-add').click();
    await page.getByTestId('ds-saved-credential-provider').selectOption('github');
    await page.getByTestId('ds-saved-credential-name').fill(name);
    await page.getByTestId('ds-saved-credential-field-access_token').fill('ghp_in_use');
    await page.getByTestId('ds-saved-credential-submit').click();
    await expect(page).toHaveURL(/savedCredential=cred-/);
    const credentialID = page.url().match(/savedCredential=(cred-[^&]+)/)?.[1] ?? '';
    expect(credentialID, 'the create redirect should name the new credential').not.toBe('');

    const source = await page.request.post('/api/data-source-integrations', {
      data: {
        name: `e2e-github-${Date.now()}`,
        providerType: 'github',
        sourceType: 'repo',
        credentialId: credentialID,
        config: { repo: 'acme/private-docs' },
      },
    });
    expect(source.status(), await source.text()).toBe(201);
    const src = (await source.json()) as { id: string };

    try {
      await page.goto('/settings/credentials');
      const row = page.getByTestId('ds-saved-credential-row').filter({ hasText: name });
      await expect(row).toContainText('1 source');
      page.once('dialog', (dialog) => dialog.accept());
      await row.getByTestId('ds-saved-credential-delete').click();

      // The server answers 409; the page surfaces its message and keeps the row.
      await expect(page.getByText(/credential is used by 1 data source/)).toBeVisible();
      await expect(page.getByTestId('ds-saved-credential-row').filter({ hasText: name })).toBeVisible();

      // Once the source is gone the credential can be deleted.
      await page.request.post(`/data-sources/${src.id}/delete`);
      await page.goto('/settings/credentials');
      page.once('dialog', (dialog) => dialog.accept());
      await page.getByTestId('ds-saved-credential-row').filter({ hasText: name }).getByTestId('ds-saved-credential-delete').click();
      await expect(page).toHaveURL(/savedCredentialDeleted=1/);
    } finally {
      await page.request.post(`/data-sources/${src.id}/delete`);
    }
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

  test('organization rows appear only once an org app is configured', async ({ page }) => {
    // Gmail is used here because no other spec configures an org app for it, so
    // the row appears and disappears with this test alone.
    await page.goto('/settings/credentials');
    const gmailOrgRows = page.getByTestId('ds-credential-row').filter({ hasText: 'Gmail' }).filter({ hasText: 'Organization' });
    await expect(gmailOrgRows).toHaveCount(0);

    // The org settings page is a table; the row opens the provider's editor.
    await page.goto('/orgs/org-1/settings/oauth-clients');
    await expect(page.getByTestId('ds-oauth-apps-table')).toBeVisible();
    const gmailRow = page.getByTestId('ds-oauth-app-row').filter({ hasText: 'Gmail' });
    await expect(gmailRow).toContainText('Not configured');
    await gmailRow.getByTestId('ds-oauth-app-manage').click();
    await expect(page).toHaveURL(/\/o\/org-1\/settings\/oauth-clients\/gmail$/);

    // The editor is the shared card, not a field on the table page.
    await expect(page.getByTestId('ds-oauth-client-detail')).toBeVisible();
    await expect(page.getByTestId('ds-field-client_secret')).toHaveAttribute('type', 'password');
    await page.getByTestId('ds-field-client_id').fill('e2e-gmail-org-client-id');
    await page.getByTestId('ds-field-client_secret').fill('super-secret-e2e-value');
    await page.getByTestId('ds-oauth-client-form').getByRole('button', { name: 'Save' }).click();

    // PRG returns to the table with the flash and the row now configured.
    await expect(page).toHaveURL(/oauth-clients\?saved=1/);
    await expect(page.getByText('OAuth client saved.')).toBeVisible();
    await expect(page.getByTestId('ds-oauth-app-row').filter({ hasText: 'Gmail' })).toContainText('Configured');

    // The project table gains the organization row, marked In use because the
    // project has no app of its own for Gmail.
    await page.goto('/settings/credentials');
    const orgRow = page.getByTestId('ds-credential-row').filter({ hasText: 'Gmail' }).filter({ hasText: 'Organization' });
    await expect(orgRow).toHaveCount(1);
    await expect(orgRow.getByTestId('ds-credential-in-use')).toBeVisible();
    await expect(orgRow.getByTestId('ds-credential-client-id')).toHaveAttribute('title', 'e2e-gmail-org-client-id');

    // Removing the org app drops the row again. Wait for the delete PRG before
    // navigating, so the assertion cannot race the removal.
    await page.goto('/orgs/org-1/settings/oauth-clients/gmail');
    await page.getByTestId('ds-oauth-client-delete').click();
    await expect(page).toHaveURL(/oauth-clients\?deleted=1/);
    await page.goto('/settings/credentials');
    await expect(page.getByTestId('ds-credential-row').filter({ hasText: 'Gmail' }).filter({ hasText: 'Organization' })).toHaveCount(0);
  });

  test('the retired OAuth-clients URL redirects to credentials', async ({ page }) => {
    await page.goto('/settings/oauth-clients');
    await expect(page).toHaveURL(/\/settings\/credentials$/);
    await expect(page.getByTestId('ds-credentials-table')).toBeVisible();
  });
});
