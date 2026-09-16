import { test, expect, Page } from '@playwright/test';
import { readBootstrap, createOrg, createProject } from '../../helpers/bootstrap';
import { expectAppPage } from '../../helpers/page';

// Org-landing projects data table (org_context.templ): a bulk-delete toolbar
// driven by a select-all header checkbox, per-row popover action menus
// (Open / Transfer / Delete), and one shared styled confirmation dialog
// (`#project-delete-modal`, projectDeleteDialog) behind both the row Delete and
// the bulk toolbar. Deletion is async + soft: the project stays on the page as a
// pending row ("Scheduled for deletion" badge, no delete checkbox) and can be
// cancelled from its row menu ("Cancel deletion", POST /projects/restore). Every
// test seeds its own scratch org + projects (never the bootstrap tenant) and
// deletes that org in `finally`, polling /api/orgs until the async cascade
// clears it — repeated runs stay idempotent.

async function listOrgs(page: Page): Promise<Array<{ id: string; name: string }>> {
  const resp = await page.request.get('/api/orgs');
  if (!resp.ok()) throw new Error(`list orgs failed: ${resp.status()}`);
  return (await resp.json()) as Array<{ id: string; name: string }>;
}

async function listOrgProjectIds(page: Page, orgId: string): Promise<string[]> {
  const resp = await page.request.get('/api/projects');
  if (!resp.ok()) throw new Error(`list projects failed: ${resp.status()}`);
  const projects = (await resp.json()) as Array<{ id: string; orgId: string }>;
  return projects.filter((p) => p.orgId === orgId).map((p) => p.id);
}

// One scratch org + N uniquely-named projects, stamped so reruns never collide.
// A mid-seed failure removes the org so nothing leaks past this call.
async function seedScratchOrg(
  page: Page,
  prefix: string,
  count: number,
): Promise<{ orgId: string; orgName: string; projectNames: string[]; projectIds: string[] }> {
  const stamp = `${Date.now()}-${Math.floor(Math.random() * 1e4)}`;
  const orgName = `${prefix} ${stamp}`;
  const orgId = await createOrg(page, orgName);
  const projectNames = Array.from(
    { length: count },
    (_, i) => `${prefix} Project ${stamp}-${i + 1}`,
  );
  const projectIds: string[] = [];
  try {
    for (const name of projectNames) {
      projectIds.push(await createProject(page, orgId, name));
    }
  } catch (e) {
    await page.request.post(`/orgs/${orgId}/delete`).catch(() => {});
    throw e;
  }
  return { orgId, orgName, projectNames, projectIds };
}

// rowForProject locates the data-table row rendering a project by its name. It
// is deliberately not keyed on the delete checkbox: a project pending deletion
// still renders a row, but with a disabled placeholder checkbox and a
// "Scheduled for deletion" badge instead of the named one.
function rowForProject(page: Page, projectName: string) {
  return page.locator('tr').filter({ has: page.getByText(projectName, { exact: true }) });
}

// Mirror the real user flow: the per-row actions live in a native popover that
// is display:none until its trigger is clicked. Works for both a deletable row
// and a pending one (whose only action is "Cancel deletion").
async function openRowMenu(page: Page, projectName: string) {
  const row = rowForProject(page, projectName);
  await row.locator('[data-gd-popover-trigger]').click();
  const menu = row.locator('[data-gd-popover-content]');
  await expect(menu).toBeVisible();
  return { menu };
}

// deleteDialog is the single styled confirmation modal behind both the row
// Delete action and the bulk toolbar (projectDeleteDialog). Its form posts to
// /projects/delete; the page script fills the heading and the hidden projectId
// fields before showing it.
function deleteDialog(page: Page) {
  return page.locator('#project-delete-modal');
}

// confirmDeletion clicks the dialog's destructive action and waits for the
// native (non-boosted) POST /projects/delete round-trip.
async function confirmDeletion(page: Page) {
  const dialog = deleteDialog(page);
  const resp = page.waitForResponse(
    (r) => r.url().includes('/projects/delete') && r.request().method() === 'POST',
    { timeout: 20_000 },
  );
  await dialog.getByRole('button', { name: 'Delete', exact: true }).click();
  await resp;
}

// expectFlash polls the rendered page for a PRG flash string. The redirect
// lands on the org landing, which re-renders the toast from the query param.
async function expectFlash(page: Page, text: string) {
  await expect
    .poll(
      async () => (await page.locator('body').innerText()).replace(/\s+/g, ' '),
      { timeout: 8000, intervals: [200, 300, 500] },
    )
    .toContain(text);
}

// Org deletion cascades projects asynchronously in the memory service — poll
// until the org disappears from /api/orgs before this run finishes.
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

test.describe('Projects table', () => {

test('schedules every project for deletion via select-all + the dialog, then cancels one', async ({ page }) => {
  const bootstrap = readBootstrap();
  expect(bootstrap, 'setup project must run first').toBeTruthy();

  const { orgId, orgName, projectNames } = await seedScratchOrg(
    page,
    'E2E Table Bulk',
    3,
  );

  try {
    await page.goto(`/orgs/${orgId}`);
    await expectAppPage(page, new RegExp(orgName));
    await expect(page.getByRole('heading', { name: orgName })).toBeVisible();

    // The projects table bulk form: the one carrying the select-all control.
    // (The styled dialog owns a second /projects/delete form, so scope by the
    // select-all checkbox rather than the action alone.)
    const form = page
      .locator('form[action="/projects/delete"]')
      .filter({ has: page.locator('input[aria-label="Select all projects"]') });
    await expect(form).toHaveCount(1);
    await expect(form.locator('input[type="hidden"][name="orgId"]')).toHaveValue(orgId);

    // Every seeded project renders as a data-table row. Scope to the delete
    // form: the topbar project switcher also carries hidden projectId inputs.
    await expect(form.locator('input[name="projectId"]')).toHaveCount(projectNames.length);
    for (const name of projectNames) {
      const row = rowForProject(page, name);
      await expect(row.locator('button.truncate.text-left')).toHaveText(name);
    }

    const selectAll = form.locator('input[aria-label="Select all projects"]');
    const toolbar = form.locator('div[x-show="count > 0"]');
    const submit = toolbar.locator('button[type="submit"]');
    const hint = page.getByText('Select projects to delete.', { exact: true });

    // Nothing selected: hint shows, the bulk-delete toolbar is x-show-hidden.
    // Assert hidden first so Alpine has applied its init state before clicking.
    await expect(submit).toBeHidden();
    await expect(hint).toBeVisible();

    // Select-all checks every row checkbox and reveals the count label.
    await selectAll.click();
    await expect(selectAll).toBeChecked();
    await expect(form.locator('input[name="projectId"]:checked')).toHaveCount(
      projectNames.length,
    );
    await expect(toolbar).toBeVisible();
    await expect(submit).toContainText('Delete');
    await expect(submit).toContainText(`${projectNames.length} selected`);
    await expect(hint).toBeHidden();

    // Clicking the toolbar opens the styled confirmation dialog — it does not
    // submit directly. The dialog lists the selected names and injects one
    // hidden projectId input per selected project into its own form.
    await submit.click();
    const dialog = deleteDialog(page);
    await expect(dialog).toBeVisible();
    await expect(
      dialog.getByRole('heading', {
        name: `Delete ${projectNames.length} selected projects?`,
      }),
    ).toBeVisible();
    await expect(dialog.locator('#project-delete-ids input[name="projectId"]')).toHaveCount(
      projectNames.length,
    );

    // Confirm → native POST /projects/delete → PRG back to the org landing with
    // the scheduled-deletion flash (was "deletions started").
    await confirmDeletion(page);
    await page.waitForURL(new RegExp(`/orgs/${orgId}\\?deleted=${projectNames.length}`), {
      timeout: 20_000,
    });
    await page.waitForLoadState('domcontentloaded');
    await expectFlash(page, `${projectNames.length} projects scheduled for deletion.`);

    // Soft delete: every row is still rendered, now pending. The select-all
    // header checkbox is gone and the toolbar hint flips to the all-pending note.
    for (const name of projectNames) {
      const row = rowForProject(page, name);
      await expect(row.getByText('Scheduled for deletion', { exact: true })).toBeVisible();
    }
    await expect(page.locator('input[aria-label="Select all projects"]')).toHaveCount(0);
    await expect(form.locator('input[name="projectId"]')).toHaveCount(0);
    await expect(
      page.getByText('Every project is scheduled for deletion.', { exact: true }),
    ).toBeVisible();
    await expect(page.getByText('this list updates automatically')).toBeVisible();

    // Cancel one project's deletion from its row menu (pending rows expose only
    // "Cancel deletion"); POST /projects/restore → PRG with the cancel flash.
    const [restoreName] = projectNames;
    const pendingMenu = (await openRowMenu(page, restoreName)).menu;
    const restoreResp = page.waitForResponse(
      (r) => r.url().includes('/projects/restore') && r.request().method() === 'POST',
      { timeout: 20_000 },
    );
    await pendingMenu.getByRole('button', { name: 'Cancel deletion', exact: true }).click();
    await restoreResp;
    await page.waitForURL(new RegExp(`/orgs/${orgId}\\?restored=1`), { timeout: 20_000 });
    await page.waitForLoadState('domcontentloaded');
    await expectFlash(page, 'Deletion cancelled.');

    // The cancelled project is active again: named checkbox back, badge gone.
    await expect(page.locator(`input[aria-label="Select ${restoreName}"]`)).toHaveCount(1);
    await expect(
      rowForProject(page, restoreName).getByText('Scheduled for deletion', { exact: true }),
    ).toHaveCount(0);
  } finally {
    await deleteOrgAndPollGone(page, orgId).catch((e) =>
      console.warn(`[projects-table-ui] cleanup skipped: ${(e as Error).message}`),
    );
  }
});

test('deletes one project via its row menu, shows pending, then cancels deletion', async ({ page }) => {
  const bootstrap = readBootstrap();
  expect(bootstrap, 'setup project must run first').toBeTruthy();

  const { orgId, orgName, projectNames, projectIds } = await seedScratchOrg(
    page,
    'E2E Table Row',
    2,
  );
  const [delName, openName] = projectNames;

  try {
    await page.goto(`/orgs/${orgId}`);
    await expectAppPage(page, new RegExp(orgName));
    await expect(page.getByRole('heading', { name: orgName })).toBeVisible();
    for (const name of projectNames) {
      const row = rowForProject(page, name);
      await expect(row.locator('button.truncate.text-left')).toHaveText(name);
    }

    // The row popover menu holds Open, Delete (and Transfer when the bootstrap
    // org is a transfer candidate — ignore it). Delete opens the shared styled
    // confirmation dialog rather than a native confirm().
    const { menu } = await openRowMenu(page, delName);
    await expect(menu.getByRole('button', { name: 'Open', exact: true })).toBeVisible();
    const deleteBtn = menu.getByRole('button', { name: 'Delete', exact: true });
    await deleteBtn.click();

    const dialog = deleteDialog(page);
    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole('heading', { name: `Delete ${delName}?` })).toBeVisible();

    // Confirm → native POST /projects/delete → PRG with the scheduled flash.
    await confirmDeletion(page);
    await page.waitForURL(new RegExp(`/orgs/${orgId}\\?deleted=1`), { timeout: 20_000 });
    await page.waitForLoadState('domcontentloaded');
    await expectFlash(page, 'Project scheduled for deletion.');

    // Soft delete: the project does NOT disappear. Its row remains but is
    // pending — badge shown, named delete checkbox replaced by a disabled one.
    const pendingRow = rowForProject(page, delName);
    await expect(pendingRow.getByText('Scheduled for deletion', { exact: true })).toBeVisible();
    await expect(page.locator(`input[aria-label="Select ${delName}"]`)).toHaveCount(0);
    await expect(
      page.locator(`input[aria-label="${delName} is scheduled for deletion and cannot be selected"]`),
    ).toHaveCount(1);

    // Cancel the deletion from the pending row's menu → restore is live again.
    const pendingMenu = (await openRowMenu(page, delName)).menu;
    const restoreResp = page.waitForResponse(
      (r) => r.url().includes('/projects/restore') && r.request().method() === 'POST',
      { timeout: 20_000 },
    );
    await pendingMenu.getByRole('button', { name: 'Cancel deletion', exact: true }).click();
    await restoreResp;
    await page.waitForURL(new RegExp(`/orgs/${orgId}\\?restored=1`), { timeout: 20_000 });
    await page.waitForLoadState('domcontentloaded');
    await expectFlash(page, 'Deletion cancelled.');

    // Both projects are active again: the restored project regains its named
    // checkbox and loses the badge (the API also lists both, once active).
    await expect(page.locator(`input[aria-label="Select ${delName}"]`)).toHaveCount(1);
    await expect(
      rowForProject(page, delName).getByText('Scheduled for deletion', { exact: true }),
    ).toHaveCount(0);
    await expect
      .poll(
        async () => {
          const ids = await listOrgProjectIds(page, orgId);
          return projectIds.every((id) => ids.includes(id));
        },
        { timeout: 20_000, intervals: [200, 300, 500] },
      )
      .toBe(true);

    // Row-menu Open activates the project (session switch) → lands on /agents.
    const openMenu = (await openRowMenu(page, openName)).menu;
    const activateResp = page.waitForResponse(
      (r) => r.url().includes('/projects/activate') && r.request().method() === 'POST',
      { timeout: 20_000 },
    );
    await openMenu.getByRole('button', { name: 'Open', exact: true }).click();
    await activateResp;
    await page.waitForURL(/\/agents/, { timeout: 20_000 });
    await expect(page.getByTestId('project-switcher')).toContainText(openName, {
      timeout: 15_000,
    });
  } finally {
    // Put the bootstrap project back as the session's active project, then
    // drop the scratch org (its cascade removes whatever the UI left behind).
    if (bootstrap?.projectId) {
      await page.request
        .post(`/api/projects/${bootstrap.projectId}/activate`)
        .catch(() => {});
    }
    await deleteOrgAndPollGone(page, orgId).catch((e) =>
      console.warn(`[projects-table-ui] cleanup skipped: ${(e as Error).message}`),
    );
  }
});
});
