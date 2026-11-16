import { test, expect } from '../../fixtures';

// The proof that the reset actually happens (#633).
//
// The datasources project runs every spec file against ONE mock process
// (`datasources.config.ts`: `workers: 1`, `fullyParallel: false`), so state a
// test writes is still there for the next one. The `test` exported by
// fixtures.ts posts the mock's `/__mock/reset` before every test, restoring the
// Data Sources store to its documented seed (mock-memory.mjs).
//
// The first test below deliberately leaves a project OAuth app behind. The
// second asserts the credentials page is empty again — and that the seed was
// restored rather than everything wiped. Without the per-test reset the second
// test fails with the leaked row still listed (one `ds-credential-row`, no
// "No OAuth apps yet"), which is exactly the order-dependent failure the issue
// describes.

test.describe('mock store is reset between tests', () => {
  test('leaves a project OAuth app behind, on purpose', async ({ page }) => {
    const name = `e2e-reset-leak-${Date.now()}`;
    await page.goto('/settings/credentials/apps/new');
    await expect(page.getByTestId('ds-oauth-app-form')).toBeVisible();
    await page.getByTestId('ds-oauth-app-vendor').selectOption('google');
    await page.getByTestId('ds-oauth-app-name').fill(name);
    await page.getByTestId('ds-oauth-app-client-id').fill('e2e-reset-client-id');
    await page.getByTestId('ds-oauth-app-client-secret').fill('super-secret-e2e-value');
    await page.getByTestId('ds-oauth-app-save').click();

    await expect(page).toHaveURL(/\/settings\/credentials\?saved=1$/);
    // The app is in the store at the end of this test; nothing deletes it.
    await expect(page.getByTestId('ds-credential-row').filter({ hasText: name })).toHaveCount(1);
  });

  test('starts from the seed, not from the previous test’s residue', async ({ page }) => {
    await page.goto('/settings/credentials');

    // The leaked app is gone.
    await expect(page.getByRole('heading', { name: 'No OAuth apps yet' })).toBeVisible();
    await expect(page.getByTestId('ds-credential-row')).toHaveCount(0);
    // …because the store was restored, not emptied: the seeded reusable
    // credential the connect specs select is still listed.
    await expect(
      page.getByTestId('ds-saved-credential-row').filter({ hasText: 'E2E GitHub token' }),
    ).toHaveCount(1);
  });
});
