import { test, expect } from '@playwright/test';
import { readBootstrap } from '../../helpers/bootstrap';
import { expectAppPage, goto } from '../../helpers/page';
import { orgScopedPath, scopedPath } from '../../helpers/scope';

// Scoped URLs (scoped-web-urls): a project page is served at
// /o/{org}/p/{project}/…, an org page at /o/{org}. When a page is loaded under
// a scoped URL, every internal link must keep the scope. Runs in the dev
// harness (no session), where the scope prefix is still stripped before routing
// and the static project is used — this verifies the URL shape and link
// scoping, not per-tab data isolation (that is covered by the gateway's
// per-request resolver tests).

test.describe('Scoped URLs', () => {
  test('project page is served at /o/{org}/p/{project} with scoped links', async ({ page }) => {
    const bootstrap = readBootstrap();
    expect(bootstrap, 'setup project must run first').toBeTruthy();
    const { orgId, projectId, projectName } = bootstrap!;

    await goto(page, scopedPath('/agents'));
    await expectAppPage(page, /Agents/);
    await expect(page).toHaveURL(new RegExp(`/o/${orgId}/p/${projectId}/agents$`));

    // The topbar switcher reflects the scoped project.
    await expect(page.getByTestId('project-switcher')).toContainText(projectName);

    // Sidebar navigation keeps the scope base.
    await expect(
      page.getByRole('link', { name: 'Objects', exact: true }).first(),
    ).toHaveAttribute('href', scopedPath('/objects'));
    await expect(
      page.getByRole('link', { name: 'Chat', exact: true }).first(),
    ).toHaveAttribute('href', scopedPath('/chat'));
  });

  test('org landing is served at /o/{org} with scoped links', async ({ page }) => {
    const bootstrap = readBootstrap();
    expect(bootstrap, 'setup project must run first').toBeTruthy();
    const { orgId, orgName } = bootstrap!;

    await goto(page, orgScopedPath(orgId));
    // The org landing titles itself with the org name (not a static label).
    await expectAppPage(page, new RegExp(orgName));
    await expect(page).toHaveURL(new RegExp(`/o/${orgId}$`));
    await expect(page.getByRole('heading', { name: orgName })).toBeVisible();

    await expect(
      page.getByRole('link', { name: 'Settings', exact: true }).first(),
    ).toHaveAttribute('href', orgScopedPath(orgId, '/settings'));
  });
});
