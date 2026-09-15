import { test, expect } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

test('creates an org via the form and deletes it', async ({ page }) => {
  const orgName = `E2E UI Org ${Date.now()}`;
  let orgId = '';
  let deletedViaUi = false;

  try {
    await page.goto('/orgs/new');
    await expectAppPage(page, /New organization/);

    await page.locator('#new-org-name').fill(orgName);
    await page.getByRole('button', { name: 'Create organization' }).click();

    // Redirects to the new org landing /orgs/:id, which renders the org name.
    await page.waitForURL(/\/orgs\/[0-9a-f-]{36}/);
    orgId = new URL(page.url()).pathname.split('/').pop()!;
    await expect(page.getByRole('heading', { name: orgName })).toBeVisible();

    // Delete through the UI too: org Settings hub → danger zone. The delete
    // form is gated by a confirm() dialog — accept it.
    await page.goto(`/orgs/${orgId}/settings`);
    await page.getByRole('link', { name: 'Danger zone' }).click();
    page.on('dialog', (d) => d.accept());
    await page.getByRole('button', { name: 'Delete organization' }).click();

    // uiDeleteOrg clears context and redirects to the org wizard/list.
    await page.waitForURL(/\/orgs/);
    await expect(page.getByText(orgName)).toHaveCount(0);
    deletedViaUi = true;
  } finally {
    // Fallback cleanup only if the UI delete never completed (e.g. assertion
    // failed earlier), so dev memory doesn't accumulate orgs.
    if (orgId && !deletedViaUi) await page.request.post(`/orgs/${orgId}/delete`).catch(() => {});
  }
});
