import { test, expect, type Page } from '@playwright/test';
import { openObjectForm, submitObjectForm } from '../../helpers/objects';
import { readBootstrap } from '../../helpers/bootstrap';
import { MEMORY_API_URL } from '../../helpers/tokens';

// Object relationships — POST /objects/:id/relationships (uiObjectRelationshipCreate).
//
// Drives the real "Connect object" dialog on an object's detail page: the empty
// state's "Connect this object to another" CTA opens the native <dialog>, the
// relationship-type select is populated from the project's compiled
// relationship types, and the target picker (#connect-dst) autocompletes through
// the /objects/search datalist. Submitting posts to
// POST /objects/:id/relationships and PRG-redirects back to the source object.
//
// The assertion is the resulting state on BOTH pages: the edge is listed in the
// "Relationships" section of the source (Task → Person) and of the target
// (Person, incoming) — never a transient toast.
//
// Cleanup: objects (and the edges touching them) are deleted through the memory
// API with the signed-in user's own Zitadel access token. The gateway exposes no
// object-delete route, so this is the only self-cleanup path; see the run
// report. Every object this spec creates is deleted in `finally`, pass or fail.

const escapeRe = (s: string): string => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

/** The Relationships section on an object detail page (the dialog lives outside it). */
function relationshipsSection(page: Page) {
  return page
    .locator('section')
    .filter({ has: page.getByRole('heading', { name: 'Relationships' }) });
}

/**
 * Create a typed object through the real create form and return its id (the
 * PRG redirect target parsed by the shared helper).
 */
async function createTypedObject(page: Page, type: string, key: string): Promise<string> {
  await openObjectForm(page, type);
  await page.locator('#object-key').fill(key);
  return submitObjectForm(page);
}

/**
 * Auth headers for the memory API: the signed-in user's Zitadel access token
 * (carried inside the gateway's memory_session cookie) plus the active project
 * id. The gateway does not proxy graph-object deletes, so the spec talks to the
 * memory API directly — the same origin the token-lifecycle specs probe.
 */
async function memoryAuthHeaders(page: Page): Promise<Record<string, string>> {
  const cookies = await page.context().cookies();
  const session = cookies.find((c) => c.name === 'memory_session');
  if (!session) throw new Error('memory_session cookie missing from the browser context');
  const payload = JSON.parse(
    Buffer.from(session.value.split('.')[0].replace(/-/g, '+').replace(/_/g, '/'), 'base64').toString(
      'utf8',
    ),
  ) as { access_token?: string };
  if (!payload.access_token) throw new Error('memory_session cookie carries no access_token');
  const bootstrap = readBootstrap();
  return {
    Authorization: `Bearer ${payload.access_token}`,
    'X-Project-ID': bootstrap?.projectId ?? '',
  };
}

/** Ids of the relationships touching an object (memory API edges response). */
async function edgeIdsOf(
  page: Page,
  headers: Record<string, string>,
  objectId: string,
): Promise<string[]> {
  const resp = await page.request
    .get(`${MEMORY_API_URL}/api/graph/objects/${encodeURIComponent(objectId)}/edges`, { headers })
    .catch(() => null);
  if (!resp || !resp.ok()) return [];
  const body = (await resp.json().catch(() => ({}))) as {
    incoming?: Array<{ id?: string }>;
    outgoing?: Array<{ id?: string }>;
  };
  return [...(body.incoming ?? []), ...(body.outgoing ?? [])]
    .map((r) => r.id)
    .filter((id): id is string => Boolean(id));
}

/** Best-effort cleanup: remove edges touching the objects, then the objects. */
async function cleanupObjects(page: Page, objectIds: string[]): Promise<void> {
  try {
    const headers = await memoryAuthHeaders(page);
    const ids = objectIds.filter(Boolean);
    for (const id of ids) {
      for (const edgeId of await edgeIdsOf(page, headers, id)) {
        await page.request
          .delete(`${MEMORY_API_URL}/api/graph/relationships/${encodeURIComponent(edgeId)}`, {
            headers,
            failOnStatusCode: false,
          })
          .catch(() => undefined);
      }
    }
    for (const id of ids) {
      await page.request
        .delete(`${MEMORY_API_URL}/api/graph/objects/${encodeURIComponent(id)}`, {
          headers,
          failOnStatusCode: false,
        })
        .catch(() => undefined);
    }
  } catch {
    // Best-effort only — never mask the spec's real failure.
  }
}

test('object relationships: an edge created from the connect dialog is visible from both objects', async ({
  page,
}) => {
  test.setTimeout(120_000);
  const stamp = Date.now();
  const taskKey = `E2E Rel Task ${stamp}`;
  const personKey = `E2E Rel Person ${stamp}`;
  let taskId = '';
  let personId = '';

  try {
    // Two scratch objects. personal-memory's `assigned_to` relationship type is
    // Task → Person, so the Task is the source and the Person the target.
    taskId = await createTypedObject(page, 'Task', taskKey);
    personId = await createTypedObject(page, 'Person', personKey);

    // Open the source object: no edges yet, so the empty state's Connect CTA
    // (the only button with this aria-label) opens the dialog.
    await page.goto(`/objects/${taskId}`);
    const relSection = relationshipsSection(page);
    await expect(relSection.getByText('No relationships')).toBeVisible();

    await page.getByRole('button', { name: 'Connect this object to another' }).click();
    const dialog = page.locator('#object-connect-modal');
    await expect(dialog).toBeVisible();

    // The dialog is pre-seeded with this object as the source.
    await page.locator('#connect-type').selectOption('assigned_to');
    await expect(page.locator('#connect-src')).toHaveValue(taskId);

    // The target picker autocompletes via /objects/search (datalist options
    // carry the object id as value and its label as text).
    await page.locator('#connect-dst').fill(personKey);
    const option = page.locator(`#object-search-options option[value="${personId}"]`);
    await expect(option).toHaveCount(1);
    await expect(option).toHaveText(personKey);

    // A datalist option cannot be "clicked" by Playwright; submitting the id the
    // search surfaced is exactly the value the browser would set on selection.
    await page.locator('#connect-dst').fill(personId);
    await dialog.getByRole('button', { name: 'Connect' }).click();
    await page.waitForURL(new RegExp(`/objects/${escapeRe(taskId)}$`));

    // Resulting state on the SOURCE: the Assigned To edge with both endpoints.
    const sourceRel = relationshipsSection(page);
    await expect(sourceRel).toBeVisible();
    await expect(sourceRel.getByText('Assigned To')).toBeVisible();
    await expect(sourceRel.getByText(personKey, { exact: true })).toBeVisible();
    await expect(sourceRel.getByText(taskKey, { exact: true })).toBeVisible();
    await expect(sourceRel.getByText('No relationships')).toHaveCount(0);

    // Resulting state on the TARGET: the same edge is listed as incoming.
    await page.goto(`/objects/${personId}`);
    const targetRel = relationshipsSection(page);
    await expect(targetRel.getByText('Assigned To')).toBeVisible();
    await expect(targetRel.getByText(taskKey, { exact: true })).toBeVisible();
    await expect(targetRel.getByText(personKey, { exact: true })).toBeVisible();
  } finally {
    await cleanupObjects(page, [taskId, personId]);
  }
});
