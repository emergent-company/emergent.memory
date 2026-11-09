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

  test('renders an empty OAuth apps list with an add action', async ({ page }) => {
    await page.goto('/settings/credentials');

    await expect(page.getByTestId('ds-oauth-apps')).toBeVisible();
    await expect(page.getByRole('heading', { name: 'OAuth apps' })).toBeVisible();
    await expect(page.getByTestId('ds-oauth-app-add')).toBeVisible();
    // No catalog rows are pre-rendered: the list starts empty.
    await expect(page.getByTestId('ds-credential-row')).toHaveCount(0);
    await expect(page.getByText('No OAuth apps yet')).toBeVisible();

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

  test('adds, edits, and deletes a named project OAuth app without echoing the secret', async ({ page }) => {
    const name = `e2e-google-${Date.now()}`;
    await page.goto('/settings/credentials/apps/new');

    await expect(page.getByTestId('ds-oauth-app-form')).toBeVisible();
    await page.getByTestId('ds-oauth-app-vendor').selectOption('google');
    await page.getByTestId('ds-oauth-app-name').fill(name);
    await page.getByTestId('ds-oauth-app-client-id').fill('e2e-project-client-id');
    await page.getByTestId('ds-oauth-app-client-secret').fill('super-secret-e2e-value');
    await page.getByTestId('ds-oauth-app-save').click();

    await expect(page).toHaveURL(/\/settings\/credentials\?saved=1$/);
    const row = page.getByTestId('ds-credential-row').filter({ hasText: name });
    await expect(row).toHaveCount(1);
    await expect(row.getByTestId('ds-credential-client-id')).toHaveAttribute('title', 'e2e-project-client-id');
    expect(await page.content()).not.toContain('super-secret-e2e-value');

    // Editing preserves the stored secret when the field is left blank.
    await row.getByTestId('ds-credential-manage').click();
    await expect(page.getByTestId('ds-oauth-app-form')).toBeVisible();
    await expect(page.getByTestId('ds-oauth-app-client-secret')).toHaveValue('');
    await page.getByTestId('ds-oauth-app-name').fill(name + '-renamed');
    await page.getByTestId('ds-oauth-app-save').click();
    await expect(page.getByTestId('ds-credential-row').filter({ hasText: name + '-renamed' })).toHaveCount(1);

    // Delete removes it from the list.
    await page.getByTestId('ds-credential-row').filter({ hasText: name + '-renamed' }).getByTestId('ds-credential-manage').click();
    page.once('dialog', (dialog) => dialog.accept());
    await page.getByTestId('ds-oauth-app-delete').click();
    await expect(page).toHaveURL(/\/settings\/credentials\?deleted=1$/);
    await expect(page.getByTestId('ds-credential-row').filter({ hasText: name + '-renamed' })).toHaveCount(0);
  });

  test('organization OAuth apps are added from the org settings page', async ({ page }) => {
    const name = `e2e-org-google-${Date.now()}`;
    await page.goto('/orgs/org-1/settings/oauth-clients');
    await expect(page.getByTestId('ds-oauth-app-add')).toBeVisible();
    await expect(page.getByTestId('ds-credentials-table')).toBeVisible();

    await page.getByTestId('ds-oauth-app-add').click();
    await expect(page).toHaveURL(/\/orgs\/org-1\/settings\/oauth-apps\/new$/);
    await page.getByTestId('ds-oauth-app-vendor').selectOption('google');
    await page.getByTestId('ds-oauth-app-name').fill(name);
    await page.getByTestId('ds-oauth-app-client-id').fill('e2e-org-client-id');
    await page.getByTestId('ds-oauth-app-client-secret').fill('super-secret-e2e-value');
    await page.getByTestId('ds-oauth-app-save').click();

    await expect(page).toHaveURL(/oauth-clients\?saved=1/);
    await expect(page.getByTestId('ds-credential-row').filter({ hasText: name })).toHaveCount(1);

    // Delete restores the empty list.
    await page.getByTestId('ds-credential-row').filter({ hasText: name }).getByTestId('ds-credential-manage').click();
    page.once('dialog', (dialog) => dialog.accept());
    await page.getByTestId('ds-oauth-app-delete').click();
    await expect(page).toHaveURL(/oauth-clients\?deleted=1/);
    await expect(page.getByTestId('ds-credential-row').filter({ hasText: name })).toHaveCount(0);
  });

  test('the retired OAuth-clients URL redirects to credentials', async ({ page }) => {
    await page.goto('/settings/oauth-clients');
    await expect(page).toHaveURL(/\/settings\/credentials$/);
    await expect(page.getByTestId('ds-credentials-table')).toBeVisible();
  });
});
