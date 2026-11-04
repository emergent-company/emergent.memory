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

// Ticket-style assignee picker + exact-status control on the board. Seeds a
// board-enabled type + one object through the memory API, then drives the real
// /board UI:
//
//   1. The assignee picker (shared object preview top workflow strip) lists the
//      project's members and reassigns the item to a chosen member, keeping the
//      preview open while the board refreshes from the event stream.
//   2. The status picker (same top strip) offers the workflow statuses minus the
//      current one and moves the item to an exact phase.
//
// No reaction agent is created, so nothing auto-runs and the item's status is
// only ever moved by these human controls. Lives in the serial `mutations`
// project (the `-ui.spec.ts` testMatch); the file shares ONE seeded schema +
// object created in beforeAll and removed in afterAll (best-effort).

let handle: BoardSchemaHandle | null = null;
let canonicalId = '';
let key = '';

/** A project member as returned by the gateway GET /api/members. */
interface MemberDto {
  displayName?: string;
  email?: string;
}

/** The label the picker renders for a member (gateway/board.go buildBoardAssigneeCandidates). */
function memberLabel(m: MemberDto): string {
  const name = (m.displayName ?? '').trim();
  return name !== '' ? name : (m.email ?? '').trim();
}

async function fetchMembers(page: Page): Promise<MemberDto[]> {
  const resp = await page.request.get('/api/members');
  if (!resp.ok()) {
    throw new Error(`GET /api/members failed (HTTP ${resp.status()}): ${await resp.text()}`);
  }
  return (await resp.json()) as MemberDto[];
}

/** The board card, scoped to one status column. */
function cardIn(page: Page, status: string) {
  return page.locator(`[data-board-column="${status}"]`).getByTestId(`board-card-${canonicalId}`);
}

/** Open the shared object preview for the seeded card (card click → preview). */
async function openPreview(page: Page): Promise<void> {
  await page.getByTestId(`board-card-${canonicalId}`).click();
  const panel = page.locator('#object-preview-panel');
  await expect(panel).toHaveAttribute('aria-hidden', 'false');
  await expect(page.locator('#object-preview-heading')).toHaveText(key);
}

/**
 * Open a CSS-only daisyUI dropdown by focusing its trigger (click, then an
 * explicit focus as a headless fallback), and return the open menu. Mirrors the
 * account-menu helper: .dropdown-content is revealed via :focus-within.
 */
async function openDropdown(page: Page, label: string) {
  const trigger = page.getByRole('button', { name: label });
  const menu = page.getByRole('menu', { name: label });
  await trigger.click();
  if (!(await menu.isVisible())) {
    await trigger.focus();
  }
  await expect(menu).toBeVisible();
  return { trigger, menu };
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

test.describe.serial('board assignee + status controls', () => {
  test.beforeAll(async ({ browser }) => {
    const context = await browser.newContext({ storageState: STORAGE_STATE });
    const page = await context.newPage();
    try {
      const stamp = Date.now();
      const type = `E2EBoardStatus${stamp}`;
      key = `e2e-board-status-${stamp}`;
      handle = await createBoardEnabledSchema(page, type);

      const headers = await memoryAuthHeaders(page);
      const objResp = await page.request.post(`${MEMORY_API_URL}/api/graph/objects`, {
        headers,
        data: { type, key, status: 'ready', properties: { title: 'E2E board status item' } },
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

  test('assignee picker reassigns the item without closing the preview', async ({ page }) => {
    expect(canonicalId, 'beforeAll must seed the item').toBeTruthy();

    const members = await fetchMembers(page);
    const target = memberLabel(members[0] ?? {});
    expect(target, 'a project member must exist to reassign').toBeTruthy();

    await goto(page, '/board');
    await expectAppPage(page, /Board/);
    await openPreview(page);

    // The picker trigger is the avatar+name pill in the preview's top strip,
    // not the action slot.
    const { menu } = await openDropdown(page, 'Assignee');
    // Unassigned is always listed alongside the members/agents.
    await expect(menu.getByRole('menuitem', { name: 'Unassigned' })).toBeVisible();
    await menu.getByRole('menuitem', { name: target }).click();

    // The preview stays open and swaps its body in place.
    await expect(page.locator('#object-preview-panel')).toHaveAttribute('aria-hidden', 'false');
    await expect(page.locator('#object-preview-heading')).toHaveText(key);
    await expect(page.getByTestId('object-assignee-control')).toContainText(target);

    // The board refreshes from the project event stream behind the open preview.
    await expect(cardIn(page, 'ready').getByText(target)).toBeVisible({ timeout: 15_000 });
    expect((await workItem(page)).assignee).toBe(target);
  });

  test('status control moves the item to an exact phase without closing the preview', async ({ page }) => {
    expect(canonicalId, 'beforeAll must seed the item').toBeTruthy();

    await goto(page, '/board');
    await expectAppPage(page, /Board/);
    await openPreview(page);

    const { menu } = await openDropdown(page, 'Change status');

    // The current "ready" phase is excluded; the other phases are offered.
    await expect(menu.getByRole('menuitem', { name: 'Done' })).toBeVisible();
    await expect(menu.getByRole('menuitem', { name: 'Ready' })).toHaveCount(0);

    await menu.getByRole('menuitem', { name: 'Done' }).click();

    // The preview stays open and reflects the new status.
    await expect(page.locator('#object-preview-panel')).toHaveAttribute('aria-hidden', 'false');
    await expect(page.getByTestId('object-status-control')).toContainText('Done');

    // The board refreshes from the event stream with the item in the done lane.
    await expect(cardIn(page, 'done')).toBeVisible({ timeout: 15_000 });
    expect((await workItem(page)).status).toBe('done');
  });
});
