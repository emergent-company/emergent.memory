import { test, expect, type Page } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';
import { memoryAuthHeaders } from '../../helpers/objects';
import { readBootstrap } from '../../helpers/bootstrap';
import { MEMORY_API_URL } from '../../helpers/tokens';
import { STORAGE_STATE } from '../../constants/storage';
import {
  createBoardEnabledSchema,
  cleanupBoardSchema,
  type BoardSchemaHandle,
} from '../../helpers/schemas';

// Full lifecycle of one board work item, seeded directly through the memory API
// (a board-enabled object type + one object) and then driven through the real
// /board UI: render → approve (review→done) → retry (blocked→ready) → reassign
// (assignee) → cancel (→blocked). No reaction agent is created, so nothing
// auto-runs and the item's status is only ever moved by this spec — the human
// actions resolve their transitions against the built-in status defaults
// (workConfigOf(nil) in domain/agents).
//
// Lives in the serial `mutations` project (the `-ui.spec.ts` testMatch). The
// whole file shares ONE seeded schema + object created in `beforeAll` and
// removed in `afterAll`; cleanup is best-effort so a failing assertion never
// strands rows in the bootstrap tenant.

let handle: BoardSchemaHandle | null = null;
let canonicalId = ''; // graph object canonical id (== id on first create)
let key = '';
let assignee = '';

/** The board card, scoped to one status column. */
function cardIn(page: Page, status: string) {
  return page.locator(`[data-board-column="${status}"]`).getByTestId(`board-card-${canonicalId}`);
}

/** Click the seeded card and wait for its drawer to open. */
async function openDrawer(page: Page): Promise<void> {
  await page.getByTestId(`board-card-${canonicalId}`).click();
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

/** The item's HEAD from the work-items API — an authoritative cross-check. */
async function workItem(page: Page): Promise<{ status?: string; assignee?: string }> {
  const projectId = readBootstrap()?.projectId;
  const headers = await memoryAuthHeaders(page);
  const resp = await page.request.get(
    `${MEMORY_API_URL}/api/projects/${projectId}/work-items/${encodeURIComponent(canonicalId)}`,
    { headers, failOnStatusCode: false },
  );
  if (!resp.ok()) return {};
  const body = (await resp.json().catch(() => ({}))) as {
    data?: { item?: { status?: string; assignee?: string } };
  };
  return body.data?.item ?? {};
}

test.describe.serial('board work-item lifecycle', () => {
  test.beforeAll(async ({ browser }) => {
    const context = await browser.newContext({ storageState: STORAGE_STATE });
    const page = await context.newPage();
    try {
      const stamp = Date.now();
      const type = `E2EBoardTask${stamp}`;
      key = `e2e-board-${stamp}`;
      assignee = `e2e-assignee-${stamp}`;
      handle = await createBoardEnabledSchema(page, type);

      const headers = await memoryAuthHeaders(page);
      const objResp = await page.request.post(`${MEMORY_API_URL}/api/graph/objects`, {
        headers,
        data: { type, key, status: 'ready', properties: { title: 'E2E board item' } },
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

  test('renders the seeded item in the ready lane and opens the drawer', async ({ page }) => {
    expect(canonicalId, 'beforeAll must seed the item').toBeTruthy();
    await page.goto('/board');
    await expectAppPage(page, /Board/);

    await expect(cardIn(page, 'ready')).toBeVisible();

    await openDrawer(page);
    const dialog = page.getByTestId('board-item-dialog');
    await expect(dialog.getByRole('heading', { name: key })).toBeVisible();

    // Close (data-dialog-close) so later steps start from a clean board.
    await dialog.getByRole('button', { name: 'Close' }).click();
    await expect(page.getByTestId('board-item-dialog')).toBeHidden();
  });

  test('approve moves a review item to done', async ({ page }) => {
    await setStatus(page, 'review');
    await page.goto('/board');
    await openDrawer(page);

    await page.getByTestId('board-item-dialog').getByRole('button', { name: 'Approve' }).click();

    await expect(cardIn(page, 'done')).toBeVisible();
    expect((await workItem(page)).status).toBe('done');
  });

  test('retry moves a blocked item back to ready', async ({ page }) => {
    await setStatus(page, 'blocked');
    await page.goto('/board');
    await openDrawer(page);

    await page.getByTestId('board-item-dialog').getByRole('button', { name: 'Retry' }).click();

    await expect(cardIn(page, 'ready')).toBeVisible();
    expect((await workItem(page)).status).toBe('ready');
  });

  test('reassign sets the assignee on the item', async ({ page }) => {
    await page.goto('/board');
    await openDrawer(page);

    const dialog = page.getByTestId('board-item-dialog');
    await dialog.locator('input[name="assignee"]').fill(assignee);
    await dialog.getByRole('button', { name: 'Reassign' }).click();

    // The card re-renders with the assignee badge; cross-check via the API.
    await expect(cardIn(page, 'ready')).toBeVisible();
    await expect(cardIn(page, 'ready').getByText(assignee)).toBeVisible();
    expect((await workItem(page)).assignee).toBe(assignee);
  });

  test('cancel moves the item to blocked', async ({ page }) => {
    await page.goto('/board');
    await openDrawer(page);

    await page.getByTestId('board-item-dialog').getByRole('button', { name: 'Cancel' }).click();

    await expect(cardIn(page, 'blocked')).toBeVisible();
    expect((await workItem(page)).status).toBe('blocked');
  });
});
