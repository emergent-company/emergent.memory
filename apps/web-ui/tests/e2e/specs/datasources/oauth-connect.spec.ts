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

// Stubs the Google Picker (gapi + google.picker) so a spec exercises the gateway
// modal, session fetch, multi-select accumulation, and save without loading
// Google's script or opening an iframe. The stub "picks" the given folders as
// soon as the dialog is shown.
async function stubGooglePicker(
  page: import('@playwright/test').Page,
  picked: Array<{ id: string; name: string }>,
): Promise<void> {
  await page.addInitScript((docs) => {
    const picker = {
      Response: { ACTION: 'action', DOCUMENTS: 'docs' },
      Action: { PICKED: 'picked', CANCEL: 'cancel' },
      Document: { ID: 'id', NAME: 'name' },
      Feature: { MULTISELECT_ENABLED: 'multiselect' },
      ViewId: { FOLDERS: 'folders' },
      DocsView: class {
        setIncludeFolders() { return this; }
        setSelectFolderEnabled() { return this; }
        setEnableDrives() { return this; }
      },
      PickerBuilder: class {
        cb: ((data: unknown) => void) | null = null;
        setOAuthToken() { return this; }
        setDeveloperKey() { return this; }
        addView() { return this; }
        setAppId() { return this; }
        setOrigin() { return this; }
        enableFeature() { return this; }
        setCallback(cb: (data: unknown) => void) { this.cb = cb; return this; }
        build() {
          const cb = this.cb;
          return {
            setVisible: () => {
              setTimeout(() => cb?.({ action: 'picked', docs }), 0);
            },
          };
        }
      },
    };
    (window as unknown as { google: unknown }).google = { picker };
    (window as unknown as { gapi: unknown }).gapi = {
      load: (_name: string, opts: { callback: () => void }) => opts.callback(),
    };
  }, picked);
}

// Google Drive advertises OAuth only (its card has no credentials action);
// GitHub advertises OAuth + credentials, so its card renders both. Scope every
// action to its provider card to disambiguate.
const driveChoice = (page: import('@playwright/test').Page) =>
  page.getByTestId('ds-provider-choice').filter({ hasText: 'Google Drive' });
const githubChoice = (page: import('@playwright/test').Page) =>
  page.getByTestId('ds-provider-choice').filter({ hasText: 'GitHub' });

test.describe('provider-generic OAuth data-source connect', () => {
  test('chooser renders the connect actions each provider advertises', async ({ page }) => {
    await page.goto('/data-sources/new');

    // The catalog-driven chooser lists BOTH providers.
    await expect(page.getByTestId('ds-provider-chooser')).toBeVisible();
    await expect(driveChoice(page)).toBeVisible();
    await expect(githubChoice(page)).toBeVisible();

    // The OAuth provider renders a "Connect with Google Drive" action that
    // starts the OAuth flow (no credentials pasted).
    const driveOAuth = driveChoice(page).getByTestId('ds-oauth-connect');
    await expect(driveOAuth).toBeVisible();
    await expect(driveOAuth).toContainText('Connect with Google Drive');
    await expect(driveOAuth).toHaveAttribute(
      'href',
      '/data-sources/oauth/start?providerType=google_drive',
    );

    // Drive is OAuth-only: its card offers no copy-with-credentials action.
    await expect(driveChoice(page).getByTestId('ds-credentials-connect')).toHaveCount(0);

    // GitHub advertises both methods, so its card renders the credentials action.
    const githubCredentials = githubChoice(page).getByTestId('ds-credentials-connect');
    await expect(githubCredentials).toBeVisible();
    await expect(githubCredentials).toHaveAttribute(
      'href',
      '/data-sources/new?providerType=github&method=credentials',
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

    // OAuth connects without pasted credentials: Drive declares no credential
    // fields, so the detail edit form renders no secret input at all.
    await expect(page.getByTestId('ds-edit-form')).toBeVisible();
    await expect(page.locator('input[type="password"]')).toHaveCount(0);
    await expect(page.getByTestId('ds-field-refresh_token')).toHaveCount(0);
    await expect(page.getByTestId('ds-field-client_secret')).toHaveCount(0);
  });

  test('OAuth-connected source picks folders in the modal and saves them', async ({ page }) => {
    await stubGooglePicker(page, [
      { id: 'folder-picked', name: 'Picked Folder' },
      { id: 'folder-two', name: 'Second Folder' },
    ]);

    // The seeded OAuth source (s1) starts without a folder target, which is the
    // state the OAuth callback leaves a source in. The sync-scope action is
    // always available, so the spec also works when a retry finds s1 populated.
    await page.goto('/data-sources/s1');
    await page.getByTestId('ds-choose-folders').click();

    const dialog = page.getByTestId('ds-folder-picker');
    await expect(dialog).toBeVisible();

    await dialog.getByTestId('ds-folder-picker-open').click();
    await expect(dialog.getByTestId('ds-folder-picker-selected')).toContainText('Picked Folder');
    await expect(dialog.getByTestId('ds-folder-picker-selected')).toContainText('Second Folder');
    await expect(dialog.getByTestId('ds-folder-picker-count')).toHaveText('2 selected');

    await dialog.getByTestId('ds-folder-picker-save').click();
    await page.waitForURL(/updated=1/);

    // The saved list renders as chips and the sync-target prompt is gone.
    await expect(page.getByTestId('ds-folder-target')).toHaveCount(2);
    await expect(page.getByTestId('ds-folder-targets')).toContainText('Picked Folder');
    await expect(page.getByTestId('ds-resource-needed')).toHaveCount(0);
  });

  test('picker save surfaces a rejected target and keeps the selection', async ({ page }) => {
    // The mock upstream resolves ids against its folder catalog and rejects an
    // unknown one, exactly as the server does against Drive. The modal must show
    // that message inline rather than closing or discarding the selection.
    await stubGooglePicker(page, [{ id: 'folder-missing', name: 'Missing Folder' }]);

    await page.goto('/data-sources/s1');
    await page.getByTestId('ds-choose-folders').click();
    const dialog = page.getByTestId('ds-folder-picker');
    await expect(dialog).toBeVisible();

    await dialog.getByTestId('ds-folder-picker-open').click();
    await expect(dialog.getByTestId('ds-folder-picker-selected')).toContainText('Missing Folder');

    await dialog.getByTestId('ds-folder-picker-save').click();
    await expect(dialog.getByTestId('ds-folder-picker-error')).toContainText('not accessible');
    await expect(dialog).toBeVisible();
    await expect(dialog.getByTestId('ds-folder-picker-selected')).toContainText('Missing Folder');
    // A rejected save never navigates: the modal stays open on the source page.
    expect(new URL(page.url()).searchParams.get('updated')).toBeNull();
    await expect(page.getByTestId('ds-sync-scope')).toBeVisible();
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
