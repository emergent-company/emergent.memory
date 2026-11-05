import { test, expect, type Page } from '@playwright/test';
import { readBootstrap, createProject } from '../../helpers/bootstrap';
import { installBlueprint } from '../../helpers/blueprints';
import { memoryAuthHeaders } from '../../helpers/objects';
import { MEMORY_API_URL } from '../../helpers/tokens';
import { goto } from '../../helpers/page';

// Blueprint object-type conflicts (gateway/blueprints_handlers.go + the merge
// review page in blueprints_conflict.templ). Installing/importing/updating a
// blueprint whose object-type name already exists in the project must NOT
// apply: the gateway renders the conflict review page with property-level
// detail and only applies after an explicit "Merge types and install/update".
//
// Fixtures are created directly against the memory API (POST /api/blueprints +
// POST /api/blueprints/:id/publish) because the gateway exposes no create route
// — the same pattern blueprint-registry-remove-ui.spec.ts uses. Each test runs on
// its own scratch project (createProject re-activates the session) and deletes
// that project, plus any private blueprints it created, in cleanup.
//
// State is asserted through the resulting project views:
//   * GET /api/blueprints/installed   — applied blueprint + version
//   * GET /api/blueprints/compiled-types — the merged object-type view
//   * the conflict page's data-testids — the review surface itself

const SCRATCH_PREFIX = 'E2E Blueprint Type Conflict ';

const CONFLICT_PAGE = '[data-testid="blueprint-type-conflict-page"]';
const MERGE_BUTTON = '[data-testid="blueprint-type-conflict-merge"]';
const CANCEL_BUTTON = '[data-testid="blueprint-type-conflict-cancel"]';

const EXISTING_TITLE_MAX = 200;
const INCOMING_TITLE_MAX = 20;

interface AppliedBlueprint {
  blueprintId: string;
  name: string;
  version: string;
}

interface CompiledType {
  name: string;
  properties?: Record<string, unknown>;
  schemaName?: string;
  schemaVersion?: string;
  shadowed?: boolean;
}

type Property = Record<string, unknown>;
type Properties = Record<string, Property>;

function property(label: string, extra: Property = {}): Property {
  return { type: 'string', title: label, ...extra };
}

// Existing project type: `Thing` keeps title (maxLength 200) and status.
const BASE_THING: Properties = {
  title: property('Title', { maxLength: EXISTING_TITLE_MAX }),
  status: property('Status'),
};
// Incoming type: title differs (maxLength 20); description + owner are new.
const CONFLICT_THING: Properties = {
  title: property('Title', { maxLength: INCOMING_TITLE_MAX }),
  description: property('Description'),
  owner: property('Owner'),
};

// A second colliding type so the multi-type case is exercised by the default
// fixture (Gadget: code conflicts, note is added).
const BASE_GADGET: Properties = {
  code: property('Code', { maxLength: 64 }),
};
const CONFLICT_GADGET: Properties = {
  code: property('Code', { maxLength: 8 }),
  note: property('Note'),
};

// A type no other pack declares, for the conflict-free install case.
const UNIQUE_NOTES: Properties = {
  notes: property('Notes'),
};

function manifest(
  packName: string,
  packVersion: string,
  types: Array<{ name: string; label: string; properties: Properties }>,
): Record<string, unknown> {
  return { packs: [{ name: packName, version: packVersion, objectTypes: types }] };
}

function baseManifest(): Record<string, unknown> {
  return manifest('base-pack', '1.0.0', [
    { name: 'Thing', label: 'Thing', properties: BASE_THING },
    { name: 'Gadget', label: 'Gadget', properties: BASE_GADGET },
  ]);
}

function conflictManifest(): Record<string, unknown> {
  return manifest('conflict-pack', '1.0.0', [
    { name: 'Thing', label: 'Thing', properties: CONFLICT_THING },
    { name: 'Gadget', label: 'Gadget', properties: CONFLICT_GADGET },
  ]);
}

function uniqueManifest(): Record<string, unknown> {
  return manifest('unique-pack', '1.0.0', [
    { name: 'UniqueThing', label: 'Unique thing', properties: UNIQUE_NOTES },
  ]);
}

function requireBootstrap() {
  const bootstrap = readBootstrap();
  expect(bootstrap, 'setup project must run first').toBeTruthy();
  return bootstrap!;
}

/**
 * Create + publish a project-private blueprint against the memory API on the
 * scratch project. Returns the blueprint id and records it for cleanup.
 */
async function createPublishedBlueprint(
  page: Page,
  projectId: string,
  name: string,
  version: string,
  blueprintManifest: Record<string, unknown>,
  created: string[],
): Promise<string> {
  const headers = await memoryAuthHeaders(page);
  headers['X-Project-ID'] = projectId;

  const create = await page.request.post(`${MEMORY_API_URL}/api/blueprints`, {
    headers,
    data: { name, version, manifest: blueprintManifest },
  });
  if (!create.ok()) {
    throw new Error(`create blueprint ${name} failed (HTTP ${create.status()}): ${await create.text()}`);
  }
  const createdBody = (await create.json()) as { id?: string };
  if (!createdBody.id) throw new Error(`create blueprint ${name} returned no id`);
  created.push(createdBody.id);

  const publish = await page.request.post(
    `${MEMORY_API_URL}/api/blueprints/${createdBody.id}/publish`,
    { headers },
  );
  if (!publish.ok()) {
    throw new Error(`publish blueprint ${name} failed (HTTP ${publish.status()}): ${await publish.text()}`);
  }
  return createdBody.id;
}

async function appliedBlueprints(page: Page): Promise<AppliedBlueprint[]> {
  const resp = await page.request.get('/api/blueprints/installed');
  expect(resp.ok(), `installed blueprints failed (HTTP ${resp.status()})`).toBeTruthy();
  return (await resp.json()) as AppliedBlueprint[];
}

async function compiledTypes(page: Page): Promise<CompiledType[]> {
  const resp = await page.request.get('/api/blueprints/compiled-types');
  expect(resp.ok(), `compiled-types failed (HTTP ${resp.status()})`).toBeTruthy();
  const data = (await resp.json()) as { objectTypes?: CompiledType[] };
  return data.objectTypes ?? [];
}

/** The effective (non-shadowed) compiled type, if present. */
async function effectiveType(page: Page, name: string): Promise<CompiledType | undefined> {
  return (await compiledTypes(page)).find((t) => t.name === name && !t.shadowed);
}

/**
 * Read the project's *registry* definition for a type through the memory
 * merge-preview endpoint (the gateway exposes no registry read). A probe pack
 * declaring one new property reports the existing schema in its conflict entry;
 * this is the merged definition an object create/validate path actually uses,
 * which the pack-based compiled-types view does not show directly.
 */
async function registryType(
  page: Page,
  projectId: string,
  typeName: string,
): Promise<{ properties?: Record<string, unknown> } | null> {
  const headers = await memoryAuthHeaders(page);
  headers['X-Project-ID'] = projectId;
  const resp = await page.request.post(
    `${MEMORY_API_URL}/api/schemas/projects/${projectId}/merge-preview`,
    {
      headers,
      data: {
        packs: [
          {
            name: `e2e-probe-${Date.now()}`,
            objectTypes: [{ name: typeName, properties: { e2e_probe: property('Probe') } }],
          },
        ],
      },
    },
  );
  expect(resp.ok(), `merge-preview failed (HTTP ${resp.status()})`).toBeTruthy();
  const body = (await resp.json()) as {
    conflicts?: Array<{ existingSchema?: { properties?: Record<string, unknown> } }>;
  };
  return body.conflicts?.[0]?.existingSchema ?? null;
}

async function waitForInstalled(page: Page, blueprintName: string, version?: string): Promise<void> {
  await expect
    .poll(
      async () =>
        (await appliedBlueprints(page)).some(
          (b) => b.name === blueprintName && (!version || b.version === version),
        ),
      { timeout: 20_000 },
    )
    .toBe(true);
}

/** Navigate to the gallery (scratch scope) and click a blueprint row's install action. */
async function clickInstall(
  page: Page,
  blueprintName: string,
  scope: { orgId: string; projectId: string },
): Promise<void> {
  await goto(page, '/blueprints', scope);
  const row = page.locator(`[data-testid="available-blueprint-${blueprintName}"]`);
  await expect(row).toBeVisible();
  await row.locator(`[data-testid="blueprint-install-${blueprintName}"]`).click();
}

/** Install a blueprint fixture through the real gallery row (scratch scope). */
async function installFixture(
  page: Page,
  blueprintName: string,
  scope: { orgId: string; projectId: string },
): Promise<void> {
  await installBlueprint(page, blueprintName, { scope });
  await waitForInstalled(page, blueprintName);
}

/**
 * Restore the bootstrap project, delete the private blueprints this test created
 * (best-effort — an applied blueprint is rejected and falls away with the
 * project), then drop the scratch project.
 */
async function cleanup(page: Page, projectId: string, blueprintIds: string[]): Promise<void> {
  const bootstrap = readBootstrap();
  if (projectId && blueprintIds.length > 0) {
    const headers = await memoryAuthHeaders(page).catch(() => null);
    if (headers) {
      headers['X-Project-ID'] = projectId;
      for (const id of blueprintIds) {
        await page.request
          .delete(`${MEMORY_API_URL}/api/blueprints/${id}`, { headers })
          .catch(() => {});
      }
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

test.describe('Blueprint object-type conflict review', () => {
  test('conflicting install renders the review page, does not apply, and cancel leaves state unchanged', async ({
    page,
  }) => {
    const bootstrap = requireBootstrap();
    const stamp = Date.now();
    const baseName = `e2e-conflict-base-${stamp}`;
    const conflictName = `e2e-conflict-incoming-${stamp}`;
    let projectId = '';
    const created: string[] = [];

    try {
      projectId = await createProject(page, bootstrap.orgId, `${SCRATCH_PREFIX}${stamp}`);
      await createPublishedBlueprint(page, projectId, baseName, '1.0.0', baseManifest(), created);
      await createPublishedBlueprint(page, projectId, conflictName, '1.0.0', conflictManifest(), created);

      // Install the base pack first so Thing + Gadget already exist.
      await installFixture(page, baseName, { orgId: bootstrap.orgId, projectId });

      const appliedBefore = await appliedBlueprints(page);
      const compiledBefore = await compiledTypes(page);
      expect(compiledBefore.some((t) => t.name === 'Thing' && !t.shadowed)).toBe(true);

      // Open the conflict from the real gallery row.
      await clickInstall(page, conflictName, { orgId: bootstrap.orgId, projectId });
      await expect(page.locator(CONFLICT_PAGE)).toBeVisible();

      // Nothing applied while the review page is open.
      expect(await appliedBlueprints(page)).toEqual(appliedBefore);
      expect(await compiledTypes(page)).toEqual(compiledBefore);
      expect(
        (await appliedBlueprints(page)).some((b) => b.name === conflictName),
        'conflict blueprint must not be applied before merge confirmation',
      ).toBe(false);

      // Cancel: back to the gallery, no side effects.
      await page.locator(CANCEL_BUTTON).click();
      await page.waitForURL((u) => u.pathname.endsWith('/blueprints'));
      expect(await appliedBlueprints(page)).toEqual(appliedBefore);
      expect(await compiledTypes(page)).toEqual(compiledBefore);
    } finally {
      await cleanup(page, projectId, created);
    }
  });

  test('conflict page names the type, packs, added properties, and the overridden title', async ({
    page,
  }) => {
    const bootstrap = requireBootstrap();
    const stamp = Date.now();
    const baseName = `e2e-conflict-detail-base-${stamp}`;
    const conflictName = `e2e-conflict-detail-incoming-${stamp}`;
    let projectId = '';
    const created: string[] = [];

    try {
      projectId = await createProject(page, bootstrap.orgId, `${SCRATCH_PREFIX}detail-${stamp}`);
      await createPublishedBlueprint(page, projectId, baseName, '1.0.0', baseManifest(), created);
      await createPublishedBlueprint(page, projectId, conflictName, '1.0.0', conflictManifest(), created);
      await installFixture(page, baseName, { orgId: bootstrap.orgId, projectId });

      await clickInstall(page, conflictName, { orgId: bootstrap.orgId, projectId });
      await expect(page.locator(CONFLICT_PAGE)).toBeVisible();

      const card = page.locator('[data-testid="blueprint-type-conflict-Thing"]');
      await expect(card).toBeVisible();
      await expect(card).toContainText('Thing');
      await expect(card).toContainText('base-pack');
      await expect(card).toContainText('v1.0.0');
      await expect(card).toContainText('conflict-pack');
      await expect(card).toContainText('Existing: base-pack v1.0.0');
      await expect(card).toContainText('Incoming: conflict-pack');

      // New properties are listed individually.
      await expect(
        page.locator('[data-testid="blueprint-conflict-added-Thing-description"]'),
      ).toBeVisible();
      await expect(page.locator('[data-testid="blueprint-conflict-added-Thing-owner"]')).toBeVisible();

      // The conflicting title is called out as keeping the existing definition.
      const overridden = page.locator('[data-testid="blueprint-conflict-overridden-Thing-title"]');
      await expect(overridden).toBeVisible();
      await expect(overridden).toContainText('definition wins');
      // Existing maxLength 200 is rendered; incoming 20 is shown as ignored.
      await expect(overridden).toContainText(String(EXISTING_TITLE_MAX));
      await expect(overridden).toContainText(String(INCOMING_TITLE_MAX));

      await expect(page.locator(MERGE_BUTTON)).toHaveText(/Merge types and install/);

      await page.locator(CANCEL_BUTTON).click();
      await page.waitForURL((u) => u.pathname.endsWith('/blueprints'));
    } finally {
      await cleanup(page, projectId, created);
    }
  });

  test('confirming merge installs the blueprint and the existing definition survives', async ({
    page,
  }) => {
    const bootstrap = requireBootstrap();
    const stamp = Date.now();
    const baseName = `e2e-conflict-merge-base-${stamp}`;
    const conflictName = `e2e-conflict-merge-incoming-${stamp}`;
    let projectId = '';
    const created: string[] = [];

    try {
      projectId = await createProject(page, bootstrap.orgId, `${SCRATCH_PREFIX}merge-${stamp}`);
      await createPublishedBlueprint(page, projectId, baseName, '1.0.0', baseManifest(), created);
      await createPublishedBlueprint(page, projectId, conflictName, '1.0.0', conflictManifest(), created);
      await installFixture(page, baseName, { orgId: bootstrap.orgId, projectId });

      // The helper detects the conflict page and confirms the merge.
      await installBlueprint(page, conflictName, { scope: { orgId: bootstrap.orgId, projectId } });
      await waitForInstalled(page, conflictName);

      // Compiled view: the effective Thing now carries the incoming
      // description/owner, and the base pack's Thing entry still holds the
      // project's existing title definition.
      await expect
        .poll(
          async () => {
            const thing = await effectiveType(page, 'Thing');
            return Object.keys(thing?.properties ?? {}).sort();
          },
          { timeout: 20_000 },
        )
        .toEqual(expect.arrayContaining(['description', 'owner']));

      const compiled = await compiledTypes(page);
      const existingThing = compiled.find(
        (t) => t.name === 'Thing' && t.schemaName === 'base-pack',
      );
      expect(existingThing?.properties?.title).toEqual({
        type: 'string',
        title: 'Title',
        maxLength: EXISTING_TITLE_MAX,
      });

      // The merged registry definition (what object create/validate uses) keeps
      // the existing title while adding the incoming properties.
      await expect
        .poll(async () => {
          const merged = await registryType(page, projectId, 'Thing');
          const properties = merged?.properties ?? {};
          return {
            keys: Object.keys(properties).sort(),
            title: properties.title,
          };
        }, { timeout: 20_000 })
        .toEqual({
          keys: ['description', 'owner', 'status', 'title'],
          title: { type: 'string', title: 'Title', maxLength: EXISTING_TITLE_MAX },
        });
    } finally {
      await cleanup(page, projectId, created);
    }
  });

  test('a conflict-free blueprint installs directly without the review page', async ({ page }) => {
    const bootstrap = requireBootstrap();
    const stamp = Date.now();
    const uniqueName = `e2e-conflict-unique-${stamp}`;
    let projectId = '';
    const created: string[] = [];

    try {
      projectId = await createProject(page, bootstrap.orgId, `${SCRATCH_PREFIX}unique-${stamp}`);
      await createPublishedBlueprint(page, projectId, uniqueName, '1.0.0', uniqueManifest(), created);

      await installBlueprint(page, uniqueName, { scope: { orgId: bootstrap.orgId, projectId } });
      await waitForInstalled(page, uniqueName);

      await expect(page.locator(CONFLICT_PAGE)).toHaveCount(0);
      await expect.poll(async () => Boolean(await effectiveType(page, 'UniqueThing'))).toBe(true);
    } finally {
      await cleanup(page, projectId, created);
    }
  });

  test('every conflicting object type is listed on the review page', async ({ page }) => {
    const bootstrap = requireBootstrap();
    const stamp = Date.now();
    const baseName = `e2e-conflict-multi-base-${stamp}`;
    const conflictName = `e2e-conflict-multi-incoming-${stamp}`;
    let projectId = '';
    const created: string[] = [];

    try {
      projectId = await createProject(page, bootstrap.orgId, `${SCRATCH_PREFIX}multi-${stamp}`);
      await createPublishedBlueprint(page, projectId, baseName, '1.0.0', baseManifest(), created);
      await createPublishedBlueprint(page, projectId, conflictName, '1.0.0', conflictManifest(), created);
      await installFixture(page, baseName, { orgId: bootstrap.orgId, projectId });

      await clickInstall(page, conflictName, { orgId: bootstrap.orgId, projectId });
      await expect(page.locator(CONFLICT_PAGE)).toBeVisible();

      await expect(page.locator('[data-testid="blueprint-type-conflict-Thing"]')).toBeVisible();
      await expect(page.locator('[data-testid="blueprint-type-conflict-Gadget"]')).toBeVisible();
      await expect(
        page.locator('[data-testid="blueprint-conflict-overridden-Gadget-code"]'),
      ).toBeVisible();
      await expect(
        page.locator('[data-testid="blueprint-conflict-added-Gadget-note"]'),
      ).toBeVisible();

      await page.locator(CANCEL_BUTTON).click();
      await page.waitForURL((u) => u.pathname.endsWith('/blueprints'));
    } finally {
      await cleanup(page, projectId, created);
    }
  });

  test('a same-pack update/re-apply does not render the conflict page', async ({ page }) => {
    const bootstrap = requireBootstrap();
    const stamp = Date.now();
    const blueprintName = `e2e-conflict-same-pack-${stamp}`;
    let projectId = '';
    const created: string[] = [];

    try {
      projectId = await createProject(page, bootstrap.orgId, `${SCRATCH_PREFIX}same-pack-${stamp}`);

      // v1 installs pack `same-pack`; v2 is a newer version of the same blueprint
      // carrying a newer `same-pack` version. Same pack name → never a conflict.
      await createPublishedBlueprint(
        page,
        projectId,
        blueprintName,
        '1.0.0',
        manifest('same-pack', '1.0.0', [
          { name: 'Thing', label: 'Thing', properties: BASE_THING },
        ]),
        created,
      );
      await installFixture(page, blueprintName, { orgId: bootstrap.orgId, projectId });

      await createPublishedBlueprint(
        page,
        projectId,
        blueprintName,
        '2.0.0',
        manifest('same-pack', '2.0.0', [
          {
            name: 'Thing',
            label: 'Thing',
            properties: { ...BASE_THING, extra: property('Extra') },
          },
        ]),
        created,
      );

      // The published v2 surfaces as an Upgrade row on the gallery. Click it and
      // require the direct installed landing — a conflict page would keep us on
      // /blueprints and time out.
      await goto(page, '/blueprints', { orgId: bootstrap.orgId, projectId });
      const row = page.locator(`[data-testid="available-blueprint-${blueprintName}"]`);
      await expect(row).toBeVisible();
      const upgrade = row.locator(`[data-testid="blueprint-install-${blueprintName}"]`);
      await expect(upgrade).toHaveText(/Upgrade/);
      await upgrade.click();

      const outcome = await Promise.race([
        page
          .waitForURL(
            (u) => u.pathname.endsWith('/blueprints') && u.searchParams.get('installed') === '1',
            { timeout: 20_000 },
          )
          .then(() => 'installed' as const),
        page
          .locator(CONFLICT_PAGE)
          .waitFor({ state: 'visible', timeout: 20_000 })
          .then(() => 'conflict' as const),
      ]);
      expect(outcome, 'same-pack re-apply must not require merge confirmation').toBe('installed');

      await waitForInstalled(page, blueprintName, '2.0.0');
    } finally {
      await cleanup(page, projectId, created);
    }
  });

  test('cancel returns to the gallery and leaves the blueprint unapplied', async ({ page }) => {
    const bootstrap = requireBootstrap();
    const stamp = Date.now();
    const baseName = `e2e-conflict-cancel-base-${stamp}`;
    const conflictName = `e2e-conflict-cancel-incoming-${stamp}`;
    let projectId = '';
    const created: string[] = [];

    try {
      projectId = await createProject(page, bootstrap.orgId, `${SCRATCH_PREFIX}cancel-${stamp}`);
      await createPublishedBlueprint(page, projectId, baseName, '1.0.0', baseManifest(), created);
      await createPublishedBlueprint(page, projectId, conflictName, '1.0.0', conflictManifest(), created);
      await installFixture(page, baseName, { orgId: bootstrap.orgId, projectId });

      await clickInstall(page, conflictName, { orgId: bootstrap.orgId, projectId });
      await expect(page.locator(CONFLICT_PAGE)).toBeVisible();
      await page.locator(CANCEL_BUTTON).click();

      await page.waitForURL((u) => u.pathname.endsWith('/blueprints'));
      const url = new URL(page.url());
      expect(url.searchParams.get('installed')).toBeNull();
      await expect(
        page.locator(`[data-testid="available-blueprint-${conflictName}"]`),
      ).toBeVisible();
      expect(
        (await appliedBlueprints(page)).some((b) => b.name === conflictName),
        'cancelled conflict must leave the blueprint unapplied',
      ).toBe(false);
    } finally {
      await cleanup(page, projectId, created);
    }
  });

  // D3 guard: no scratch project this spec created may survive the run.
  test('leaves no scratch conflict projects behind', async ({ page }) => {
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
