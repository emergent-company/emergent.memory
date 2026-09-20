import { test, expect, type Page } from '@playwright/test';
import { readBootstrap, createOrg } from '../../helpers/bootstrap';

// Org rename: gateway/org_context.go `uiOrgRename` (POST /orgs/:id/rename,
// form field `name`) surfaced on the org Settings hub → General section
// (`/orgs/:id/settings/general`, `#org-name`). A scratch org is created via the
// API (never the bootstrap tenant), renamed through the UI, and deleted in
// `finally` (with the bootstrap project reactivated so the shared tenant is not
// left with a cleared session context). Mirrors the project-restore-ui scratch
// lifecycle.

async function deleteOrgAndPollGone(page: Page, orgId: string): Promise<void> {
  await page.request.post(`/orgs/${orgId}/delete`).catch(() => {});
  await expect
    .poll(
      async () => {
        const resp = await page.request.get('/api/orgs');
        if (!resp.ok()) throw new Error(`list orgs failed: ${resp.status()}`);
        const orgs = (await resp.json()) as Array<{ id: string }>;
        return orgs.some((o) => o.id === orgId);
      },
      { timeout: 20_000, intervals: [200, 300, 500] },
    )
    .toBe(false);
}

test('renaming an org persists on the General settings page', async ({ page }) => {
  const bootstrap = readBootstrap();
  expect(bootstrap, 'setup project must run first').toBeTruthy();

  const stamp = `${Date.now()}-${Math.floor(Math.random() * 1e4)}`;
  const name = `E2E Rename Org ${stamp}`;
  const renamed = `${name} Renamed`;
  const orgId = await createOrg(page, name);

  try {
    await page.goto(`/orgs/${orgId}/settings/general`);
    await expect(page.locator('#org-name')).toHaveValue(name);

    await page.locator('#org-name').fill(renamed);
    await page.getByRole('button', { name: 'Save name' }).click();

    // Success PRG redirects back with ?renamed=1; the new name persists.
    await page.waitForURL(new RegExp(`/orgs/${orgId}/settings/general\\?renamed=1`));
    await page.reload();
    await expect(page.locator('#org-name')).toHaveValue(renamed);
  } finally {
    if (bootstrap?.projectId) {
      await page.request
        .post(`/api/projects/${bootstrap.projectId}/activate`)
        .catch(() => {});
    }
    await deleteOrgAndPollGone(page, orgId);
  }
});
