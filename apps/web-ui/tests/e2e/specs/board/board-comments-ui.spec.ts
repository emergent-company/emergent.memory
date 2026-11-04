import { test, expect, type Page } from '@playwright/test';
import { expectAppPage, goto } from '../../helpers/page';
import { memoryAuthHeaders } from '../../helpers/objects';
import { readBootstrap } from '../../helpers/bootstrap';
import { MEMORY_API_URL } from '../../helpers/tokens';
import { STORAGE_STATE } from '../../constants/storage';
import {
  createBoardEnabledSchema,
  cleanupBoardSchema,
  type BoardSchemaHandle,
} from '../../helpers/schemas';

// Comment-thread lifecycle of one board work item, seeded directly through the
// memory API (a board-enabled object type + one object) and then driven through
// the real /board UI: empty state → post a comment from the inline form → a
// rework feedback round and a second comment render in the same merged thread in
// ascending `createdAt` order. No reaction agent is created, so nothing
// auto-runs and the item's status is only ever moved by this spec (see the
// sibling board-lifecycle-ui.spec.ts for the status-move lifecycle; the human
// actions resolve their transitions against the built-in status defaults).
//
// The merged "Activity" thread (durable comments + rework feedback rounds) and
// its inline form render in BOTH board surfaces:
//   * the shared object preview drawer's board slot (#object-preview-actions-slot)
//     — the card-click path, and the primary way a human opens a board item; and
//   * the native drawer dialog (#board-drawer) — the rarer drag→review path.
// Both surfaces can share one DOM (the dialog's content persists in #board-drawer
// after it closes), so each surface renders its own `data-testid` family
// ("board-preview-comment-*" / "board-drawer-comment-*") to keep a Playwright
// locator unambiguous. The first three cases drive the card-click preview; the
// last opens the native drawer too and pins the two id families apart.
//
// Lives in the serial `mutations` project (the `-ui.spec.ts` testMatch). The
// whole file shares ONE seeded schema + object created in `beforeAll` and
// removed in `afterAll`; cleanup is best-effort so a failing assertion never
// strands rows in the bootstrap tenant.

let handle: BoardSchemaHandle | null = null;
let canonicalId = ''; // graph object canonical id (== id on first create)
let key = '';

/** One entry of the merged thread, read back from the work-item detail API. */
interface ThreadEntry {
  kind: 'comment' | 'feedback';
  author: string;
  body: string;
  createdAt: string;
}

/** Click the seeded card and wait for the shared object preview drawer + its board slot. */
async function openPreview(page: Page): Promise<void> {
  await expect(page.getByTestId(`board-card-${canonicalId}`)).toBeVisible();
  await page.getByTestId(`board-card-${canonicalId}`).click();
  const panel = page.locator('#object-preview-panel');
  await expect(panel).toHaveAttribute('aria-hidden', 'false');
  // The summary and the ?actions=board slot (actions + thread) load over htmx.
  await expect(page.locator('#object-preview-heading')).toHaveText(key);
}

/** The preview's board slot — status-gated actions followed by the merged thread. */
function previewBoard(page: Page) {
  return page.locator('#object-preview-actions-slot');
}

/** The merged thread's rows in the preview surface. */
function commentRows(page: Page) {
  return previewBoard(page).getByTestId('board-preview-comment-row');
}

/**
 * Open the native board-item dialog (the drag review→revision target) by loading
 * its drawer partial into #board-drawer — the same request app.js fires on a
 * review→revision drop, without simulating a native drag.
 */
async function openItemDialog(page: Page): Promise<void> {
  await page.evaluate(
    (id) => {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const htmx = (window as any).htmx;
      if (!htmx || typeof htmx.ajax !== 'function') {
        throw new Error('htmx is unavailable; cannot open the board item dialog');
      }
      htmx.ajax('GET', `/board/items/${encodeURIComponent(id)}`, {
        target: '#board-drawer',
        swap: 'innerHTML',
      });
    },
    canonicalId,
  );
  await expect(page.getByTestId('board-item-dialog')).toBeVisible();
}

/** PATCH the object's status directly (the graph API is the deterministic setter). */
async function setStatus(page: Page, status: string): Promise<void> {
  const headers = await memoryAuthHeaders(page);
  const resp = await page.request.patch(
    `${MEMORY_API_URL}/api/graph/objects/${encodeURIComponent(canonicalId)}`,
    { headers, data: { status }, failOnStatusCode: false },
  );
  if (!resp.ok()) {
    throw new Error(
      `setStatus: PATCH /api/graph/objects/${canonicalId} failed (HTTP ${resp.status()}): ${await resp.text()}`,
    );
  }
}

/**
 * POST request-changes to seed one rework feedback round. The item must be in
 * `review` (setStatus first). With no agent seeded this records the feedback
 * round and escalates to a human task — the feedback round is what the thread
 * assertion depends on; the escalation task is a benign, project-scoped artifact
 * no other spec reads.
 */
async function requestChanges(page: Page, feedback: string): Promise<void> {
  const projectId = readBootstrap()?.projectId;
  const headers = await memoryAuthHeaders(page);
  const resp = await page.request.post(
    `${MEMORY_API_URL}/api/projects/${projectId}/work-items/${encodeURIComponent(canonicalId)}/request-changes`,
    { headers, data: { feedback }, failOnStatusCode: false },
  );
  if (!resp.ok()) {
    throw new Error(
      `requestChanges: POST work-items/${canonicalId}/request-changes failed ` +
        `(HTTP ${resp.status()}): ${await resp.text()}`,
    );
  }
}

/** POST a durable comment through the memory API (the deterministic seeder). */
async function addCommentViaApi(page: Page, body: string): Promise<void> {
  const projectId = readBootstrap()?.projectId;
  const headers = await memoryAuthHeaders(page);
  const resp = await page.request.post(
    `${MEMORY_API_URL}/api/projects/${projectId}/work-items/${encodeURIComponent(canonicalId)}/comments`,
    { headers, data: { body }, failOnStatusCode: false },
  );
  if (!resp.ok()) {
    throw new Error(
      `addCommentViaApi: POST work-items/${canonicalId}/comments failed ` +
        `(HTTP ${resp.status()}): ${await resp.text()}`,
    );
  }
}

/**
 * The work-item detail's comments + feedback, each normalised into one thread
 * entry. These are the exact `createdAt` values the gateway merges and renders,
 * so the expected order below is derived from server data, never from how long
 * the test happened to take.
 */
async function threadEntries(page: Page): Promise<ThreadEntry[]> {
  const projectId = readBootstrap()?.projectId;
  const headers = await memoryAuthHeaders(page);
  const resp = await page.request.get(
    `${MEMORY_API_URL}/api/projects/${projectId}/work-items/${encodeURIComponent(canonicalId)}`,
    { headers, failOnStatusCode: false },
  );
  if (!resp.ok()) return [];
  const body = (await resp.json().catch(() => ({}))) as {
    data?: {
      comments?: Array<{ author?: string; body?: string; createdAt?: string }>;
      feedback?: Array<{ author?: string; text?: string; createdAt?: string }>;
    };
  };
  const out: ThreadEntry[] = [];
  for (const c of body.data?.comments ?? []) {
    out.push({ kind: 'comment', author: c.author ?? '', body: c.body ?? '', createdAt: c.createdAt ?? '' });
  }
  for (const f of body.data?.feedback ?? []) {
    out.push({ kind: 'feedback', author: f.author ?? '', body: f.text ?? '', createdAt: f.createdAt ?? '' });
  }
  return out;
}

/**
 * The order the gateway renders the merged thread in, derived from the API's own
 * `createdAt` values: ascending, then the deterministic same-second tie-break
 * (kind, author, body) exactly as `boardComments` applies it in
 * apps/web-ui/gateway/board.go. Deriving the expectation this way keeps the
 * ordering assertion independent of wall-clock seconds, so it is stable whether
 * or not two seeds land in the same second.
 */
function expectedOrder(entries: ThreadEntry[]): ThreadEntry[] {
  return [...entries].sort((a, b) => {
    if (a.createdAt !== b.createdAt) return a.createdAt < b.createdAt ? -1 : 1;
    if (a.kind !== b.kind) return a.kind < b.kind ? -1 : 1;
    if (a.author !== b.author) return a.author < b.author ? -1 : 1;
    return a.body < b.body ? -1 : a.body > b.body ? 1 : 0;
  });
}

test.describe.serial('board comments thread', () => {
  test.beforeAll(async ({ browser }) => {
    const context = await browser.newContext({ storageState: STORAGE_STATE });
    const page = await context.newPage();
    try {
      const stamp = Date.now();
      const type = `E2EBoardCommentTask${stamp}`;
      key = `e2e-board-comment-${stamp}`;
      handle = await createBoardEnabledSchema(page, type);

      const headers = await memoryAuthHeaders(page);
      const objResp = await page.request.post(`${MEMORY_API_URL}/api/graph/objects`, {
        headers,
        data: { type, key, status: 'ready', properties: { title: 'E2E board comment item' } },
        failOnStatusCode: false,
      });
      if (!objResp.ok()) {
        throw new Error(
          `beforeAll: create object failed (HTTP ${objResp.status()}): ${await objResp.text()}`,
        );
      }
      const obj = (await objResp.json()) as { canonical_id?: string };
      if (!obj.canonical_id) {
        throw new Error(`beforeAll: object response missing canonical_id: ${JSON.stringify(obj)}`);
      }
      canonicalId = obj.canonical_id;
    } finally {
      await context.close();
    }
  });

  test.afterAll(async ({ browser }) => {
    const context = await browser.newContext({ storageState: STORAGE_STATE });
    const page = await context.newPage();
    try {
      const headers = await memoryAuthHeaders(page);
      if (canonicalId) {
        await page.request
          .delete(`${MEMORY_API_URL}/api/graph/objects/${encodeURIComponent(canonicalId)}`, {
            headers,
            failOnStatusCode: false,
          })
          .catch(() => undefined);
      }
      await cleanupBoardSchema(page, handle);
    } catch {
      // Best-effort only — never mask the spec's real failure.
    } finally {
      await context.close();
    }
  });

  test('an item with no comments or feedback shows the muted empty state', async ({ page }) => {
    expect(canonicalId, 'beforeAll must seed the item').toBeTruthy();
    await goto(page, '/board');
    await expectAppPage(page, /Board/);

    await openPreview(page);
    await expect(previewBoard(page).getByRole('heading', { name: 'Activity' })).toBeVisible();
    await expect(previewBoard(page).getByTestId('board-preview-comment-empty')).toBeVisible();
    await expect(previewBoard(page).getByTestId('board-preview-comment-empty')).toHaveText(
      'No activity yet',
    );
    await expect(commentRows(page)).toHaveCount(0);
    expect((await threadEntries(page)).length).toBe(0);

    // Close so later steps start from a clean board.
    await page.getByTestId('object-preview-close').click();
    await expect(page.locator('#object-preview-panel')).toHaveAttribute('aria-hidden', 'true');
  });

  test('posting a comment from the inline form adds it to the merged thread', async ({ page }) => {
    await goto(page, '/board');
    await expectAppPage(page, /Board/);

    // The inline form (this spec drives the preview surface) posts to #board;
    // object-preview.js closes the drawer on a successful POST and the board
    // re-renders.
    await openPreview(page);
    await previewBoard(page).getByTestId('board-preview-comment-input').fill('First human note');
    await previewBoard(page).getByRole('button', { name: 'Comment' }).click();
    await expect(page.locator('#object-preview-panel')).toHaveAttribute('aria-hidden', 'true');

    // Re-open the drawer to see the comment in the merged thread.
    await openPreview(page);
    await expect(commentRows(page)).toHaveCount(1);
    await expect(commentRows(page).first()).toContainText('First human note');
    await expect(previewBoard(page).getByTestId('board-preview-comment-empty')).toHaveCount(0);
    expect((await threadEntries(page)).length).toBe(1);

    await page.getByTestId('object-preview-close').click();
    await expect(page.locator('#object-preview-panel')).toHaveAttribute('aria-hidden', 'true');
  });

  test('a feedback round and a comment render in one thread in ascending order', async ({
    page,
  }) => {
    // Seed a rework feedback round (needs review status), then a later comment.
    await setStatus(page, 'review');
    await requestChanges(page, 'Rework the title please');
    await addCommentViaApi(page, 'Follow-up note');

    await goto(page, '/board');
    await expectAppPage(page, /Board/);
    await openPreview(page);

    // The expected order comes from the server's own createdAt values (see
    // expectedOrder), so it does not depend on the two seeds landing in different
    // wall-clock seconds.
    const order = expectedOrder(await threadEntries(page));
    const rows = commentRows(page);
    await expect(rows).toHaveCount(order.length);
    for (let i = 0; i < order.length; i++) {
      await expect(rows.nth(i)).toContainText(order[i].body);
    }

    // The feedback round is badged with its rework round; a comment is not.
    const feedbackRow = rows.filter({ hasText: 'Rework the title please' });
    await expect(feedbackRow).toContainText('rework round 1');

    const counts = await threadEntries(page);
    expect(counts.filter((e) => e.kind === 'comment')).toHaveLength(2);
    expect(counts.filter((e) => e.kind === 'feedback')).toHaveLength(1);
  });

  test('both surfaces render the thread, each under its own test ids', async ({ page }) => {
    await goto(page, '/board');
    await expectAppPage(page, /Board/);

    // Card-click path: the preview's board slot carries the thread.
    await openPreview(page);
    await expect(previewBoard(page).getByRole('heading', { name: 'Activity' })).toBeVisible();
    const expected = expectedOrder(await threadEntries(page));
    expect(expected.length, 'the earlier cases must have seeded the thread').toBeGreaterThan(0);
    await expect(page.getByTestId('board-preview-comment-row')).toHaveCount(expected.length);

    // Now open the native drawer too (the drag→review path). Its content persists
    // in #board-drawer, so both surfaces share one DOM at once — each family of
    // test ids must still resolve to only its own surface.
    await openItemDialog(page);
    await expect(page.getByTestId('board-drawer-comment-row')).toHaveCount(expected.length);
    await expect(page.getByTestId('board-preview-comment-row')).toHaveCount(expected.length);
    await expect(page.locator('#board-drawer').getByTestId('board-preview-comment-row')).toHaveCount(0);
    await expect(
      page.locator('#object-preview-actions-slot').getByTestId('board-drawer-comment-row'),
    ).toHaveCount(0);

    // The drawer renders the thread under its own eyebrow heading ("Activity").
    await expect(page.locator('#board-drawer').getByText('Activity', { exact: true })).toBeVisible();
  });
});
