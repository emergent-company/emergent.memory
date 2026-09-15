import { test, expect } from '@playwright/test';

test('deletes an org via the danger zone', async ({ page }) => {
  const name = `E2E UI Del Org ${Date.now()}`;

  const resp = await page.request.post('/api/orgs', { data: { name } });
  expect(resp.ok(), `create org failed: ${await resp.text()}`).toBeTruthy();
  const org = await resp.json();
  const orgId = org.id as string;

  // The delete-org danger zone lives in the org Settings hub.
  await page.goto(`/orgs/${orgId}/settings`);
  await page.getByRole('link', { name: 'Danger zone' }).click();

  // The delete form is gated by a confirm() dialog — accept it.
  page.on('dialog', (d) => d.accept());
  await page.getByRole('button', { name: 'Delete organization' }).click();

  // uiDeleteOrg clears context and redirects to the org wizard.
  await page.waitForURL(/\/orgs/);
  await expect(page.getByText(name)).toHaveCount(0);
});
