import { test, expect, type Page } from '@playwright/test';
import { readBootstrap, createOrg } from '../../helpers/bootstrap';

// Org tool settings: gateway/org_context.go `uiOrgToolSettingUpdate` +
// `uiOrgToolSettingDelete` (POST /orgs/:id/tool-settings/:toolName [+ /delete]),
// surfaced on the org Settings hub (`/orgs/:id/settings`). An override row is a
// name + Enabled/Disabled badge with enable/disable/delete forms. A fresh org
// has no overrides, so the spec first creates one via the toggle route
// (enabled=true), then toggles it off and deletes it through the UI, asserting
// the resulting state each step. Scratch org + bootstrap-reactivation cleanup,
// mirroring project-restore-ui.

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

test('org tool-setting override toggles and deletes', async ({ page }) => {
  const bootstrap = readBootstrap();
  expect(bootstrap, 'setup project must run first').toBeTruthy();

  const stamp = `${Date.now()}-${Math.floor(Math.random() * 1e4)}`;
  const name = `E2E ToolSettings Org ${stamp}`;
  const toolName = `e2e-tool-${stamp}`;
  const orgId = await createOrg(page, name);

  try {
    // Seed the override via the toggle route (a fresh org has none to render,
    // so there is no Enable button to click yet).
    const created = await page.request.post(
      `/orgs/${orgId}/tool-settings/${toolName}`,
      { form: { enabled: 'true' } },
    );
    expect(created.ok(), `seed override failed (HTTP ${created.status()})`).toBeTruthy();

    await page.goto(`/orgs/${orgId}/settings`);
    await expect(page.getByText(toolName, { exact: true })).toBeVisible();
    await expect(page.getByText('Enabled', { exact: true })).toBeVisible();

    // Toggle off via the Disable form (enabled=false).
    await page.getByRole('button', { name: 'Disable' }).click();
    await page.waitForURL(new RegExp(`/orgs/${orgId}/settings\\?updated=1`));
    await expect(page.getByText('Disabled', { exact: true })).toBeVisible();
    await expect(page.getByText('Enabled', { exact: true })).toHaveCount(0);

    // Delete the override; the row disappears.
    await page.getByRole('button', { name: `Delete ${toolName}` }).click();
    await page.waitForURL(new RegExp(`/orgs/${orgId}/settings\\?updated=1`));
    await expect(page.getByText(toolName, { exact: true })).toHaveCount(0);
  } finally {
    if (bootstrap?.projectId) {
      await page.request
        .post(`/api/projects/${bootstrap.projectId}/activate`)
        .catch(() => {});
    }
    await deleteOrgAndPollGone(page, orgId);
  }
});
