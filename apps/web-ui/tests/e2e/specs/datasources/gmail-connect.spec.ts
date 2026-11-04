import { test, expect } from '@playwright/test';

// Gmail is OAuth-only and always authorizes exactly one scope, so its connect
// surface has no pasted-credential form and no scope picker. Runs against the
// dev-mode mock harness (see datasources.config.ts), which mirrors the server's
// provider catalog and config schema; no Google account or network is involved.
//
// The seeded source `s2` is a connected Gmail mailbox: it proves the gateway's
// fixed-scope table renders the granted scope instead of the amber "not
// reported" badge (the UI fix that shipped with the connector, #2219).

const gmailChoice = (page: import('@playwright/test').Page) =>
  page.getByTestId('ds-provider-choice').filter({ hasText: 'Gmail' });

test.describe('Connect a Gmail source', () => {
  test('the chooser offers Gmail as an OAuth-only provider', async ({ page }) => {
    await page.goto('/data-sources/new');

    await expect(page.getByTestId('ds-provider-chooser')).toBeVisible();
    await expect(gmailChoice(page)).toBeVisible();
    await expect(gmailChoice(page)).toContainText('Sync email threads from a Gmail mailbox');

    // One OAuth action, targeting the provider-generic OAuth start route.
    const oauth = gmailChoice(page).getByTestId('ds-oauth-connect');
    await expect(oauth).toBeVisible();
    await expect(oauth).toContainText('Connect with Gmail');
    await expect(oauth).toHaveAttribute(
      'href',
      '/data-sources/oauth/start?providerType=gmail',
    );

    // OAuth-only: no credential form and no config-only form on the card.
    await expect(gmailChoice(page).getByTestId('ds-credentials-connect')).toHaveCount(0);
    await expect(gmailChoice(page).getByTestId('ds-config-connect')).toHaveCount(0);
  });

  test('a connected mailbox shows its fixed read-only scope and no secret inputs', async ({ page }) => {
    await page.goto('/data-sources/s2');

    await expect(page.getByTestId('page-support-mailbox')).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Support Mailbox' })).toBeVisible();

    // The granted-scope badge comes from the gateway's fixed-scope table: Gmail
    // reports no strategy of its own, so an empty strategy must not fall back to
    // the "Not reported by the provider" warning.
    const card = page.getByTestId('ds-credentials-card');
    await expect(card).toBeVisible();
    await expect(card).toContainText('Read-only Gmail access (gmail.readonly)');
    await expect(card).not.toContainText('Not reported by the provider');

    // The non-secret scope config round-trips into the edit form.
    await expect(page.getByTestId('ds-field-query')).toHaveValue('in:inbox');

    // Credentials are OAuth-only and write-only: nothing to paste, nothing
    // rendered back.
    await expect(page.locator('input[type="password"]')).toHaveCount(0);
    await expect(page.getByTestId('ds-field-refresh_token')).toHaveCount(0);
    await expect(page.getByTestId('ds-field-client_secret')).toHaveCount(0);
  });
});
