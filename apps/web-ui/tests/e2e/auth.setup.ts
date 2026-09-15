import { test as setup, expect } from '@playwright/test';
import { login } from './helpers/auth';
import {
  createOrg,
  createProject,
  configureProvider,
  writeBootstrap,
} from './helpers/bootstrap';
import { STORAGE_STATE } from './constants/storage';

// Stable tenant identity — reused across runs so the org/project are created
// once, not re-created (and deleted) every run. Force a clean slate by running
// with E2E_RESET=1.
const ORG_NAME = 'E2E Main';
const PROJECT_NAME = 'E2E Main Project';
// Bundled schema pack that defines object types (Person, Task, …) so object
// create / schema object-type detail / blueprint detail are testable.
const SCHEMA_PACK = 'personal-memory';

interface OrgRef {
  id: string;
  name: string;
}
interface ProjectRef {
  id: string;
  name: string;
  orgId: string;
}

setup('authenticate and reuse-or-create tenant', async ({ page }) => {
  await login(page);

  // Reuse-or-create the org.
  const orgsResp = await page.request.get('/api/orgs');
  expect(
    orgsResp.ok(),
    `session not established after login (HTTP ${orgsResp.status()})`,
  ).toBeTruthy();
  const orgs = (await orgsResp.json()) as OrgRef[];
  let orgId = orgs.find((o) => o.name === ORG_NAME)?.id;
  if (!orgId) orgId = await createOrg(page, ORG_NAME);

  // Reuse-or-create the project in that org.
  const projectsResp = await page.request.get('/api/projects');
  expect(projectsResp.ok(), `list projects failed (HTTP ${projectsResp.status()})`).toBeTruthy();
  const projects = (await projectsResp.json()) as ProjectRef[];
  const projectId =
    projects.find((p) => p.orgId === orgId && p.name === PROJECT_NAME)?.id ??
    (await createProject(page, orgId, PROJECT_NAME));

  // Make sure the session's active project is the (possibly reused) one.
  const act = await page.request.post(`/api/projects/${projectId}/activate`);
  expect(act.ok(), `activate project failed (HTTP ${act.status()})`).toBeTruthy();

  // Provider config is best-effort: the backend runs a live generate test on
  // save, so a fake key 401s. Supply a real key (or skip) — the form flow is
  // covered by the provider UI spec.
  try {
    await configureProvider(page, 'deepseek', 'sk-e2e-test-key');
  } catch (e) {
    console.warn(`[setup] provider config skipped: ${(e as Error).message}`);
  }

  // Seed a bundled schema pack (idempotent) so object types exist.
  const installed = (await (
    await page.request.get('/api/blueprints/installed')
  ).json()) as Array<{ blueprintId: string; name: string }>;
  if (!(installed ?? []).some((b) => b.name === SCHEMA_PACK)) {
    const inst = await page.request.post('/api/blueprints/install', {
      data: { source: 'bundled', name: SCHEMA_PACK },
    });
    expect(inst.ok(), `install schema pack failed: ${await inst.text()}`).toBeTruthy();
  }

  writeBootstrap({ orgId, projectId, orgName: ORG_NAME, projectName: PROJECT_NAME });

  await page.context().storageState({ path: STORAGE_STATE });
});
