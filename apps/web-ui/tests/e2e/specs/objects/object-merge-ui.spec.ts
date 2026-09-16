import { test, expect, type Page } from '@playwright/test';
import { openObjectForm, submitObjectForm } from '../../helpers/objects';
import { readBootstrap } from '../../helpers/bootstrap';
import { MEMORY_API_URL } from '../../helpers/tokens';

// Object merge — GET /objects/:id/merge (uiObjectMerge).
//
// IMPORTANT scope note (recorded in the lane report): the gateway has NO merge
// form and NO object-merge endpoint. `uiObjectMerge` does not fuse objects; it
// builds a merge instruction naming the source (`:id`) and target (`?with=`) and
// 303-redirects into /chat with a resolved agent, where the agent performs the
// fusion with its memory tools. A real merge therefore requires a live LLM turn
// and is non-deterministic, so it cannot be asserted here — the destructive
// "source gone, relationships carried" scenario is out of reach for a
// deterministic UI spec (it would need the env-gated live-LLM `scenarios`
// suite). What this spec DOES cover behaviorally is the route's real, complete
// contract: it starts a merge session naming both objects and leaves the graph
// untouched until the agent acts.
//
// Cleanup: scratch agent, edges and objects are removed in `finally`, pass or
// fail. Graph objects/relationships are deleted through the memory API with the
// signed-in user's own Zitadel access token — the gateway exposes no
// object-delete route.

async function createTypedObject(page: Page, type: string, key: string): Promise<string> {
  await openObjectForm(page, type);
  await page.locator('#object-key').fill(key);
  return submitObjectForm(page);
}

/** Auth headers for the memory API (see the relationships spec for the why). */
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

/** True when the memory graph still lists the object (soft-deleted ones drop out). */
async function objectStillListed(page: Page, objectId: string): Promise<boolean> {
  const headers = await memoryAuthHeaders(page);
  const resp = await page.request.get(
    `${MEMORY_API_URL}/api/graph/objects/search?limit=100&ids=${encodeURIComponent(objectId)}`,
    { headers, failOnStatusCode: false },
  );
  if (!resp.ok()) return false;
  const body = (await resp.json().catch(() => ({}))) as { items?: Array<{ id?: string }> };
  return (body.items ?? []).some((o) => o.id === objectId);
}

async function cleanupMergeRun(
  page: Page,
  agentId: string,
  objectIds: string[],
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

test('object merge: starts a merge session naming both objects and leaves the graph untouched', async ({
  page,
}) => {
  test.setTimeout(120_000);
  const stamp = Date.now();
  const sourceKey = `E2E Merge Source ${stamp}`;
  const targetKey = `E2E Merge Target ${stamp}`;
  let sourceId = '';
  let targetId = '';
  let agentId = '';

  try {
    sourceId = await createTypedObject(page, 'Task', sourceKey);
    targetId = await createTypedObject(page, 'Person', targetKey);

    // A relationship between the two, mirroring the destructive-flow setup.
    const edge = await page.request.post(`/objects/${sourceId}/relationships`, {
      form: { type: 'assigned_to', src_id: sourceId, dst_id: targetId },
      maxRedirects: 0,
    });
    expect(edge.status(), 'POST /objects/:id/relationships should PRG (303)').toBe(303);

    // Snapshot the edge from BOTH endpoints before the merge: the post-merge
    // check must prove the graph is unchanged, and "both objects still exist"
    // alone would pass even if the relationship were dropped.
    const preHeaders = await memoryAuthHeaders(page);
    const sourceEdgesBefore = await edgeIdsOf(page, preHeaders, sourceId);
    const targetEdgesBefore = await edgeIdsOf(page, preHeaders, targetId);
    const sharedEdgeIds = sourceEdgesBefore.filter((id) => targetEdgesBefore.includes(id));
    expect(sharedEdgeIds.length, 'the new edge must be visible from both endpoints').toBeGreaterThan(0);

    // An agent must exist or uiObjectMerge bails back to the source object (a
    // scratch agent guarantees the merge-session branch is exercised).
    const agentResp = await page.request.post('/api/agents', {
      data: { name: `E2E Merge Agent ${stamp}`, tools: [], skills: [], config: {} },
    });
    expect(agentResp.ok(), `create scratch agent failed (HTTP ${agentResp.status()})`).toBeTruthy();
    agentId = ((await agentResp.json()) as { id: string }).id;

    // The route redirects into a chat merge session; inspect the redirect
    // without following it into the chat surface.
    const resp = await page.request.get(`/objects/${sourceId}/merge?with=${targetId}`, {
      maxRedirects: 0,
    });
    expect(resp.status(), 'GET /objects/:id/merge should 303').toBe(303);

    const location = resp.headers()['location'];
    expect(location, 'merge redirect must carry a Location header').toBeTruthy();
    const redirect = new URL(location!, 'http://localhost');
    expect(redirect.pathname).toBe('/chat');
    expect(redirect.searchParams.get('agent'), 'merge session must name an agent').toBeTruthy();

    // The instruction names both objects by label AND id, and frames source vs
    // target — that is the entire deterministic behavior of uiObjectMerge.
    const prompt = redirect.searchParams.get('prompt') ?? '';
    expect(prompt).toContain('Merge the following two knowledge-graph objects into one.');
    expect(prompt).toContain('Object A (source):');
    expect(prompt).toContain(sourceKey);
    expect(prompt).toContain(sourceId);
    expect(prompt).toContain('Object B (target):');
    expect(prompt).toContain(targetKey);
    expect(prompt).toContain(targetId);

    // The route starts a session; it must not silently remove either object.
    expect(await objectStillListed(page, sourceId), 'source must still exist').toBe(true);
    expect(await objectStillListed(page, targetId), 'target must still exist').toBe(true);

    // ...nor the relationship between them: starting a merge session only
    // composes a prompt, so the exact same edge must still be attached to both
    // endpoints (same edge id, not merely "some edge").
    const postHeaders = await memoryAuthHeaders(page);
    expect(
      await edgeIdsOf(page, postHeaders, sourceId),
      'source must still expose the original edge after the merge session starts',
    ).toEqual(expect.arrayContaining(sharedEdgeIds));
    expect(
      await edgeIdsOf(page, postHeaders, targetId),
      'target must still expose the original edge after the merge session starts',
    ).toEqual(expect.arrayContaining(sharedEdgeIds));
  } finally {
    await cleanupMergeRun(page, agentId, [sourceId, targetId]);
  }
});

test('object merge: missing or unknown merge target redirects back to the object', async ({
  page,
}) => {
  test.setTimeout(120_000);
  const stamp = Date.now();
  const key = `E2E Merge Guard ${stamp}`;
  let objectId = '';

  try {
    objectId = await createTypedObject(page, 'Person', key);

    // No `with` at all.
    const noTarget = await page.request.get(`/objects/${objectId}/merge`, { maxRedirects: 0 });
    expect(noTarget.status()).toBe(303);
    expect(new URL(noTarget.headers()['location']!, 'http://localhost').pathname).toBe(
      `/objects/${objectId}`,
    );

    // Unknown target id.
    const unknown = await page.request.get(
      `/objects/${objectId}/merge?with=00000000-0000-0000-0000-000000000000`,
      { maxRedirects: 0 },
    );
    expect(unknown.status()).toBe(303);
    expect(new URL(unknown.headers()['location']!, 'http://localhost').pathname).toBe(
      `/objects/${objectId}`,
    );

    // The object is untouched and still renders.
    await page.goto(`/objects/${objectId}`);
    await expect(page.getByRole('heading', { name: key })).toBeVisible();
    await expect(page.locator('#object-key')).toHaveValue(key);
  } finally {
    await cleanupMergeRun(page, '', [objectId]);
  }
});
