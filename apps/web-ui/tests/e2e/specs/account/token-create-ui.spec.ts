import { test, expect } from '@playwright/test';

test('creates a token via the form', async ({ page }) => {
  await page.goto('/settings/tokens/new');
  await page.locator('#api-token-name').fill(`e2e-token-${Date.now()}`);
  // At least one scope is required.
  await page.locator('input[name="scopes"]').first().check();
  await page.getByRole('button', { name: 'Create token' }).click();
  // The one-shot reveal panel shows the plaintext secret.
  await expect(page.locator('#api-token-secret')).toBeVisible();
});
