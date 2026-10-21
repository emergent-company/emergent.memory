import { test, expect } from '@playwright/test';
import { createGitHubSource, deleteDriveSource } from '../../helpers/datasources';

// GitHub credentials connect, driven by the provider schema: the repository and
// ref fields offer suggestions from credential-scoped listing endpoints instead
// of requiring owner/name and a branch to be typed from memory. Runs against the
// dev-mode mock harness, so no GitHub token or network is involved.

test.describe('Connect a GitHub source', () => {
  test('renders the schema-driven form with repo and ref pickers', async ({ page }) => {
    await page.goto('/data-sources/new?providerType=github&method=credentials');
    await expect(page.getByTestId('ds-connect-form')).toBeVisible();
    await expect(page.getByTestId('ds-field-repo')).toBeVisible();
    await expect(page.getByTestId('ds-field-ref')).toBeVisible();
    // The token is a write-only credential input, rendered as a password.
    await expect(page.getByTestId('ds-field-access_token')).toHaveAttribute('type', 'password');
    await expect(page.getByTestId('ds-load-repos')).toBeVisible();
    await expect(page.getByTestId('ds-load-refs')).toBeVisible();
  });

  test('loads repository suggestions from the entered token', async ({ page }) => {
    await page.goto('/data-sources/new?providerType=github&method=credentials');
    await page.getByTestId('ds-field-access_token').fill('ghp_e2e_token');
    await page.getByTestId('ds-load-repos').click();

    const options = page.locator('#ds-options-repo option');
    await expect(options).toHaveCount(2);
    await expect(page.locator('#ds-options-repo option[value="acme/private-docs"]')).toHaveCount(1);
    await expect(page.getByTestId('ds-picker-repo')).toContainText(/showing 2 suggestions/i);
  });

  test('loads branch and tag suggestions once a repository is entered', async ({ page }) => {
    await page.goto('/data-sources/new?providerType=github&method=credentials');
    await page.getByTestId('ds-field-access_token').fill('ghp_e2e_token');
    await page.getByTestId('ds-field-repo').fill('acme/private-docs');
    await page.getByTestId('ds-load-refs').click();

    const options = page.locator('#ds-options-ref option');
    await expect(options).toHaveCount(3);
    await expect(page.locator('#ds-options-ref option[value="main"]')).toContainText('(branch)');
    await expect(page.locator('#ds-options-ref option[value="v1.0.0"]')).toContainText('(tag)');
  });

  test('a listing failure is inline and keeps the typed value', async ({ page }) => {
    await page.goto('/data-sources/new?providerType=github&method=credentials');
    await page.getByTestId('ds-field-access_token').fill('ghp_e2e_token');
    await page.getByTestId('ds-field-repo').fill('acme/unknown');
    await page.getByTestId('ds-load-refs').click();

    await expect(page.getByTestId('ds-options-error')).toContainText('not found');
    await expect(page.getByTestId('ds-field-repo')).toHaveValue('acme/unknown');
    // The failure does not block the form.
    await expect(page.getByRole('button', { name: 'Connect', exact: true })).toBeEnabled();
  });

  test('tests the connection against GitHub, not Drive', async ({ page }) => {
    await page.goto('/data-sources/new?providerType=github&method=credentials');
    await page.getByTestId('ds-field-access_token').fill('ghp_e2e_token');
    await page.getByTestId('ds-field-repo').fill('acme/private-docs');
    await page.getByTestId('ds-test-config').click();
    await expect(page.getByTestId('ds-test-result')).toContainText('Connected to GitHub.');
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
      // The token is write-only and never rendered back.
      await expect(page.getByTestId('ds-field-access_token')).toHaveValue('');
      // The old Drive-hardcoded path is gone.
      await expect(page.locator('body')).not.toContainText('config.folder_id is required');
    } finally {
      await deleteDriveSource(page, src.id);
    }
  });

  test('rejects a GitHub config without a token on the same provider form', async ({ page }) => {
    await page.goto('/data-sources/new?providerType=github&method=credentials');
    await page.getByTestId('ds-field-name').fill(`e2e-github-${Date.now()}`);
    await page.getByTestId('ds-field-repo').fill('acme/private-docs');
    await page.getByRole('button', { name: 'Connect', exact: true }).click();

    await expect(page).toHaveURL(/providerType=github/);
    await expect(page).toHaveURL(/access_token/);
    await expect(page.getByTestId('ds-connect-form')).toBeVisible();
  });
});
