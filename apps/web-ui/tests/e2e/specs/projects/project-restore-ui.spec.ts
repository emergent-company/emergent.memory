import { test, expect, Page } from '@playwright/test';
import { readBootstrap, createOrg, createProject } from '../../helpers/bootstrap';
import { expectAppPage } from '../../helpers/page';

// Focused coverage for the restore half of the soft-delete project lifecycle
// (org_context.templ → POST /projects/restore, uiRestoreProject). The
// schedule/bulk flows live in the sibling projects-table-ui.spec.ts; this spec
// is the single-project counterpart: schedule one scratch project for deletion,
// assert the page shows it as pending (and the API drops it from the active
// list), then restore it from its row menu and assert it is active again.
//
// Every test seeds its own scratch org + project (never the bootstrap tenant)
// and deletes that org in `finally`, polling /api/orgs until the async cascade
// clears it — repeated runs stay idempotent.

interface ProjectRef {
  id: string;
  name: string;
  orgId: string;
  deletionStatus?: string;
  deletionScheduledFor?: string;
}

async function listOrgs(page: Page): Promise<Array<{ id: string; name: string }>> {
  const resp = await page.request.get('/api/orgs');
  if (!resp.ok()) throw new Error(`list orgs failed: ${resp.status()}`);
  return (await resp.json()) as Array<{ id: string; name: string }>;
}

// GET /api/projects returns the *active* projects only: a project scheduled for
// deletion is absent until it is restored. That makes membership here a clean
// resulting-state check for the restore flow.
async function listActiveProjects(page: Page): Promise<ProjectRef[]> {
  const resp = await page.request.get('/api/projects');
  if (!resp.ok()) throw new Error(`list projects failed: ${resp.status()}`);
  return (await resp.json()) as ProjectRef[];
}

// rowForProject locates the data-table row rendering a project by its name. It
// is deliberately not keyed on the delete checkbox: a project pending deletion
// still renders a row, but with a disabled placeholder checkbox and a
// "Scheduled for deletion" badge instead of the named one.
function rowForProject(page: Page, projectName: string) {
  return page.locator('tr').filter({ has: page.getByText(projectName, { exact: true }) });
}

// openRowMenu mirrors the real user flow: each row's actions live in a native
// popover, hidden until its labelled trigger is clicked. The trigger carries a
// stable accessible name ("Actions for <project>"), so the menu itself is an
// ARIA menu — no implementation attribute is needed to reach it.
async function openRowMenu(page: Page, projectName: string) {
  const row = rowForProject(page, projectName);
  await row.getByRole('button', { name: `Actions for ${projectName}` }).click();
  const menu = row.getByRole('menu');
  await expect(menu).toBeVisible();
  return menu;
}

function deleteDialog(page: Page) {
  return page.locator('#project-delete-modal');
}

// deleteOrgAndPollGone drops the scratch org and waits for the async project
// cascade to clear it before the run finishes.
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

test.describe('Project restore', () => {
  test('restores a project scheduled for deletion and clears its pending state', async ({ page }) => {
    const bootstrap = readBootstrap();
    expect(bootstrap, 'setup project must run first').toBeTruthy();

    const stamp = `${Date.now()}-${Math.floor(Math.random() * 1e4)}`;
    const orgName = `E2E Restore Org ${stamp}`;
    const projectName = `E2E Restore Project ${stamp}`;
    const orgId = await createOrg(page, orgName);
    const projectId = await createProject(page, orgId, projectName);

    try {
      await page.goto(`/orgs/${orgId}`);
      await expectAppPage(page, new RegExp(orgName));
      await expect(page.getByRole('heading', { name: orgName })).toBeVisible();

      // Start active: the named checkbox and the project name are both present.
      const activeRow = rowForProject(page, projectName);
      await expect(activeRow.locator('button.truncate.text-left')).toHaveText(projectName);
      await expect(page.locator(`input[aria-label="Select ${projectName}"]`)).toHaveCount(1);
      await expect(activeRow.getByText('Scheduled for deletion', { exact: true })).toHaveCount(0);

      // Schedule deletion from the row menu → shared confirmation dialog →
      // native POST /projects/delete → PRG back to the org landing.
      const menu = await openRowMenu(page, projectName);
      await menu.getByRole('button', { name: 'Delete', exact: true }).click();

      const dialog = deleteDialog(page);
      await expect(dialog).toBeVisible();
      await expect(dialog.getByRole('heading', { name: `Delete ${projectName}?` })).toBeVisible();

      const deleteResp = page.waitForResponse(
        (r) => r.url().includes('/projects/delete') && r.request().method() === 'POST',
        { timeout: 20_000 },
      );
      await dialog.getByRole('button', { name: 'Delete', exact: true }).click();
      await deleteResp;
      await page.waitForURL(new RegExp(`/orgs/${orgId}\\?deleted=1`), { timeout: 20_000 });
      await page.waitForLoadState('domcontentloaded');

      // Resulting state: soft-deleted. The row stays but is flagged pending,
      // its named checkbox gives way to a disabled placeholder, and the API no
      // longer lists it as an active project.
      const pendingRow = rowForProject(page, projectName);
      await expect(pendingRow.getByText('Scheduled for deletion', { exact: true })).toBeVisible();
      await expect(page.locator(`input[aria-label="Select ${projectName}"]`)).toHaveCount(0);
      await expect(
        page.locator(`input[aria-label="${projectName} is scheduled for deletion and cannot be selected"]`),
      ).toHaveCount(1);
      await expect
        .poll(
          async () => (await listActiveProjects(page)).some((p) => p.id === projectId),
          { timeout: 20_000, intervals: [200, 300, 500] },
        )
        .toBe(false);

      // Restore from the pending row's menu (its only action is "Cancel
      // deletion") → POST /projects/restore → PRG with the cancel flash.
      const pendingMenu = await openRowMenu(page, projectName);
      const restoreResp = page.waitForResponse(
        (r) => r.url().includes('/projects/restore') && r.request().method() === 'POST',
        { timeout: 20_000 },
      );
      await pendingMenu.getByRole('button', { name: 'Cancel deletion', exact: true }).click();
      await restoreResp;
      await page.waitForURL(new RegExp(`/orgs/${orgId}\\?restored=1`), { timeout: 20_000 });
      await page.waitForLoadState('domcontentloaded');

      // Resulting state: active again. The badge is gone, the named checkbox is
      // back, the row is openable, and the API lists it as an active project.
      const restoredRow = rowForProject(page, projectName);
      await expect(restoredRow.getByText('Scheduled for deletion', { exact: true })).toHaveCount(0);
      await expect(restoredRow.locator('button.truncate.text-left')).toHaveText(projectName);
      await expect(page.locator(`input[aria-label="Select ${projectName}"]`)).toHaveCount(1);
      await expect
        .poll(
          async () => {
            const p = (await listActiveProjects(page)).find((x) => x.id === projectId);
            return p ? (p.deletionStatus ?? 'active') : 'missing';
          },
          { timeout: 20_000, intervals: [200, 300, 500] },
        )
        .not.toBe('pending_deletion');
    } finally {
      // Put the bootstrap project back as the session's active project, then
      // drop the scratch org (its cascade removes whatever the UI left behind).
      if (bootstrap?.projectId) {
        await page.request
          .post(`/api/projects/${bootstrap.projectId}/activate`)
          .catch(() => {});
      }
      await deleteOrgAndPollGone(page, orgId).catch((e) =>
        console.warn(`[project-restore-ui] cleanup skipped: ${(e as Error).message}`),
      );
    }
  });
});
