import { test, expect } from '@playwright/test';
import { fillProviderForm, saveProviderForm } from '../../helpers/providers';

test('provider form renders and surfaces the save error', async ({ page }) => {
  await page.goto('/settings/providers/new');

  // Add-provider form renders with the provider select, API-key field, and save.
  await expect(page.locator('#provider-type')).toBeVisible();
  await expect(page.locator('input[name="api_key"]')).toBeVisible();
  await expect(page.locator('#provider-save-btn')).toBeVisible();

  // The dev memory backend rejects the save until the deepseek model catalog is
  // synced ("no models in catalog for provider deepseek"). Assert the UI
  // surfaces that as the "Couldn't save provider" error modal.
  await fillProviderForm(page, 'deepseek', 'sk-e2e-test-key');
  expect(await saveProviderForm(page)).toBe('error');
  await expect(page.getByText(/couldn't save provider|could not save provider/i).first()).toBeVisible();
});
