import { test, expect } from '@playwright/test';
import { readBootstrap } from '../../helpers/bootstrap';
import { expectAppPage } from '../../helpers/page';

// /orgs/:id/settings is the org Settings hub: an in-page side rail links the
// Tools section (/orgs/:id/settings) and the Danger zone
// (/orgs/:id/settings/danger-zone). Both sections keep the "Settings" browser
// title (data-testid page-settings). The pre-hub page URL
// /orgs/:id/tool-settings now redirects permanently (301) to the hub.
// Render/navigation only — the delete-org confirm flow itself is covered by
// org-delete-ui.spec.ts and is deliberately not duplicated here.

test.describe('Org settings hub', () => {

test('org settings hub renders Tools + Danger zone sections with the side rail', async ({ page }) => {
  const bootstrap = readBootstrap();
  expect(bootstrap, 'setup project must run first').toBeTruthy();
  const orgId = bootstrap!.orgId;
  const hub = `/orgs/${orgId}/settings`;
  const dangerZone = `${hub}/danger-zone`;

  await page.goto(hub);
  await expectAppPage(page, /Settings/);
  await expect(page).toHaveURL(new RegExp(`/orgs/${orgId}/settings$`));

  // Side rail links point at the hub sections.
  const rail = page.getByRole('navigation', { name: 'Organization settings sections' });
  const toolsLink = rail.getByRole('link', { name: 'Tools' });
  await expect(toolsLink).toBeVisible();
  await expect(toolsLink).toHaveAttribute('href', hub);
  const dangerLink = rail.getByRole('link', { name: 'Danger zone' });
  await expect(dangerLink).toBeVisible();
  await expect(dangerLink).toHaveAttribute('href', dangerZone);

  // Tools content: the org Settings page header is rendered.
  await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible();

  // Navigate into the Danger zone section: same Settings title + page anchor,
  // and the delete-org confirm form is present (locator mirrors
  // org-delete-ui.spec.ts).
  await dangerLink.click();
  await page.waitForURL(new RegExp(`/orgs/${orgId}/settings/danger-zone$`));
  await expectAppPage(page, /Settings/);
  await expect(page.getByTestId('page-settings')).toBeVisible();
  await expect(page.locator(`form[action="/orgs/${orgId}/delete"]`)).toHaveCount(1);
  await expect(page.getByRole('button', { name: 'Delete organization' })).toBeVisible();
});

test('legacy /tool-settings page URL redirects 301 into the settings hub', async ({ page }) => {
  const bootstrap = readBootstrap();
  expect(bootstrap, 'setup project must run first').toBeTruthy();
  const orgId = bootstrap!.orgId;
  const legacy = `/orgs/${orgId}/tool-settings`;

  // Browser navigation follows the redirect; assert the final landing page.
  await page.goto(legacy);
  await expect(page).toHaveURL(new RegExp(`/orgs/${orgId}/settings$`));
  await expectAppPage(page, /Settings/);

  // No tool-settings link/URL reference survives anywhere on the rendered page.
  await expect(page.locator('a[href*="tool-settings"]')).toHaveCount(0);
  await expect(page.locator('body')).not.toContainText('tool-settings');

  // The intermediate hop is a permanent redirect: with redirects disabled the
  // raw response exposes the 301 + Location header.
  const resp = await page.request.get(legacy, { maxRedirects: 0 });
  expect(resp.status()).toBe(301);
  expect(resp.headers().location).toBe(`/orgs/${orgId}/settings`);
});
});
