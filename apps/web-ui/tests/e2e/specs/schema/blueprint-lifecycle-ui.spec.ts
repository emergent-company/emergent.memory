import { test, expect, type Page } from '@playwright/test';
import { readBootstrap, createProject } from '../../helpers/bootstrap';
import { installBlueprint } from '../../helpers/blueprints';

// Blueprint enable + unapply (gateway/blueprints_handlers.go → POST
// /blueprints/install, POST /blueprints/:id/unapply). The bootstrap project
// already has `personal-memory` applied, so every test runs on its OWN scratch
// project (created via POST /api/projects, which also activates it for the
// session) and deletes that project — plus anything it installed — in cleanup.
// The shared bootstrap org / `E2E Main Project` are never touched.
//
// Assertions are on resulting state, not the one-shot PRG flash:
//   * `GET /api/blueprints/compiled-types` is the merged compiled view the
//     object-create form itself reads — object/relationship types are the
//     observable contract of an applied pack.
//   * `/objects/new`'s `select[name="type"]` is the user-facing proof the type
//     is now (or no longer) offered in this project.
// Counts/ordering are never asserted — the tenant is shared.

const BLUEPRINT = 'agent-notes';
// agent-notes v2.0.0 defines these two object types (plus the annotates /
// belongs_to_cluster / supersedes relationship types).
const OBJECT_TYPES = ['Note', 'NoteCluster'];
const SCRATCH_PREFIX = 'E2E Blueprint Lifecycle ';

interface CompiledType {
  name: string;
}

interface AppliedBlueprint {
  blueprintId: string;
  name: string;
  version: string;
}

function requireBootstrap() {
  const bootstrap = readBootstrap();
  expect(bootstrap, 'setup project must run first').toBeTruthy();
  return bootstrap!;
}

async function compiledObjectTypes(page: Page): Promise<string[]> {
  const resp = await page.request.get('/api/blueprints/compiled-types');
  expect(resp.ok(), `compiled-types failed (HTTP ${resp.status()})`).toBeTruthy();
  const data = (await resp.json()) as { objectTypes?: CompiledType[] };
  return (data.objectTypes ?? []).map((t) => t.name);
}

async function appliedBlueprints(page: Page): Promise<AppliedBlueprint[]> {
  const resp = await page.request.get('/api/blueprints/installed');
  expect(resp.ok(), `installed blueprints failed (HTTP ${resp.status()})`).toBeTruthy();
  return (await resp.json()) as AppliedBlueprint[];
}

/** Restore the bootstrap project, then drop the scratch project (best-effort). */
async function cleanup(page: Page, projectId: string): Promise<void> {
  const bootstrap = readBootstrap();
  // Unapply anything this spec installed while the scratch project is still
  // active; the project delete below also cascades its applied state.
  if (projectId) {
    const applied = await appliedBlueprints(page).catch(() => [] as AppliedBlueprint[]);
    const bp = applied.find((b) => b.name === BLUEPRINT);
    if (bp) {
      await page.request
        .post(`/api/blueprints/${bp.blueprintId}/unapply`, { data: {} })
        .catch(() => {});
    }
  }
  if (bootstrap?.projectId) {
    await page.request.post(`/api/projects/${bootstrap.projectId}/activate`).catch(() => {});
  }
  if (projectId && bootstrap?.orgId) {
    await page.request
      .post(`/projects/delete?projectId=${projectId}&orgId=${bootstrap.orgId}`)
      .catch(() => {});
  }
}

test.describe('Blueprint enable + unapply', () => {
  test('installing a bundled blueprint adds its object types and unapplying removes them', async ({
    page,
  }) => {
    const bootstrap = requireBootstrap();
    const projectName = `${SCRATCH_PREFIX}${Date.now()}`;
    let projectId = '';

    try {
      projectId = await createProject(page, bootstrap.orgId, projectName);

      // Pre-state: a fresh project offers none of the pack's types (it only has
      // the built-in session/message types).
      await expect
        .poll(async () => (await compiledObjectTypes(page)).filter((n) => OBJECT_TYPES.includes(n)))
        .toEqual([]);

      // Install through the real gallery form (POST /blueprints/install with the
      // bundled pack's hidden name input; the helper waits out the PRG redirect).
      await installBlueprint(page, BLUEPRINT);

      // Resulting state: the pack is applied and every declared object type is
      // in the project's compiled view.
      await expect
        .poll(async () => (await appliedBlueprints(page)).some((b) => b.name === BLUEPRINT), {
          timeout: 20_000,
        })
        .toBe(true);
      await expect
        .poll(async () => {
          const names = await compiledObjectTypes(page);
          return OBJECT_TYPES.filter((t) => names.includes(t)).sort();
        }, { timeout: 20_000 })
        .toEqual([...OBJECT_TYPES].sort());

      // User-facing proof: the object-create form now offers the type. Asserting
      // the <option> count (selectOption would also work) keeps this a
      // resulting-state check rather than a render smoke test.
      await page.goto('/objects/new');
      await expect(page.locator('select[name="type"] option[value="Note"]')).toHaveCount(1);

      // Unapply through the real row action on the gallery: the applied row's
      // destructive submit carries aria-label "Remove <name>" (POST
      // /blueprints/:id/unapply, PRG back to /blueprints).
      await page.goto('/blueprints');
      const removeButton = page.getByRole('button', { name: `Remove ${BLUEPRINT}` });
      await expect(removeButton).toBeVisible();
      const unapplyResponse = page.waitForResponse(
        (r) => r.url().includes('/unapply') && r.request().method() === 'POST',
        { timeout: 20_000 },
      );
      await removeButton.click();
      expect((await unapplyResponse).status()).toBeLessThan(400);

      // Resulting state: the pack's types are gone from the compiled view and it
      // is no longer applied.
      await expect
        .poll(async () => (await compiledObjectTypes(page)).some((n) => OBJECT_TYPES.includes(n)), {
          timeout: 20_000,
        })
        .toBe(false);
      await expect
        .poll(async () => (await appliedBlueprints(page)).some((b) => b.name === BLUEPRINT), {
          timeout: 20_000,
        })
        .toBe(false);

      // And the object-create form no longer offers the type.
      await page.goto('/objects/new');
      await expect(page.locator('select[name="type"] option[value="Note"]')).toHaveCount(0);
    } finally {
      await cleanup(page, projectId);
    }
  });

  // D3 guard: no scratch project this spec created may survive the run.
  test('leaves no scratch lifecycle projects behind', async ({ page }) => {
    requireBootstrap();
    await expect
      .poll(
        async () => {
          const resp = await page.request.get('/api/projects');
          expect(resp.ok()).toBeTruthy();
          const projects = (await resp.json()) as Array<{ name: string }>;
          return projects
            .map((p) => p.name)
            .filter((n) => n.startsWith(SCRATCH_PREFIX))
            .sort();
        },
        { timeout: 20_000, intervals: [200, 300, 500] },
      )
      .toEqual([]);
  });
});
