import { Page, expect } from '@playwright/test';

export interface DriveSource {
  id: string;
  name: string;
}

/**
 * Create a Drive source through the gateway's JSON API — the same surface
 * legacy and API clients use. Drive's UI is OAuth-only, so there is no
 * pasted-credential form to drive: this is the create shape the connect flow
 * ends with (folder scope plus a stored refresh-token credential), which is
 * what the server's ExtractCredentials expects. Returns after navigating to
 * the detail page so callers start where the form flow used to leave them.
 */
export async function createDriveSource(
  page: Page,
  opts: { name: string; folder: string; token?: string },
): Promise<DriveSource> {
  const resp = await page.request.post('/api/data-source-integrations', {
    data: {
      name: opts.name,
      providerType: 'google_drive',
      sourceType: 'drive',
      config: {
        // Canonical folder-target list. A legacy folder_id is still accepted by
        // the API, but the specs exercise the shape the picker writes.
        folders: [{ folder_id: opts.folder, name: opts.folder }],
        refresh_token: opts.token ?? 'e2e-refresh-token',
      },
    },
  });
  if (resp.status() !== 201) {
    throw new Error(`create Drive source failed (${resp.status()}): ${await resp.text()}`);
  }
  const created = (await resp.json()) as { id: string };
  expect(created.id, 'create should return the new source id').toBeTruthy();
  await page.goto(`/data-sources/${created.id}`);
  return { id: created.id, name: opts.name };
}

/**
 * Delete a source through the gateway form endpoint. Cleanup is best-effort so
 * a failed assertion never masks the original failure.
 */
export async function deleteDriveSource(page: Page, id: string): Promise<void> {
  if (!id) return;
  try {
    await page.request.post(`/data-sources/${id}/delete`);
  } catch {
    /* cleanup is best-effort */
  }
}

export interface GitHubSource {
  id: string;
  name: string;
}

/** The saved GitHub credential the mock seeds for the picker specs. */
export const SEEDED_GITHUB_CREDENTIAL_ID = 'cred-gh-1';

/**
 * Select a saved credential on the connect form (or the source settings card).
 * The credential is chosen by value so the label copy can change freely.
 */
export async function selectSavedCredential(
  page: Page,
  credentialId: string = SEEDED_GITHUB_CREDENTIAL_ID,
): Promise<void> {
  const picker = page.getByTestId('ds-field-credential');
  await expect(picker).toBeVisible();
  await picker.selectOption(credentialId);
}

/**
 * Connect a GitHub source through the schema-driven credentials form: a saved
 * personal-access-token credential, repository (typed or picked), name. The mock
 * validates that a repository and a credential are present, matching the server.
 */
export async function createGitHubSource(
  page: Page,
  opts: { name: string; repo: string; credentialId?: string },
): Promise<GitHubSource> {
  await page.goto('/data-sources/new?providerType=github&method=credentials');
  await expect(page.getByTestId('ds-connect-form')).toBeVisible();
  await page.getByTestId('ds-field-name').fill(opts.name);
  await selectSavedCredential(page, opts.credentialId);
  await page.getByTestId('ds-field-repo').fill(opts.repo);
  await page.getByTestId('ds-connect-submit').click();
  await page.waitForURL(/\/data-sources\/ds-\d+/);
  const id = page.url().match(/\/data-sources\/(ds-\d+)/)?.[1] ?? '';
  expect(id, 'connect should redirect to the new source detail page').not.toBe('');
  return { id, name: opts.name };
}

export interface WebSource {
  id: string;
  name: string;
}

/**
 * Connect a Web source through the schema-driven config form. Web is
 * config-only (no credential panel): the connect form collects the start URL
 * list (a textarea, one URL per line) plus optional crawl-scope filters and
 * limits. The mock validates that at least one start URL is present, matching
 * the server's `start_urls` requirement.
 */
export async function createWebSource(
  page: Page,
  opts: { name: string; startUrls: string[] },
): Promise<WebSource> {
  await page.goto('/data-sources/new?providerType=web&method=config');
  await expect(page.getByTestId('ds-connect-form')).toBeVisible();
  await page.getByTestId('ds-field-name').fill(opts.name);
  await page.getByTestId('ds-field-start_urls').fill(opts.startUrls.join('\n'));
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.waitForURL(/\/data-sources\/ds-\d+/);
  const id = page.url().match(/\/data-sources\/(ds-\d+)/)?.[1] ?? '';
  expect(id, 'connect should redirect to the new source detail page').not.toBe('');
  return { id, name: opts.name };
}

export interface TraleSource {
  id: string;
  name: string;
}

/**
 * Create a Trale source through the gateway's JSON API. Trale connect is
 * OAuth-only and self-registering, so there is no pasted-credential form to
 * drive: this is the create shape the OAuth connect flow ends with (the chosen
 * tag scope plus a stored token set), which is what the server's
 * ExtractCredentials expects. Returns after navigating to the detail page.
 */
export async function createTraleSource(
  page: Page,
  opts: { name: string; tags?: string[]; backfillDays?: number; includeTranscript?: boolean },
): Promise<TraleSource> {
  const resp = await page.request.post('/api/data-source-integrations', {
    data: {
      name: opts.name,
      providerType: 'trale',
      sourceType: 'meeting',
      config: {
        tags: opts.tags ?? ['standup'],
        backfill_days: opts.backfillDays ?? 30,
        include_transcript: opts.includeTranscript ?? true,
        // OAuth credential material the connect flow stores; the mock strips it
        // from read responses exactly like the server does. Mirrors a full Trale
        // token exchange (the server's write-only set — access_token, refresh_token,
        // expires_at, client_id, token_endpoint, resource, scope), so the spec can
        // assert the *extra* keys are stripped too, not just the token pair.
        access_token: 'e2e-trale-token',
        refresh_token: 'e2e-trale-refresh',
        expires_at: '2026-10-22T00:00:00Z',
        token_endpoint: 'https://example.invalid/oauth/token',
        resource: 'https://app.trale.ai/api/mcp',
        scope: 'openid email profile',
      },
    },
  });
  if (resp.status() !== 201) {
    throw new Error(`create Trale source failed (${resp.status()}): ${await resp.text()}`);
  }
  const created = (await resp.json()) as { id: string };
  expect(created.id, 'create should return the new source id').toBeTruthy();
  await page.goto(`/data-sources/${created.id}`);
  return { id: created.id, name: opts.name };
}
