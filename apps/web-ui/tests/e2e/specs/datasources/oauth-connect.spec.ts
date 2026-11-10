import { test, expect } from '@playwright/test';

// Hermetic (no-auth, mock-backed) coverage for the provider-generic OAuth
// data-source connect UI. Runs against the dev-mode mock harness (see
// datasources.config.ts): `run-e2e.sh` boots `mock-memory.mjs` plus the gateway
// in AUTH_MODE=dev, on a port pair derived per run. Every memory call is
// answered by the mock, so the flows are deterministic and never touch a real
// backend.
//
// Routes under test (gateway, datasources.go / datasources_ui.go):
//   /data-sources/new                     provider chooser (catalog-driven)
//   /data-sources/oauth/start             OAuth start → follows memory authorizeUrl
//   /data-sources/oauth/callback          memory OAuth callback landing
//   /data-sources/:id                     source detail (seeded s1)
//   /orgs/:id/settings/oauth-clients      org-admin OAuth-client settings

// Stubs the Google Picker (gapi + google.picker) so a spec exercises the gateway
// modal, session fetch, multi-select accumulation, and save without loading
// Google's script or opening an iframe. The stub "picks" the given folders (or
// cancels) as soon as the dialog is shown, and records the views it was given
// plus whether the host <dialog> was still in the top layer at that moment.
async function stubGooglePicker(
  page: import('@playwright/test').Page,
  picked: Array<{ id: string; name: string }>,
  options: { cancel?: boolean } = {},
): Promise<void> {
  await page.addInitScript(
    ({ docs, cancel }) => {
      interface StubView {
        viewId: string;
        enableDrives: boolean;
        mode: string | null;
      }
      const views: StubView[] = [];
      const picker = {
        Response: { ACTION: 'action', DOCUMENTS: 'docs' },
        Action: { PICKED: 'picked', CANCEL: 'cancel', ERROR: 'error' },
        Document: { ID: 'id', NAME: 'name' },
        Feature: { MULTISELECT_ENABLED: 'multiselect' },
        ViewId: { FOLDERS: 'folders' },
        DocsViewMode: { LIST: 'list' },
        DocsView: class {
          viewId: string;
          enableDrives = false;
          mode: string | null = null;
          constructor(viewId: string) {
            this.viewId = viewId;
            views.push(this);
          }
          setIncludeFolders() { return this; }
          setSelectFolderEnabled() { return this; }
          setEnableDrives(value: boolean) {
            this.enableDrives = value !== false;
            return this;
          }
          setMode(mode: string) {
            this.mode = mode;
            return this;
          }
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
                const host = document.getElementById('ds-folder-picker') as HTMLDialogElement | null;
                w.__dsPickerHostOpenAtShow = Boolean(host && host.open);
                setTimeout(() => cb?.(cancel ? { action: 'cancel' } : { action: 'picked', docs }), 0);
              },
            };
          }
        },
      };
      const w = window as unknown as {
        google: unknown;
        gapi: unknown;
        __dsPickerViews: StubView[];
        __dsPickerHostOpenAtShow?: boolean;
      };
      w.google = { picker };
      w.__dsPickerViews = views;
      w.gapi = {
        load: (_name: string, opts: { callback: () => void }) => opts.callback(),
      };
    },
    { docs: picked, cancel: options.cancel === true },
  );
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

  test('reconnect repairs the seeded source instead of opening a new one', async ({ page }) => {
    // A grant that was revoked leaves the source with an actionable error; the
    // credentials card offers the repair, and it targets THIS source (#2107).
    await page.goto('/data-sources/s1');
    const reconnect = page.getByTestId('ds-credential-reconnect');
    await expect(reconnect).toBeVisible();
    await expect(reconnect).toHaveAttribute('href', '/data-sources/s1/reconnect');

    // Consent runs again and lands back on the same source as a renewal — not on
    // the new-source flow, and not claiming a second source was connected.
    await reconnect.click();
    await page.waitForURL('**/data-sources/s1?reconnected=1**');
    await expect(page.getByTestId('page-team-drive')).toBeVisible();
    await expect(page.getByText('Connection renewed.')).toBeVisible();
    await expect(page.getByText('Data source connected.')).toHaveCount(0);
  });

  test('cancelling the Google Picker reopens the modal without changing the selection', async ({ page }) => {
    await stubGooglePicker(page, [], { cancel: true });

    await page.goto('/data-sources/s1');
    await page.getByTestId('ds-choose-folders').click();
    const dialog = page.getByTestId('ds-folder-picker');
    await expect(dialog).toBeVisible();
    const before = ((await dialog.getByTestId('ds-folder-picker-count').textContent()) ?? '').trim();

    await dialog.getByTestId('ds-folder-picker-open').click();

    // Wait for the picker itself to mount (the stub records that synchronously
    // from setVisible) before asserting anything about the callback/restore;
    // the session fetch and script load are async.
    await expect
      .poll(async () =>
        page.evaluate(() => {
          const w = window as unknown as { __dsPickerHostOpenAtShow?: boolean };
          return w.__dsPickerHostOpenAtShow;
        }),
      )
      .toBe(false);
    await expect(dialog).toBeVisible();
    await expect(dialog.getByTestId('ds-folder-picker-count')).toHaveText(before);
    await expect(dialog.getByTestId('ds-folder-picker-error')).toBeHidden();
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

    // Regression: My Drive must be its own view — setEnableDrives(true) alone
    // restricts a view to shared drives, hiding My Drive entirely — and the
    // host modal must leave the browser top layer while the picker is visible.
    const pickerState = await page.evaluate(() => {
      const w = window as unknown as {
        __dsPickerViews: Array<{ viewId: string; enableDrives: boolean; mode: string | null }>;
        __dsPickerHostOpenAtShow?: boolean;
      };
      return {
        views: w.__dsPickerViews.map((view) => ({
          viewId: view.viewId,
          enableDrives: view.enableDrives,
          mode: view.mode,
        })),
        hostOpenAtShow: w.__dsPickerHostOpenAtShow,
      };
    });
    expect(pickerState.views.map((view) => view.enableDrives)).toEqual([false, true]);
    expect(pickerState.views.every((view) => view.viewId === 'folders')).toBe(true);
    expect(pickerState.views.every((view) => view.mode === 'list')).toBe(true);
    expect(pickerState.hostOpenAtShow).toBe(false);

    await dialog.getByTestId('ds-folder-picker-save').click();
    await page.waitForURL(/updated=1/);

    // The saved list renders as chips and the sync-target prompt is gone.
    await expect(page.getByTestId('ds-folder-target')).toHaveCount(2);
    await expect(page.getByTestId('ds-folder-targets')).toContainText('Picked Folder');
    await expect(page.getByTestId('ds-resource-needed')).toHaveCount(0);
  });

  test('picker save surfaces an unreadable target and keeps the selection', async ({ page }) => {
    // The mock upstream resolves ids against its folder catalog and rejects an
    // unknown one, exactly as the server does against Drive. The modal must show
    // that message inline rather than closing or discarding the selection.
    await stubGooglePicker(page, [{ id: 'folder-missing', name: 'Missing Folder' }]);

    await page.goto('/data-sources/s1');
    await page.getByTestId('ds-choose-folders').click();
    const dialog = page.getByTestId('ds-folder-picker');
    await expect(dialog).toBeVisible();

    // The modal seeds from the source's current selection and s1 is shared with
    // the spec above, which saves two folders. Remove them so the only id saved
    // is the unreadable one — the account-mismatch shape, where none of the
    // picked folders resolves, rather than one bad id among resolvable ones.
    for (;;) {
      const remove = dialog.getByRole('button', { name: /^Remove / }).first();
      if ((await remove.count()) === 0) break;
      await remove.click();
    }
    await expect(dialog.getByTestId('ds-folder-picker-count')).toHaveText('0 selected');

    await dialog.getByTestId('ds-folder-picker-open').click();
    await expect(dialog.getByTestId('ds-folder-picker-selected')).toContainText('Missing Folder');

    await dialog.getByTestId('ds-folder-picker-save').click();
    await expect(dialog.getByTestId('ds-folder-picker-error')).toContainText(
      'can be seen by the account',
    );
    await expect(dialog).toBeVisible();
    await expect(dialog.getByTestId('ds-folder-picker-selected')).toContainText('Missing Folder');
    // A rejected save never navigates: the modal stays open on the source page.
    expect(new URL(page.url()).searchParams.get('updated')).toBeNull();
    await expect(page.getByTestId('ds-sync-scope')).toBeVisible();
  });

  test('org OAuth apps keep the secret write-only', async ({ page }) => {
    const name = `e2e-org-${Date.now()}`;
    await page.goto('/orgs/org-1/settings/oauth-clients');

    // The org-admin gate passes (mock /api/user/orgs-and-projects marks org-1
    // org_admin). The page lists named apps and offers an add action; no
    // credential form is rendered on the list itself.
    await expect(page.getByTestId('ds-credentials-table')).toBeVisible();
    await expect(page.getByTestId('ds-oauth-app-add')).toBeVisible();
    await expect(page.getByTestId('ds-oauth-app-form')).toHaveCount(0);
    await page.getByTestId('ds-oauth-app-add').click();
    await expect(page).toHaveURL(/\/orgs\/org-1\/settings\/oauth-apps\/new$/);
    await expect(page.getByTestId('ds-oauth-app-form')).toBeVisible();

    const secret = page.getByTestId('ds-oauth-app-client-secret');
    await expect(secret).toHaveAttribute('type', 'password');
    await expect(secret).toHaveValue('');

    await page.getByTestId('ds-oauth-app-vendor').selectOption('google');
    await page.getByTestId('ds-oauth-app-name').fill(name);
    await page.getByTestId('ds-oauth-app-client-id').fill('e2e-oauth-client-id');
    await secret.fill('super-secret-e2e-value');
    // Google Drive advertises org-selectable scope strategies.
    await page.getByTestId('ds-oauth-app-drive-strategy').selectOption('drive.file');
    await page.getByTestId('ds-oauth-app-save').click();

    // PRG round trip: redirect to ?saved=1 with the flash.
    await expect(page).toHaveURL(/oauth-clients\?saved=1/);
    await expect(page.getByText('OAuth app saved.')).toBeVisible();

    // The submitted secret is never echoed back into the page.
    expect(await page.content()).not.toContain('super-secret-e2e-value');
    const row = page.getByTestId('ds-credential-row').filter({ hasText: name });
    await expect(row).toHaveCount(1);

    // Re-opening the editor renders a blank secret field: write-only.
    await row.getByTestId('ds-credential-manage').click();
    await expect(page.getByTestId('ds-oauth-app-client-secret')).toHaveValue('');
    await expect(page.getByTestId('ds-oauth-app-client-id')).toHaveValue('e2e-oauth-client-id');
  });
});
