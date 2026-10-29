import { test, expect } from '@playwright/test';
import { createTraleSource, deleteDriveSource } from '../../helpers/datasources';

// Trale data source connect + edit, driven by the provider catalog. Trale is
// OAuth-only and self-registering (RFC 7591 dynamic client registration), so its
// chooser card advertises no pasted-credential action, the `?providerType=trale`
// connect surface renders the OAuth panel with no connect form, and its config
// (tags / name_pattern / include_transcript / backfill_days) is collected only
// on the post-OAuth edit form. Runs against the dev-mode mock harness (see
// datasources.config.ts), so every memory call is answered by mock-memory.mjs.

const traleChoice = (page: import('@playwright/test').Page) =>
  page.getByTestId('ds-provider-choice').filter({ hasText: 'Trale' });

test.describe('Trale data source', () => {
  test('chooser renders a Trale card with OAuth only (no credentials CTA)', async ({ page }) => {
    await page.goto('/data-sources/new');

    await expect(page.getByTestId('ds-provider-chooser')).toBeVisible();
    const card = traleChoice(page);
    await expect(card).toBeVisible();

    const oauth = card.getByTestId('ds-oauth-connect');
    await expect(oauth).toBeVisible();
    await expect(oauth).toContainText('Connect with Trale');
    await expect(oauth).toHaveAttribute('href', '/data-sources/oauth/start?providerType=trale');

    // Trale advertises oauth only, so its card offers no credentials action.
    await expect(card.getByTestId('ds-credentials-connect')).toHaveCount(0);
  });

  test('?providerType=trale renders the OAuth panel with no connect form', async ({ page }) => {
    await page.goto('/data-sources/new?providerType=trale');

    // The connect step is OAuth-only: tags are chosen post-OAuth, so the panel
    // renders and there is never a schema-driven connect form or a password.
    await expect(page.getByTestId('ds-oauth-connect')).toBeVisible();
    await expect(page.getByTestId('ds-connect-form')).toHaveCount(0);
    await expect(page.locator('input[type="password"]')).toHaveCount(0);
  });

  test('creates a source and renders its config fields (no credential inputs)', async ({ page }) => {
    const src = await createTraleSource(page, { name: `e2e-trale-${Date.now()}` });
    try {
      await expect(page.getByRole('heading', { name: src.name })).toBeVisible();
      await expect(page.getByTestId('ds-edit-form')).toBeVisible();

      // The post-OAuth edit form exposes the whole config contract.
      await expect(page.getByTestId('ds-field-tags')).toBeVisible();
      await expect(page.getByTestId('ds-field-name_pattern')).toBeVisible();
      await expect(page.getByTestId('ds-field-include_transcript')).toBeVisible();
      await expect(page.getByTestId('ds-field-backfill_days')).toBeVisible();

      // Persisted values round-trip: tags join to the slug list, the boolean is
      // checked, and the integer is prefilled (not blanked to the 90 default).
      await expect(page.getByTestId('ds-field-tags')).toHaveValue('standup');
      await expect(page.getByTestId('ds-field-include_transcript')).toBeChecked();
      await expect(page.getByTestId('ds-field-backfill_days')).toHaveValue('30');

      // OAuth-only: no write-only credential input is rendered on the edit form.
      await expect(page.locator('input[type="password"]')).toHaveCount(0);
      await expect(page.getByTestId('ds-field-access_token')).toHaveCount(0);
      await expect(page.getByTestId('ds-field-refresh_token')).toHaveCount(0);
    } finally {
      await deleteDriveSource(page, src.id);
    }
  });

  test('round-trips the backfill_days integer through edit', async ({ page }) => {
    // This is the regression the review caught twice: an integer config value
    // must not blank on the edit form (a save would silently reset it to the
    // default 90). Pin it end to end: 30 → edit 45 → save → re-read as 45.
    const src = await createTraleSource(page, { name: `e2e-trale-${Date.now()}`, backfillDays: 30 });
    try {
      await expect(page.getByTestId('ds-field-backfill_days')).toHaveValue('30');

      await page.getByTestId('ds-field-backfill_days').fill('45');
      await page.getByRole('button', { name: 'Save changes' }).click();
      await page.waitForURL(/updated=1/);

      // The PRG re-render shows the new value…
      await expect(page.getByTestId('ds-field-backfill_days')).toHaveValue('45');
      // …and a fresh read of the source proves persistence, not just the flash.
      await page.goto(`/data-sources/${src.id}`);
      await expect(page.getByTestId('ds-field-backfill_days')).toHaveValue('45');
    } finally {
      await deleteDriveSource(page, src.id);
    }
  });

  test('rejects an empty tags list', async ({ page }) => {
    const resp = await page.request.post('/api/data-source-integrations', {
      data: {
        name: `e2e-trale-${Date.now()}`,
        providerType: 'trale',
        sourceType: 'meeting',
        config: { tags: [] },
      },
    });
    expect(resp.ok(), await resp.text()).toBe(false);
    expect(await resp.text()).toContain(
      'config.tags is required: choose at least one Trale tag to sync',
    );
  });

  test('rejects an uncompilable name_pattern', async ({ page }) => {
    const resp = await page.request.post('/api/data-source-integrations', {
      data: {
        name: `e2e-trale-${Date.now()}`,
        providerType: 'trale',
        sourceType: 'meeting',
        config: { tags: ['standup'], name_pattern: '[' },
      },
    });
    expect(resp.ok(), await resp.text()).toBe(false);
    expect(await resp.text()).toContain('config.name_pattern is not a valid regular expression:');
  });

  test('rejects a JS-only name_pattern construct (RE2 has no lookbehind)', async ({ page }) => {
    // `(?<=x)y` is a lookbehind: JS RegExp accepts it but Go RE2 rejects it, so
    // the mock must reject it too (server-shaped error). This pins the RE2
    // approximation — without it, a JS-only pattern would pass the mock and fail
    // on the real server.
    const resp = await page.request.post('/api/data-source-integrations', {
      data: {
        name: `e2e-trale-${Date.now()}`,
        providerType: 'trale',
        sourceType: 'meeting',
        config: { tags: ['standup'], name_pattern: '(?<=x)y' },
      },
    });
    expect(resp.ok(), await resp.text()).toBe(false);
    expect(await resp.text()).toContain('config.name_pattern is not a valid regular expression:');
  });

  test('rejects a backreference ending in 8 or 9, which RE2 refuses', async ({ page }) => {
    // `\18` is not a valid JS backreference (it needs a group 18) but it *is*
    // an escape Go rejects, because a trailing 8 is not an octal digit: RE2
    // accepts `\128` and refuses `\18`. Pin that edge so the mock cannot drift
    // back to accepting it while claiming to model RE2.
    const resp = await page.request.post('/api/data-source-integrations', {
      data: {
        name: `e2e-trale-${Date.now()}`,
        providerType: 'trale',
        sourceType: 'meeting',
        config: { tags: ['standup'], name_pattern: '\\18' },
      },
    });
    expect(resp.ok(), await resp.text()).toBe(false);
    expect(await resp.text()).toContain('config.name_pattern is not a valid regular expression:');
  });

  test('never echoes Trale credential material on read', async ({ page }) => {
    // The create fixture mirrors a full Trale token exchange (expires_at,
    // token_endpoint, resource, scope in addition to the token pair). The read
    // payload must strip the whole write-only set, so assert each extra key is
    // absent from the JSON API read and that no token value leaks into the page.
    const src = await createTraleSource(page, { name: `e2e-trale-${Date.now()}` });
    try {
      const resp = await page.request.get(`/api/data-source-integrations/${src.id}`);
      expect(resp.ok(), await resp.text()).toBe(true);
      const body = (await resp.json()) as { config?: Record<string, unknown> };
      const config = body.config ?? {};
      for (const key of ['expires_at', 'token_endpoint', 'resource', 'scope']) {
        expect(config, `read config must not echo ${key}`).not.toHaveProperty(key);
      }
      expect(JSON.stringify(body)).not.toContain('e2e-trale-token');
      expect(JSON.stringify(body)).not.toContain('e2e-trale-refresh');

      // The edit page renders the same stripped config: no credential field or
      // value surfaces.
      await page.goto(`/data-sources/${src.id}`);
      await expect(page.getByTestId('ds-edit-form')).toBeVisible();
      const html = await page.content();
      expect(html).not.toContain('e2e-trale-token');
      expect(html).not.toContain('e2e-trale-refresh');
      expect(html).not.toContain('https://app.trale.ai/api/mcp');
    } finally {
      await deleteDriveSource(page, src.id);
    }
  });

  test('rejects a non-integer backfill_days', async ({ page }) => {
    const resp = await page.request.post('/api/data-source-integrations', {
      data: {
        name: `e2e-trale-${Date.now()}`,
        providerType: 'trale',
        sourceType: 'meeting',
        config: { tags: ['standup'], backfill_days: 'ninety' },
      },
    });
    expect(resp.ok(), await resp.text()).toBe(false);
    expect(await resp.text()).toContain('config.backfill_days must be an integer');
  });

  test('is absent from the project credentials surface (self-registering)', async ({ page }) => {
    await page.goto('/settings/credentials');
    await expect(page.getByTestId('ds-credentials-table')).toBeVisible();
    // Trale registers its own OAuth client at connect time, so there is no
    // client id/secret for an admin to manage and no table row for it.
    await expect(page.getByTestId('ds-credential-row').filter({ hasText: 'Trale' })).toHaveCount(0);

    // The per-provider editor 404s for a self-registering provider.
    const resp = await page.goto('/settings/credentials/trale');
    expect(resp?.status()).toBe(404);
  });

  test('reports the self-registered client and an honest grant, with no app set-up affordance', async ({
    page,
  }) => {
    // A connected Trale source used to claim "No OAuth app is resolved…" and
    // link to a credential page that 404s, while labelling the account "Google
    // account" and the grant "Not reported by the provider". None of that is
    // actionable for a provider that registers its own client (RFC 7591).
    const src = await createTraleSource(page, { name: `e2e-trale-${Date.now()}` });
    try {
      const card = page.getByTestId('ds-credentials-card');
      await expect(card).toBeVisible();

      await expect(page.getByTestId('ds-credential-scope')).toHaveText('Registered at connect');
      await expect(card).toContainText('Public client registered automatically');

      // The account row is named after the provider's vendor, not Google.
      await expect(card.locator('dt', { hasText: 'Trale account' })).toBeVisible();

      // A source created through the API carries no grant metadata (the OAuth
      // callback records it), so the card says so neutrally instead of claiming
      // the provider failed to report a grant. The recorded-scope rendering is
      // covered by the gateway tests, which can build that config.
      await expect(card).toContainText('Not recorded at connect');
      await expect(card).not.toContainText('Not reported by the provider');
      await expect(card).not.toContainText('openid email profile');

      // No dead end: no "set one up" warning and no link to the 404ing editor.
      await expect(page.getByTestId('ds-credential-missing')).toHaveCount(0);
      await expect(page.getByTestId('ds-credential-settings-link')).toHaveCount(0);
      // The repair action that does exist is still offered.
      await expect(page.getByTestId('ds-credential-reconnect')).toBeVisible();
    } finally {
      await deleteDriveSource(page, src.id);
    }
  });

  test('the tags picker lists the saved source’s tags and appends to them', async ({ page }) => {
    // The picker used to be labelled "Load repositories" and to fail with
    // "config.access_token is required": it posted the edit form, which carries
    // no credential. A saved source must list through its STORED grant.
    const src = await createTraleSource(page, { name: `e2e-trale-${Date.now()}` });
    try {
      const tags = page.getByTestId('ds-field-tags');
      await expect(tags).toHaveValue('standup');

      const load = page.getByTestId('ds-load-tags');
      await expect(load).toHaveText('Load tags');
      await load.click();

      const chips = page.getByTestId('ds-options-chips');
      await expect(chips).toBeVisible();
      await expect(page.getByTestId('ds-options-error')).toHaveCount(0);

      // Appending keeps the value already chosen: a datalist click would have
      // replaced it.
      await chips.getByRole('button', { name: /Planning/ }).click();
      await expect(tags).toHaveValue(/standup[\s\S]*planning/);

      // The appended value is a real config value, not just text in the field.
      await page.getByRole('button', { name: 'Save changes' }).click();
      await page.waitForURL(/updated=1/);
      await expect(page.getByTestId('ds-field-tags')).toHaveValue(/standup[\s\S]*planning/);
    } finally {
      await deleteDriveSource(page, src.id);
    }
  });
});
