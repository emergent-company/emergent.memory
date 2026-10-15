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
//   1. The assignee picker (shared object preview action slot) lists the
//      project's members and reassigns the item to a chosen member.
//   2. The "Set status" control (board item dialog metadata row) offers the
//      work-path vocabulary minus the current phase and moves the item to an
//      exact phase.
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

/** Open the board item dialog by loading its drawer partial (drag review→revision equivalent). */
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

  test('assignee picker reassigns the item to a project member', async ({ page }) => {
    expect(canonicalId, 'beforeAll must seed the item').toBeTruthy();

    const members = await fetchMembers(page);
    const target = memberLabel(members[0] ?? {});
    expect(target, 'a project member must exist to reassign').toBeTruthy();

    await goto(page, '/board');
    await expectAppPage(page, /Board/);
    await openPreview(page);

    // The picker trigger is the avatar+name pill inside the action slot.
    const { menu } = await openDropdown(page, 'Assignee');
    // Unassigned is always listed alongside the members/agents.
    await expect(menu.getByRole('menuitem', { name: 'Unassigned' })).toBeVisible();
    await menu.getByRole('menuitem', { name: target }).click();

    // A successful slot action closes the preview to reveal the refreshed board.
    await expect(page.locator('#object-preview-panel')).toHaveAttribute('aria-hidden', 'true');
    await expect(cardIn(page, 'ready')).toBeVisible();
    await expect(cardIn(page, 'ready').getByText(target)).toBeVisible();
    expect((await workItem(page)).assignee).toBe(target);
  });

  test('status control moves the item to an exact phase', async ({ page }) => {
    expect(canonicalId, 'beforeAll must seed the item').toBeTruthy();

    await goto(page, '/board');
    await expectAppPage(page, /Board/);
    await openItemDialog(page);

    const { menu } = await openDropdown(page, 'Set status');

    // The current "ready" phase is excluded; the other five phases are offered.
    await expect(menu.getByRole('menuitem', { name: 'Done' })).toBeVisible();
    await expect(menu.getByRole('menuitem', { name: 'Ready' })).toHaveCount(0);

    await menu.getByRole('menuitem', { name: 'Done' }).click();

    // The action clears the dialog and re-renders the board with the item done.
    await expect(page.getByTestId('board-item-dialog')).toHaveCount(0);
    await expect(cardIn(page, 'done')).toBeVisible();
    expect((await workItem(page)).status).toBe('done');
  });
});
