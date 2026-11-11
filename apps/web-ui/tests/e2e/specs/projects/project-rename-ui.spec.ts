import { test, expect, Page } from '@playwright/test';
import { readBootstrap, createOrg, createProject } from '../../helpers/bootstrap';
import { expectAppPage, goto } from '../../helpers/page';

// Focused coverage for the Rename row action on the org landing
// (org_context.templ → POST /projects/rename, uiRenameProject): open the row
// menu, confirm the dialog is prefilled with the project's current name, cancel
// as a no-op, then rename and assert the success flash plus the API's new name.
//
// Seeds its own scratch org + project (never the bootstrap tenant) and deletes
// that org in `finally`, polling /api/orgs until the async cascade clears it —
// repeated runs stay idempotent.

async function listOrgs(page: Page): Promise<Array<{ id: string; name: string }>> {
  const resp = await page.request.get('/api/orgs');
  if (!resp.ok()) throw new Error(`list orgs failed: ${resp.status()}`);
  return (await resp.json()) as Array<{ id: string; name: string }>;
}

async function listProjects(page: Page): Promise<Array<{ id: string; name: string }>> {
  const resp = await page.request.get('/api/projects');
  if (!resp.ok()) throw new Error(`list projects failed: ${resp.status()}`);
  return (await resp.json()) as Array<{ id: string; name: string }>;
}

function rowForProject(page: Page, projectName: string) {
  return page.locator('tr').filter({ has: page.getByText(projectName, { exact: true }) });
}

// openRowMenu mirrors the real user flow: each row's actions live in a native
// popover, hidden until its labelled trigger is clicked.
async function openRowMenu(page: Page, projectName: string) {
  const row = rowForProject(page, projectName);
  await row.getByRole('button', { name: `Actions for ${projectName}` }).click();
  const menu = row.getByRole('menu');
  await expect(menu).toBeVisible();
  return menu;
}

function renameDialog(page: Page) {
  return page.locator('#rename-project-modal');
}

async function deleteOrgAndPollGone(page: Page, orgId: string) {
  await page.request.post(`/orgs/${orgId}/delete`).catch(() => {});
  await expect
    .poll(
      async () => {
        const orgs = await listOrgs(page);
        return orgs.some((o) => o.id === orgId);
      },
      { timeout: 20_000, intervals: [200, 300, 500] },
    )
    .toBe(false);
}

test.describe('Project rename', () => {
  test('renames a project from its row menu', async ({ page }) => {
    const bootstrap = readBootstrap();
    expect(bootstrap, 'setup project must run first').toBeTruthy();

    const stamp = Date.now();
    const orgName = `E2E Rename Org ${stamp}`;
    const oldName = `E2E Rename Old ${stamp}`;
    const newName = `E2E Rename New ${stamp}`;
    let orgId = '';
    let projectId = '';

    try {
      orgId = await createOrg(page, orgName);
      projectId = await createProject(page, orgId, oldName);

      await goto(page, `/orgs/${orgId}`);
      await expectAppPage(page, new RegExp(orgName));
      await expect(page.getByRole('heading', { name: orgName })).toBeVisible();

      // Rename is a row-menu action, next to Open.
      const menu = await openRowMenu(page, oldName);
      await expect(menu.getByRole('button', { name: 'Rename', exact: true })).toBeVisible();

      // The dialog names the project and prefills its current name.
      await menu.getByRole('button', { name: 'Rename', exact: true }).click();
      const dialog = renameDialog(page);
      await expect(dialog).toBeVisible();
      await expect(dialog.locator('#rename-project-name')).toHaveText(oldName);
      await expect(dialog.locator('#rename-project-input')).toHaveValue(oldName);

      // Cancel is a no-op: still the old name, no request submitted.
      await dialog.getByRole('button', { name: 'Cancel' }).click();
      await expect(dialog).not.toBeVisible();
      // Scope to the row: the dialog keeps the name it was filled with in the
      // DOM after close, so a page-wide text match resolves to two elements.
      await expect(rowForProject(page, oldName)).toBeVisible();

      // Submit the rename.
      const reopened = await openRowMenu(page, oldName);
      await reopened.getByRole('button', { name: 'Rename', exact: true }).click();
      await expect(dialog).toBeVisible();
      await dialog.locator('#rename-project-input').fill(newName);
      const respPromise = page.waitForResponse(
        (r) => r.url().includes('/projects/rename'),
        { timeout: 20_000 },
      );
      await dialog.getByRole('button', { name: 'Rename', exact: true }).click();
      const resp = await respPromise;
      await page.waitForLoadState('domcontentloaded');

      // Redirected back to the org landing with the success flash and the new
      // name on the row.
      await expect(page.getByRole('heading', { name: orgName })).toBeVisible({
        timeout: 15_000,
      });
      await expect
        .poll(
          async () => {
            const body = await page.locator('body').innerText();
            return /Project renamed\./.test(body);
          },
          { timeout: 8000, intervals: [200, 300, 500] },
        )
        .toBeTruthy();
      await expect(rowForProject(page, newName)).toBeVisible();
      expect(resp.status()).toBe(303);

      // The API agrees: the same project now carries the new name.
      const projects = await listProjects(page);
      const renamed = projects.find((p) => p.id === projectId);
      expect(renamed, `probe project missing after rename: ${projectId}`).toBeTruthy();
      expect(renamed!.name).toBe(newName);
    } finally {
      if (orgId) await deleteOrgAndPollGone(page, orgId);
    }
  });
});
