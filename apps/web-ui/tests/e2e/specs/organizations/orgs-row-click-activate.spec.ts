import { test, expect, Locator, Page } from '@playwright/test';
import { readBootstrap, createOrg, createProject } from '../../helpers/bootstrap';
import { expectAppPage } from '../../helpers/page';

// Each project inside an org tree row on /orgs is a submit button POSTing
// /projects/activate (hidden projectId input), so a row click opens the
// project. Because /orgs is org-scoped, a switch from there lands on /agents
// (never bounces back to /orgs), and the topbar project switcher then shows
// the activated project. Self-cleaning: the scratch org is deleted (and the
// bootstrap project re-activated) in `finally`.

/** The org tree row for `orgName` on /orgs (org name in the p.font-semibold). */
function orgTreeRow(page: Page, orgName: string): Locator {
  return page
    .locator('main div.divide-y.divide-base-200 > div')
    .filter({ has: page.getByText(orgName, { exact: true }) });
}

/** The full-row project submit button for `projectName` inside that org's row. */
function projectRowButton(page: Page, orgName: string, projectName: string): Locator {
  return orgTreeRow(page, orgName).getByRole('button', { name: projectName });
}

test.describe('Org list project activation', () => {

test('clicking a scratch project row on /orgs activates it and lands on /agents', async ({ page }) => {
  const bootstrap = readBootstrap();
  expect(bootstrap, 'setup project must run first').toBeTruthy();

  const stamp = Date.now();
  const orgName = `E2E Row Click Org ${stamp}`;
  const projectName = `E2E Row Click Project ${stamp}`;
  let orgId = '';

  try {
    orgId = await createOrg(page, orgName);
    const projectId = await createProject(page, orgId, projectName);

    await page.goto('/orgs');
    await expectAppPage(page, /Organizations/);

    // The scratch org tree row shows the project as a full-row submit form
    // carrying the projectId; its button's accessible name is the project name.
    const row = orgTreeRow(page, orgName);
    await expect(row).toHaveCount(1);
    const btn = projectRowButton(page, orgName, projectName);
    await expect(btn).toHaveAttribute('title', `Open ${projectName}`);
    await expect(
      row.locator('form[action="/projects/activate"] input[name="projectId"]'),
    ).toHaveValue(projectId);

    // Clicking the row activates the project and lands on /agents (the Referer
    // is the org-scoped /orgs page, so the switch must not bounce back).
    await btn.click();
    await page.waitForURL(/\/agents$/, { timeout: 20_000 });
    await expectAppPage(page, /Agents/);

    // The topbar project switcher now shows the activated scratch project.
    await expect(page.getByTestId('project-switcher')).toContainText(projectName, {
      timeout: 15_000,
    });
  } finally {
    // Restore the shared bootstrap context, then remove the scratch org.
    if (bootstrap) {
      await page.request
        .post(`/api/projects/${bootstrap.projectId}/activate`)
        .catch(() => {});
    }
    if (orgId) {
      await expect
        .poll(async () => {
          await page.request.post(`/orgs/${orgId}/delete`).catch(() => {});
          const resp = await page.request.get('/api/orgs');
          if (!resp.ok()) return false;
          const orgs = (await resp.json()) as Array<{ id: string }>;
          return !orgs.some((o) => o.id === orgId);
        }, { timeout: 15_000, intervals: [200, 300, 500] })
        .toBe(true);
    }
  }
});

test('clicking the bootstrap project row opens /agents with it still active', async ({ page }) => {
  const bootstrap = readBootstrap();
  expect(bootstrap, 'setup project must run first').toBeTruthy();

  await page.goto('/orgs');
  await expectAppPage(page, /Organizations/);

  // Cheap sanity pass over the same row-click path with no scratch fixtures.
  const row = orgTreeRow(page, bootstrap!.orgName);
  await expect(row).toHaveCount(1);
  const btn = projectRowButton(page, bootstrap!.orgName, bootstrap!.projectName);
  await expect(btn).toBeVisible();

  await btn.click();
  await page.waitForURL(/\/agents$/, { timeout: 20_000 });
  await expectAppPage(page, /Agents/);
  await expect(page.getByTestId('project-switcher')).toContainText(bootstrap!.projectName, {
    timeout: 15_000,
  });
});
});
