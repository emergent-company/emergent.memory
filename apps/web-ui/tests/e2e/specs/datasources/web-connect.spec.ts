import { test, expect } from '@playwright/test';
import { createWebSource, deleteDriveSource } from '../../helpers/datasources';

// Web connect is config-only: the provider advertises no credentials method and
// declares no credential fields, so its UI renders the crawl-scope config form
// (start URLs, filters, limits) with no pasted-credential surface. Runs against
// the dev-mode mock harness, so no website or network is involved.

test.describe('Connect a Web source', () => {
  test('renders the schema-driven config-only form with no credentials panel', async ({ page }) => {
    await page.goto('/data-sources/new?providerType=web&method=config');
    await expect(page.getByTestId('ds-connect-form')).toBeVisible();
    await expect(page.getByTestId('ds-field-start_urls')).toBeVisible();
    await expect(page.getByTestId('ds-field-include_globs')).toBeVisible();
    await expect(page.getByTestId('ds-field-exclude_globs')).toBeVisible();
    await expect(page.getByTestId('ds-field-max_pages')).toBeVisible();
    await expect(page.getByTestId('ds-field-max_depth')).toBeVisible();
    // Config-only: no write-only credential input is rendered.
    await expect(page.locator('input[type="password"]')).toHaveCount(0);
  });

  test('tests the connection against Web, not Drive', async ({ page }) => {
    await page.goto('/data-sources/new?providerType=web&method=config');
    await page.getByTestId('ds-field-start_urls').fill('https://docs.example.com');
    await page.getByTestId('ds-test-config').click();
    await expect(page.getByTestId('ds-test-result')).toContainText('Connected to Web.');
  });

  test('connects the source and lands on its detail page', async ({ page }) => {
    const src = await createWebSource(page, {
      name: `e2e-web-${Date.now()}`,
      startUrls: ['https://docs.example.com', 'https://blog.example.com'],
    });
    try {
      await expect(page.getByRole('heading', { name: src.name })).toBeVisible();
      await expect(page.getByTestId('ds-trigger-sync')).toBeVisible();
      await expect(page.getByTestId('ds-test-connection')).toBeVisible();
      // The start URL list round-trips through the stored (non-secret) config.
      await expect(page.getByTestId('ds-field-start_urls')).toHaveValue(
        'https://docs.example.com\nhttps://blog.example.com',
      );
      // The optional filters and limits are present but unfilled.
      await expect(page.getByTestId('ds-field-include_globs')).toHaveValue('');
      await expect(page.getByTestId('ds-field-max_pages')).toHaveValue('');
      // No credential material is rendered for a config-only provider.
      await expect(page.locator('input[type="password"]')).toHaveCount(0);
    } finally {
      await deleteDriveSource(page, src.id);
    }
  });

  test('rejects a Web config without a start URL on the same provider form', async ({ page }) => {
    await page.goto('/data-sources/new?providerType=web&method=config');
    await page.getByTestId('ds-field-name').fill(`e2e-web-${Date.now()}`);
    await page.getByRole('button', { name: 'Connect', exact: true }).click();

    await expect(page).toHaveURL(/providerType=web/);
    await expect(page).toHaveURL(/start_urls/);
    await expect(page.getByTestId('ds-connect-form')).toBeVisible();
  });
});
