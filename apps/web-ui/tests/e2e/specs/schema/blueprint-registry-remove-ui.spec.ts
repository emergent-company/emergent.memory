import { test, expect, type Page } from '@playwright/test';
import { readBootstrap, createProject } from '../../helpers/bootstrap';
import { removeBlueprint, installBlueprint } from '../../helpers/blueprints';
import { memoryAuthHeaders } from '../../helpers/objects';
import { MEMORY_API_URL } from '../../helpers/tokens';

// Registry remove (gateway/blueprints_handlers.go → POST /blueprints/:id/remove →
// memory DELETE /api/blueprints/:id). The Available list is registry-backed: a
// non-built-in blueprint (origin user/import) that is NOT applied stays in the
// Available list and offers a Remove action; removing it deletes the blueprint
// from the registry. A blueprint that IS applied cannot be removed — memory
// rejects with 409 and the gateway maps that to a friendly
// "uninstall first" flash.
//
// Blueprints are created directly against the memory API (POST /api/blueprints +
// POST /api/blueprints/:id/publish) because the gateway exposes no create route
// and the GitHub import path applies in one step (so it never leaves a
// blueprint sitting unapplied in the Available list). The create is scoped to
// the scratch project via X-Project-ID, and the session's Zitadel access token
// (memory_session cookie) authorizes it — the same pattern object specs use to
// talk to memory directly.
//
// Everything runs on a scratch project; the shared bootstrap project is never
// touched.

const SCRATCH_PREFIX = 'E2E Blueprint Remove ';

function requireBootstrap() {
  const bootstrap = readBootstrap();
  expect(bootstrap, 'setup project must run first').toBeTruthy();
  return bootstrap!;
}

/**
 * Create + publish a project-private (origin "user") blueprint on the scratch
 * project via the memory API. Returns the blueprint id.
 */
async function createRegistryBlueprint(
  page: Page,
  projectId: string,
  name: string,
  version: string,
): Promise<string> {
  const headers = await memoryAuthHeaders(page);
  headers['X-Project-ID'] = projectId;

  const create = await page.request.post(`${MEMORY_API_URL}/api/blueprints`, {
    headers,
    data: {
      name,
      version,
      description: 'e2e registry-remove fixture',
      manifest: { packs: [{ name, version }] },
    },
  });
  if (!create.ok()) {
    throw new Error(`create blueprint failed (HTTP ${create.status()}): ${await create.text()}`);
  }
  const created = (await create.json()) as { id?: string };
  if (!created.id) throw new Error('create blueprint returned no id');

  const publish = await page.request.post(`${MEMORY_API_URL}/api/blueprints/${created.id}/publish`, {
    headers,
  });
  if (!publish.ok()) {
    throw new Error(`publish blueprint failed (HTTP ${publish.status()}): ${await publish.text()}`);
  }
  return created.id;
}

/** Restore the bootstrap project, then drop the scratch project (best-effort). */
async function cleanup(page: Page, projectId: string): Promise<void> {
  const bootstrap = readBootstrap();
  if (bootstrap?.projectId) {
    await page.request.post(`/api/projects/${bootstrap.projectId}/activate`).catch(() => {});
  }
  if (projectId && bootstrap?.orgId) {
    await page.request
      .post(`/projects/delete?projectId=${projectId}&orgId=${bootstrap.orgId}`)
      .catch(() => {});
  }
}

test.describe('Blueprint registry remove', () => {
  test('an uninstalled non-built-in blueprint stays available and is removed from the registry', async ({
    page,
  }) => {
    const bootstrap = requireBootstrap();
    const projectName = `${SCRATCH_PREFIX}${Date.now()}`;
    const blueprintName = `e2e-remove-${Date.now()}`;
    let projectId = '';

    try {
      projectId = await createProject(page, bootstrap.orgId, projectName);
      await createRegistryBlueprint(page, projectId, blueprintName, '1.0.0');

      // Uninstalled → it remains in the Available list, carries NO Built-in
      // badge, and offers a Remove action (non-built-in only).
      await page.goto('/blueprints');
      const row = page.locator(`[data-testid="available-blueprint-${blueprintName}"]`);
      await expect(row).toBeVisible();
      await expect(row.locator(`[data-testid="blueprint-builtin-${blueprintName}"]`)).toHaveCount(0);
      await expect(row.locator('[data-testid^="blueprint-remove-"]')).toHaveCount(1);

      // Remove → confirm → PRG back to /blueprints?removed=1.
      await removeBlueprint(page, blueprintName);

      // It is gone from the Available list.
      await expect(
        page.locator(`[data-testid="available-blueprint-${blueprintName}"]`),
      ).toHaveCount(0);
    } finally {
      await cleanup(page, projectId);
    }
  });

  test('removing an applied blueprint surfaces the uninstall-first error', async ({ page }) => {
    const bootstrap = requireBootstrap();
    const projectName = `${SCRATCH_PREFIX}${Date.now()}`;
    const blueprintName = `e2e-remove-${Date.now()}`;
    let projectId = '';

    try {
      projectId = await createProject(page, bootstrap.orgId, projectName);
      const id = await createRegistryBlueprint(page, projectId, blueprintName, '1.0.0');

      // Apply it so memory rejects deletion with 409.
      await installBlueprint(page, blueprintName);

      // Drive the real remove route; the gateway maps the 409 to a friendly
      // flash and PRG-redirects to /blueprints?err=….
      const resp = await page.request.post(`/blueprints/${id}/remove`, { maxRedirects: 0 });
      expect(resp.status()).toBe(303);
      const location = resp.headers()['location'] ?? '';
      const err = new URL(location, 'http://gateway.invalid').searchParams.get('err') ?? '';
      expect(err).toContain('Uninstall the blueprint before removing it');
    } finally {
      await cleanup(page, projectId);
    }
  });

  // D3 guard: no scratch remove project may survive the run.
  test('leaves no scratch remove projects behind', async ({ page }) => {
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
