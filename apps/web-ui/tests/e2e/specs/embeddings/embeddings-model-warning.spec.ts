import { test, expect } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

// Read-surface smoke test for the /embeddings "no embedding model" warning.
//
// The bootstrap tenant (see auth.setup.ts) is configured with a generative-only
// provider (or, in CI, no provider at all) and never a project embedding model,
// so the page deterministically shows the missing-model warning. The warning
// links to /settings/providers, where both the provider credential embedding
// model and the project default model are configured.
test.describe('Embeddings status page', () => {
  test('shows the no-embedding-model warning with a configure CTA', async ({ page }) => {
    await page.goto('/embeddings');
    await expectAppPage(page, /Embeddings/);

    // Main content root renders (session-auth page, no login redirect).
    await expect(page.getByTestId('page-embeddings')).toBeVisible();

    // Warning banner is present and names the missing model.
    const warning = page.getByTestId('embeddings-model-warning');
    await expect(warning).toBeVisible();
    await expect(warning).toContainText(/no embedding model is configured/i);

    // CTA points at the settings page where the model is configured.
    const cta = page.getByRole('link', { name: /Configure embedding model/ });
    await expect(cta).toBeVisible();
    await expect(cta).toHaveAttribute('href', '/settings/providers');
  });
});
