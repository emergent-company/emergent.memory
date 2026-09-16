import { Page, expect } from '@playwright/test';
import { readBootstrap } from './bootstrap';
import { MEMORY_API_URL } from './tokens';

// uiObjectCreate redirects to the new object's detail page (/objects/:id).
const OBJECT_DETAIL_URL = /\/objects\/[0-9a-f-]{36}/;

/**
 * Open /objects/new, pick the object type, and wait for the details form
 * (key + fields) to render.
 */
export async function openObjectForm(page: Page, type: string): Promise<void> {
  await page.goto('/objects/new');
  await page.locator('select[name="type"]').selectOption(type);
  await expect(page.locator('#object-key')).toBeVisible();
}

/** Type a label and press Enter — the TagList renders a chip + hidden input. */
export async function addObjectLabel(page: Page, label: string): Promise<void> {
  await page.locator('#object-labels').fill(label);
  await page.locator('#object-labels').press('Enter');
}

/**
 * Click "Create object" and wait for the redirect to the new object's detail
 * page; return the created object's id parsed from the URL.
 */
export async function submitObjectForm(page: Page): Promise<string> {
  await page.getByRole('button', { name: 'Create object' }).click();
  await page.waitForURL(OBJECT_DETAIL_URL);
  const id = new URL(page.url()).pathname.split('/').pop();
  if (!id) {
    throw new Error(`submitObjectForm: no id in the detail URL "${page.url()}"`);
  }
  return id;
}

/**
 * Create an object through the real form: pick the type, fill the key, add
 * each label as a chip, submit, and return the created object's id.
 */
export async function createObjectViaForm(
  page: Page,
  type: string,
  key: string,
  labels: string[],
): Promise<string> {
  await openObjectForm(page, type);
  await page.locator('#object-key').fill(key);
  for (const label of labels) {
    await addObjectLabel(page, label);
  }
  return submitObjectForm(page);
}

/**
 * Create a typed object through the real create form and return its id (the PRG
 * redirect target parsed by `submitObjectForm`).
 */
export async function createTypedObject(page: Page, type: string, key: string): Promise<string> {
  await openObjectForm(page, type);
  await page.locator('#object-key').fill(key);
  return submitObjectForm(page);
}

/**
 * Auth headers for the memory API: the signed-in user's Zitadel access token
 * (carried inside the gateway's `memory_session` cookie) plus the active project
 * id. The gateway does not proxy graph-object deletes, so object specs talk to
 * the memory API directly — the same origin the token-lifecycle specs probe.
 */
export async function memoryAuthHeaders(page: Page): Promise<Record<string, string>> {
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
export async function edgeIdsOf(
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

/**
 * Best-effort cleanup: remove edges touching the objects, then the objects, and
 * optionally a scratch agent id. Graph deletes are soft — rows drop out of live
 * listings but remain restorable in the archive (see the e2e README). Never
 * throws, so it is safe to call from `finally` without masking the spec's real
 * failure.
 */
export async function cleanupObjects(
  page: Page,
  objectIds: string[],
  agentId?: string,
): Promise<void> {
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
    if (agentId) await page.request.delete(`/api/agents/${agentId}`).catch(() => undefined);
  } catch {
    // Best-effort only — never mask the spec's real failure.
  }
}
