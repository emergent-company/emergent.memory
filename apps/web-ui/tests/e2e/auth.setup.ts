import { test as setup, expect } from '@playwright/test';
import { login } from './helpers/auth';
import {
  createOrg,
  createProject,
  configureLiveProvider,
  hasLiveProviderCreds,
  writeBootstrap,
  LIVE_PROVIDER,
  LIVE_PROVIDER_BASE_URL,
} from './helpers/bootstrap';
import { STORAGE_STATE } from './constants/storage';
import {
  importGitHubBlueprint,
  PERSONAL_MEMORY_BLUEPRINT_URL,
  PERSONAL_MEMORY_BLUEPRINT_REF,
  PERSONAL_MEMORY_BLUEPRINT_VERSION,
} from './helpers/blueprints';

// The GitHub import can take longer than a default test timeout (the backend
// allows the archive fetch alone up to 60s); give setup a matching budget.
setup.setTimeout(180_000);

// Stable tenant identity — reused across runs so the org/project are created
// once, not re-created (and deleted) every run. Force a clean slate by running
// with E2E_RESET=1.
const ORG_NAME = 'E2E Main';
const PROJECT_NAME = 'E2E Main Project';
// Schema pack that defines object types (Person, Task, …) so object create /
// schema object-type detail / blueprint detail are testable. The pack is no
// longer bundled with the binary — it is imported from a public GitHub repo.
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
  // save, so a fake key 401s. Configure the real dev provider from env
  // (E2E_SCENARIO_LLM_*) so provider-gated specs have a configured provider;
  // when the creds are unset (default CI/local run) leave the project
  // provider-less with a note — those specs then skip rather than fail setup.
  if (hasLiveProviderCreds()) {
    try {
      await configureLiveProvider(page);
      console.log(
        `[setup] configured live provider '${LIVE_PROVIDER}' (base_url ${LIVE_PROVIDER_BASE_URL})`,
      );
    } catch (e) {
      console.warn(`[setup] provider config failed, continuing without it: ${(e as Error).message}`);
    }
  } else {
    console.warn(
      '[setup] provider config skipped: set E2E_SCENARIO_LLM_API_KEY ' +
        '(and optionally E2E_SCENARIO_LLM_BASE_URL) to configure a live-validated provider',
    );
  }

  // Seed the schema pack (idempotent) so object types exist. It ships from the
  // external GitHub blueprint repo, so install via the Import-from-GitHub path.
  // Match on name AND version: a stale personal-memory v2.x already in the
  // workspace must not satisfy the guard and let the suite run against the wrong
  // pack while silently bypassing PERSONAL_MEMORY_BLUEPRINT_REF (#1529).
  const installed = (await (
    await page.request.get('/api/blueprints/installed')
  ).json()) as Array<{ blueprintId: string; name: string; version?: string }>;
  const seeded = (installed ?? []).find((b) => b.name === SCHEMA_PACK);
  if (!seeded) {
    await importGitHubBlueprint(page, PERSONAL_MEMORY_BLUEPRINT_URL, {
      ref: PERSONAL_MEMORY_BLUEPRINT_REF,
      // Navigate scoped: the bootstrap file is not written yet (writeBootstrap
      // runs below), so pass the org/project ids resolved above explicitly.
      scope: { orgId, projectId },
    });
  } else if (seeded.version !== PERSONAL_MEMORY_BLUEPRINT_VERSION) {
    throw new Error(
      `setup: installed "${SCHEMA_PACK}" is version ${seeded.version ?? '(unknown)'}, but this ` +
        `suite pins ${PERSONAL_MEMORY_BLUEPRINT_REF} (blueprint version ` +
        `${PERSONAL_MEMORY_BLUEPRINT_VERSION}). A stale pack would run the suite against the ` +
        `wrong schema. Reset the e2e tenant — re-run once with E2E_RESET=1 to delete it, then ` +
        `run again — or uninstall/purge the stale "${SCHEMA_PACK}" blueprint.`,
    );
  }

  writeBootstrap({ orgId, projectId, orgName: ORG_NAME, projectName: PROJECT_NAME });

  await page.context().storageState({ path: STORAGE_STATE });
});
