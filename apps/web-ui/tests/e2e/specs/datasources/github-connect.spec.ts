import { test, expect } from '@playwright/test';
import {
  SEEDED_GITHUB_CREDENTIAL_ID,
  createGitHubSource,
  deleteDriveSource,
  selectSavedCredential,
} from '../../helpers/datasources';

// GitHub credentials connect, driven by the provider schema: the source uses a
// saved personal-access-token credential from the project (created on the
// Credentials page or inline here), while the repository and ref fields offer
// suggestions from credential-scoped listing endpoints instead of requiring
// owner/name and a branch to be typed from memory. Runs against the dev-mode
// mock harness, so no GitHub token or network is involved.

test.describe('Connect a GitHub source', () => {
  test('renders the schema-driven form with repo and ref pickers', async ({ page }) => {
    await page.goto('/data-sources/new?providerType=github&method=credentials');
    await expect(page.getByTestId('ds-connect-form')).toBeVisible();
    await expect(page.getByTestId('ds-field-repo')).toBeVisible();
    await expect(page.getByTestId('ds-field-ref')).toBeVisible();
    // The token is a saved credential, chosen from the picker — never typed here.
    await expect(page.getByTestId('ds-field-credential')).toBeVisible();
    await expect(page.getByTestId('ds-credential-create')).toBeVisible();
    // No inline token input on the form itself: the shared add-credential modal
    // owns the paste field.
    await expect(page.getByTestId('ds-field-access_token')).toHaveCount(0);
    await expect(page.getByTestId('ds-connect-form').locator('input[name="access_token"]')).toHaveCount(0);
    await expect(page.getByTestId('ds-load-repos')).toBeVisible();
    await expect(page.getByTestId('ds-load-refs')).toBeVisible();
  });

  test('loads repository suggestions with the selected credential', async ({ page }) => {
    await page.goto('/data-sources/new?providerType=github&method=credentials');
    await selectSavedCredential(page);
    await page.getByTestId('ds-load-repos').click();

    const options = page.locator('#ds-options-repo option');
    await expect(options).toHaveCount(2);
    await expect(page.locator('#ds-options-repo option[value="acme/private-docs"]')).toHaveCount(1);
    await expect(page.getByTestId('ds-picker-repo')).toContainText(/showing 2 suggestions/i);
  });

  test('loads branch and tag suggestions once a repository is entered', async ({ page }) => {
    await page.goto('/data-sources/new?providerType=github&method=credentials');
    await selectSavedCredential(page);
    await page.getByTestId('ds-field-repo').fill('acme/private-docs');
    await page.getByTestId('ds-load-refs').click();

    const options = page.locator('#ds-options-ref option');
    await expect(options).toHaveCount(3);
    await expect(page.locator('#ds-options-ref option[value="main"]')).toContainText('(branch)');
    await expect(page.locator('#ds-options-ref option[value="v1.0.0"]')).toContainText('(tag)');
  });

  test('a listing failure is inline and keeps the typed value', async ({ page }) => {
    await page.goto('/data-sources/new?providerType=github&method=credentials');
    await selectSavedCredential(page);
    await page.getByTestId('ds-field-repo').fill('acme/unknown');
    await page.getByTestId('ds-load-refs').click();

    await expect(page.getByTestId('ds-options-error')).toContainText('not found');
    await expect(page.getByTestId('ds-field-repo')).toHaveValue('acme/unknown');
    // The failure does not block the form.
    await expect(page.getByTestId('ds-connect-submit')).toBeEnabled();
  });

  test('tests the connection against GitHub, not Drive', async ({ page }) => {
    await page.goto('/data-sources/new?providerType=github&method=credentials');
    await selectSavedCredential(page);
    await page.getByTestId('ds-field-repo').fill('acme/private-docs');
    await page.getByTestId('ds-test-config').click();
    await expect(page.getByTestId('ds-test-result')).toContainText('Connected to GitHub.');
  });

  test('creates a credential inline and preselects it on the connect form', async ({ page }) => {
    await page.goto('/data-sources/new?providerType=github&method=credentials');
    await page.getByTestId('ds-credential-create').click();

    const modal = page.getByTestId('ds-saved-credential-modal');
    await expect(modal).toBeVisible();
    // Opened from a provider-specific form: the provider is fixed, not chosen.
    await expect(page.getByTestId('ds-saved-credential-provider')).toHaveCount(0);
    await expect(page.getByTestId('ds-saved-credential-provider-fixed')).toContainText('GitHub');

    await page.getByTestId('ds-saved-credential-name').fill('Inline PAT');
    await page.getByTestId('ds-saved-credential-field-access_token').fill('ghp_inline_token');
    await page.getByTestId('ds-saved-credential-submit').click();

    await page.waitForURL(/\/data-sources\/new\?providerType=github/);
    await expect(page).toHaveURL(/savedCredential=cred-/);
    // The just-created credential is selected and Connect is enabled.
    await expect(page.getByTestId('ds-field-credential')).not.toHaveValue('');
    await expect(page.locator('#credential_id option:checked')).toContainText('Inline PAT');
    await expect(page.getByTestId('ds-connect-submit')).toBeEnabled();
  });

  test('keeps Connect disabled until a credential is selected', async ({ page }) => {
    await page.goto('/data-sources/new?providerType=github&method=credentials');
    await page.getByTestId('ds-field-name').fill(`e2e-github-${Date.now()}`);
    await page.getByTestId('ds-field-repo').fill('acme/private-docs');

    const connect = page.getByTestId('ds-connect-submit');
    await expect(connect).toBeDisabled();

    await selectSavedCredential(page);
    await expect(connect).toBeEnabled();
  });

  test('connects the source and lands on its detail page', async ({ page }) => {
    const src = await createGitHubSource(page, {
      name: `e2e-github-${Date.now()}`,
      repo: 'acme/private-docs',
    });
    try {
      await expect(page.getByRole('heading', { name: src.name })).toBeVisible();
      await expect(page.getByTestId('ds-trigger-sync')).toBeVisible();
      await expect(page.getByTestId('ds-test-connection')).toBeVisible();
      // The repository round-trips through the stored (non-secret) config.
      await expect(page.getByTestId('ds-field-repo')).toHaveValue('acme/private-docs');
      // The settings card preselects the saved credential and never the secret.
      await expect(page.getByTestId('ds-field-credential')).toHaveValue(SEEDED_GITHUB_CREDENTIAL_ID);
      await expect(page.getByTestId('ds-edit-form').locator('input[name="access_token"]')).toHaveCount(0);
      // The old Drive-hardcoded path is gone.
      await expect(page.locator('body')).not.toContainText('config.folder_id is required');
    } finally {
      await deleteDriveSource(page, src.id);
    }
  });
});
