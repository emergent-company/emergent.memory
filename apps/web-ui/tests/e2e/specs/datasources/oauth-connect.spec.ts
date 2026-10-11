import { test, expect } from '@playwright/test';

// Hermetic (no-auth, mock-backed) coverage for the provider-generic OAuth
// data-source connect UI. Runs against the dev-mode mock harness (see
// data-sources.config.ts): `run-e2e.sh` boots `mock-memory.mjs` (:5301) plus
// the gateway in AUTH_MODE=dev (:8097). Every memory call is answered by the
// mock, so the flows are deterministic and never touch a real backend.
//
// Routes under test (gateway, datasources.go / datasources_ui.go):
//   /data-sources/new                     provider chooser (catalog-driven)
//   /data-sources/oauth/start             OAuth start → follows memory authorizeUrl
//   /data-sources/oauth/callback          memory OAuth callback landing
//   /data-sources/:id                     source detail (seeded s1)
//   /orgs/:id/settings/oauth-clients      org-admin OAuth-client settings

// The two catalog providers both advertise OAuth + credentials, so the chooser
// renders a `ds-oauth-connect` and `ds-credentials-connect` per provider card.
// Scope every action to its provider card to disambiguate.
const driveChoice = (page: import('@playwright/test').Page) =>
  page.getByTestId('ds-provider-choice').filter({ hasText: 'Google Drive' });

test.describe('provider-generic OAuth data-source connect', () => {
  test('chooser lists both providers with OAuth and credentials actions', async ({ page }) => {
    await page.goto('/data-sources/new');

    // The catalog-driven chooser lists BOTH providers.
    await expect(page.getByTestId('ds-provider-chooser')).toBeVisible();
    await expect(page.getByTestId('ds-provider-choice').filter({ hasText: 'Google Drive' })).toBeVisible();
    await expect(page.getByTestId('ds-provider-choice').filter({ hasText: 'GitHub' })).toBeVisible();

    // The OAuth provider renders a "Connect with Google Drive" action that
    // starts the OAuth flow (no credentials pasted).
    const driveOAuth = driveChoice(page).getByTestId('ds-oauth-connect');
    await expect(driveOAuth).toBeVisible();
    await expect(driveOAuth).toContainText('Connect with Google Drive');
    await expect(driveOAuth).toHaveAttribute(
      'href',
      '/data-sources/oauth/start?providerType=google_drive',
    );

    // The credentials provider renders a credentials connect action.
    const driveCredentials = driveChoice(page).getByTestId('ds-credentials-connect');
    await expect(driveCredentials).toBeVisible();
    await expect(driveCredentials).toHaveAttribute(
      'href',
      '/data-sources/new?providerType=google_drive&method=credentials',
    );
  });

  test('OAuth start → callback → source detail (no secret prompt)', async ({ page }) => {
    await page.goto('/data-sources/new');

    // Start the OAuth flow: the gateway asks memory for the provider
    // authorizeUrl, the browser is bounced to the memory OAuth callback, and the
    // callback redirects to the freshly connected source.
    await driveChoice(page).getByTestId('ds-oauth-connect').click();
    await page.waitForURL('**/data-sources/s1?created=1**');

    // The detail page shows the source name and the PRG "connected" flash.
    await expect(page.getByTestId('page-team-drive')).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Team Drive' })).toBeVisible();
    await expect(page.getByText('Data source connected.')).toBeVisible();

    // OAuth connects without pasted credentials: the source detail never
    // prefills a secret. The detail edit form may render the write-only
    // credential fields (the credentials fallback), but they are always empty.
    for (const tid of ['ds-field-client_secret', 'ds-field-refresh_token']) {
      const inputs = page.getByTestId(tid);
      for (let i = 0; i < await inputs.count(); i++) {
        await expect(inputs.nth(i)).toHaveValue('');
      }
    }
  });

  test('org OAuth-client settings keep the secret write-only', async ({ page }) => {
    await page.goto('/orgs/org-1/settings/oauth-clients');

    // The org-admin gate passes (mock /api/user/orgs-and-projects marks org-1
    // org_admin), so the page renders the provider card form.
    const secret = page.getByTestId('ds-field-client_secret').first();
    await expect(secret).toBeVisible();
    await expect(secret).toHaveAttribute('type', 'password');
    // Write-only: the secret is never prefilled.
    await expect(secret).toHaveValue('');

    const form = secret.locator('xpath=ancestor::form[1]');
    await expect(form).toHaveAttribute('data-testid', 'ds-oauth-client-form');
    await form.getByTestId('ds-field-client_id').fill('e2e-oauth-client-id');
    await secret.fill('super-secret-e2e-value');
    // Google Drive advertises org-selectable scope strategies.
    await form.getByTestId('ds-field-strategy').selectOption('drive.file');
    await form.getByRole('button', { name: 'Save' }).click();

    // PRG round trip: redirect to ?saved=1 with the "OAuth client saved." flash.
    await expect(page).toHaveURL(/oauth-clients\?saved=1/);
    await expect(page.getByText('OAuth client saved.')).toBeVisible();

    // The submitted secret is never echoed back into the page.
    expect(await page.content()).not.toContain('super-secret-e2e-value');
    // And the freshly re-rendered secret field is still blank.
    await expect(page.getByTestId('ds-field-client_secret').first()).toHaveValue('');
  });
});
