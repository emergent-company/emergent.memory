import { test, expect, Page } from '@playwright/test';
import { readBootstrap } from '../../helpers/bootstrap';
import { expectAppPage } from '../../helpers/page';

// Transfers a project from the bootstrap org ("E2E Main") into a destination
// org, verifying the org-view Transfer action end to end (success flash +
// reparent via API). Self-cleaning: probe project and destination org are
// deleted in `finally`, so repeated runs stay idempotent.
const DEST_ORG = 'E2E Transfer Dest';

async function createOrg(page: Page, name: string): Promise<string> {
  const resp = await page.request.post('/api/orgs', { data: { name } });
  if (!resp.ok()) throw new Error(`createOrg ${name} failed: ${resp.status()} ${await resp.text()}`);
  return (await resp.json()).id as string;
}

async function listOrgs(page: Page): Promise<Array<{ id: string; name: string }>> {
  const resp = await page.request.get('/api/orgs');
  if (!resp.ok()) throw new Error(`list orgs failed: ${resp.status()}`);
  return (await resp.json()) as Array<{ id: string; name: string }>;
}

async function createProject(page: Page, orgId: string, name: string): Promise<string> {
  const resp = await page.request.post('/api/projects', { data: { name, orgId } });
  if (!resp.ok()) throw new Error(`createProject failed: ${resp.status()} ${await resp.text()}`);
  return (await resp.json()).id as string;
}

// The Transfer action lives inside the per-row native popover menu, which is
// display:none until its trigger is clicked — mirror the real user flow.
async function openRowMenuAndClickTransfer(page: Page, projectName: string) {
  const row = page
    .locator('tr')
    .filter({ has: page.locator(`button[data-project-name="${projectName}"]`) });
  await row.locator('[data-gd-popover-trigger]').click();
  const btn = row.locator(`button[data-project-name="${projectName}"]`);
  await expect(btn).toBeVisible();
  await btn.click();
}

test('transfers a project to another org from the org view', async ({ page }) => {
  const bootstrap = readBootstrap();
  expect(bootstrap, 'setup project must run first').toBeTruthy();
  const sourceOrgId = bootstrap!.orgId;
  const sourceOrgName = bootstrap!.orgName;

  const stamp = Date.now();
  const probeName = `E2E Transfer Probe ${stamp}`;
  let destOrgId = '';
  let createdDestOrg = false;
  let probeProjectId = '';

  const orgs = await listOrgs(page);
  const existingDest = orgs.find((o) => o.name === DEST_ORG);
  destOrgId = existingDest?.id ?? (await createOrg(page, DEST_ORG));
  createdDestOrg = !existingDest;

  try {
    probeProjectId = await createProject(page, sourceOrgId, probeName);

    await page.goto(`/orgs/${sourceOrgId}`);
    await expectAppPage(page, new RegExp(sourceOrgName));
    await expect(page.getByRole('heading', { name: sourceOrgName })).toBeVisible();

    // The Transfer row action exists for the probe project.
    await expect(page.locator(`button[data-project-name="${probeName}"]`)).toHaveCount(1);

    // Dialog lists the destination org and excludes the source org.
    await openRowMenuAndClickTransfer(page, probeName);
    const dialog = page.locator('#transfer-project-modal');
    await expect(dialog).toBeVisible();
    await expect(dialog.locator('#transfer-project-name')).toHaveText(probeName);
    const optionLabels = await dialog.locator('#transfer-destination-org option').allTextContents();
    expect(optionLabels).toContain(DEST_ORG);
    expect(optionLabels).not.toContain(sourceOrgName);

    // Cancel is a no-op.
    await dialog.getByRole('button', { name: 'Cancel' }).click();
    await expect(dialog).not.toBeVisible();
    await expect(page.locator(`button[data-project-name="${probeName}"]`)).toHaveCount(1);

    // Submit the transfer.
    await openRowMenuAndClickTransfer(page, probeName);
    await expect(dialog).toBeVisible();
    await dialog.locator('#transfer-destination-org').selectOption({ label: DEST_ORG });
    const respPromise = page.waitForResponse(
      (r) => r.url().includes('/projects/transfer'),
      { timeout: 20_000 },
    );
    await dialog.getByRole('button', { name: 'Transfer', exact: true }).click();
    const resp = await respPromise;
    await page.waitForLoadState('domcontentloaded');

    // Redirected back to the source org with the success flash.
    await expect(page.getByRole('heading', { name: sourceOrgName })).toBeVisible({
      timeout: 15_000,
    });
    await expect
      .poll(async () => {
        const b = await page.locator('body').innerText();
        return /moved to /.test(b);
      }, { timeout: 8000, intervals: [200, 300, 500] })
      .toBeTruthy();

    // The project now lives under the destination org.
    const projectsResp = await page.request.get('/api/projects');
    expect(projectsResp.ok()).toBeTruthy();
    const projects = (await projectsResp.json()) as Array<{ id: string; orgId: string }>;
    const moved = projects.find((p) => p.id === probeProjectId);
    expect(moved, `probe project missing after transfer: ${projectsResp.status()}`).toBeTruthy();
    expect(moved!.orgId).toBe(destOrgId);
    expect(resp.status()).toBe(303);
  } finally {
    // Best-effort cleanup: delete the probe project from whichever org it now
    // sits in, then the destination org if we created it.
    if (probeProjectId) {
      for (const orgId of [sourceOrgId, destOrgId]) {
        await page.request
          .post(`/projects/delete?projectId=${probeProjectId}&orgId=${orgId}`)
          .catch(() => {});
        const stillListed = await page.request.get('/api/projects');
        const projects = stillListed.ok()
          ? ((await stillListed.json()) as Array<{ id: string }>)
          : [];
        if (!projects.some((p) => p.id === probeProjectId)) break;
      }
    }
    if (destOrgId && createdDestOrg) {
      await page.request.post(`/orgs/${destOrgId}/delete`).catch(() => {});
    }
  }
});
