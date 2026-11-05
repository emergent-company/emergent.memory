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

// Seeded object-detail surface: the Overview / History / Relationships /
// Similar objects rail, the Overview Workflow panel, and the separate
// Relationships subpage. Seeds a board-enabled type plus a few objects and one
// relationship through the memory API and drives the real object pages.
//
// Lives in the serial `mutations` project (`-ui.spec.ts` testMatch). The file
// shares ONE seeded schema + object created in beforeAll and removed in
// afterAll; cleanup is best-effort so a failing assertion never strands rows in
// the bootstrap tenant.

let handle: BoardSchemaHandle | null = null;
let canonicalId = '';
let relatedId = '';
let emptyId = '';
let relationshipType = '';
let key = '';

interface MemberDto {
  displayName?: string;
  email?: string;
}

function memberLabel(m: MemberDto): string {
  const name = (m.displayName ?? '').trim();
  return name !== '' ? name : (m.email ?? '').trim();
}

async function openDropdown(page: Page, controlTestId: string, label: string) {
  const control = page.getByTestId(controlTestId);
  const trigger = control.getByRole('button', { name: label });
  const menu = control.getByRole('menu', { name: label });
  await trigger.click();
  if (!(await menu.isVisible())) {
    await trigger.focus();
  }
  await expect(menu).toBeVisible();
  return { control, menu };
}

test.describe.serial('object detail subpages + workflow controls', () => {
  test.beforeAll(async ({ browser }) => {
    const context = await browser.newContext({ storageState: STORAGE_STATE });
    const page = await context.newPage();
    try {
      const stamp = Date.now();
      const type = `E2EObjectDetail${stamp}`;
      key = `e2e-object-detail-${stamp}`;
      relationshipType = `related_to_${stamp}`;
      handle = await createBoardEnabledSchema(page, type, relationshipType);

      const headers = await memoryAuthHeaders(page);
      const createObject = async (objectKey: string): Promise<string> => {
        const resp = await page.request.post(`${MEMORY_API_URL}/api/graph/objects`, {
          headers,
          data: { type, key: objectKey, status: 'ready', properties: { title: objectKey } },
          failOnStatusCode: false,
        });
        if (!resp.ok()) {
          throw new Error(
            `beforeAll: create object failed (HTTP ${resp.status()}): ${await resp.text()}`,
          );
        }
        const body = (await resp.json()) as { canonical_id?: string };
        if (!body.canonical_id) {
          throw new Error(`beforeAll: object response missing canonical_id: ${JSON.stringify(body)}`);
        }
        return body.canonical_id;
      };
      canonicalId = await createObject(key);
      relatedId = await createObject(`${key}-related`);
      emptyId = await createObject(`${key}-empty`);

      const relResp = await page.request.post(`${MEMORY_API_URL}/api/graph/relationships`, {
        headers,
        data: { type: relationshipType, src_id: canonicalId, dst_id: relatedId },
        failOnStatusCode: false,
      });
      if (!relResp.ok()) {
        throw new Error(
          `beforeAll: create relationship failed (HTTP ${relResp.status()}): ${await relResp.text()}`,
        );
      }
    } finally {
      await context.close();
    }
  });

  test.afterAll(async ({ browser }) => {
    const context = await browser.newContext({ storageState: STORAGE_STATE });
    const page = await context.newPage();
    try {
      const headers = await memoryAuthHeaders(page);
      for (const id of [canonicalId, relatedId, emptyId]) {
        if (!id) continue;
        await page.request
          .delete(`${MEMORY_API_URL}/api/graph/objects/${encodeURIComponent(id)}`, {
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

  test('overview renders the rail, workflow panel, and relationship badge', async ({ page }) => {
    expect(canonicalId, 'beforeAll must seed the object').toBeTruthy();
    await goto(page, `/objects/${encodeURIComponent(canonicalId)}`);
    await expectAppPage(page, new RegExp(key));

    const panel = page.getByTestId('object-workflow-panel');
    await expect(panel).toBeVisible();
    await expect(panel.getByText('Status', { exact: true })).toBeVisible();
    await expect(panel.getByText('Assignee', { exact: true })).toBeVisible();
    await expect(page.getByTestId('object-status-control')).toBeVisible();
    await expect(page.getByTestId('object-assignee-control')).toBeVisible();

    const rail = page.getByRole('navigation', { name: 'Object sections' });
    await expect(rail.getByRole('link', { name: 'Overview' })).toHaveAttribute('aria-current', 'page');
    await expect(rail.getByRole('link', { name: 'History' })).toBeVisible();
    await expect(rail.getByRole('link', { name: /Relationships/ })).toBeVisible();
    await expect(rail.getByTestId('object-relationships-count')).toHaveText('1');
    await expect(rail.getByRole('link', { name: /Similar objects/ })).toBeVisible();

    // Similar objects moved to its own tab: the overview must not render the
    // deferred similar card, and Relationships moved to its own subpage.
    await expect(page.getByTestId('object-similar-deferred')).toHaveCount(0);
    await expect(page.getByTestId('object-connect-modal')).toHaveCount(0);
  });

  test('the Similar rail badge loads lazily from the count fragment', async ({ page }) => {
    await page.route(`**/objects/${canonicalId}/similar/count`, (route) =>
      route.fulfill({
        status: 200,
        contentType: 'text/html',
        body: '<span class="badge badge-neutral badge-soft badge-xs" data-testid="object-similar-count">4</span>',
      }),
    );
    await goto(page, `/objects/${encodeURIComponent(canonicalId)}`);
    await expectAppPage(page, new RegExp(key));
    await expect(
      page.getByRole('navigation', { name: 'Object sections' }).getByTestId('object-similar-count'),
    ).toHaveText('4');
    // The deferred count swap must stay scoped to the rail badge: without its
    // own hx-target it inherits #main-content and replaces the page body.
    await expect(page.getByTestId('object-workflow-panel')).toBeVisible();
    await page.unroute(`**/objects/${canonicalId}/similar/count`);
  });

  test('relationships is a full subpage with the moved list', async ({ page }) => {
    await goto(page, `/objects/${encodeURIComponent(canonicalId)}/relationships`);
    await expectAppPage(page, new RegExp(key));
    await expect(
      page.getByRole('navigation', { name: 'Object sections' }).getByRole('link', { name: /Relationships/ }),
    ).toHaveAttribute('aria-current', 'page');
    await expect(page.getByRole('heading', { name: 'Relationships' })).toBeVisible();
    await expect(page.getByText(`${key}-related`)).toBeVisible();
    await expect(page.getByTestId('object-connect-modal')).toBeAttached();
  });

  test('an object with no relationships shows the Connect CTA', async ({ page }) => {
    await goto(page, `/objects/${encodeURIComponent(emptyId)}/relationships`);
    await expectAppPage(page, new RegExp(`${key}-empty`));
    await expect(page.getByText('No relationships')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Connect this object to another' })).toBeVisible();
  });

  test('history is a full subpage with the shared header and timeline', async ({ page }) => {
    await goto(page, `/objects/${encodeURIComponent(canonicalId)}`);
    await expectAppPage(page, new RegExp(key));

    await page
      .getByRole('navigation', { name: 'Object sections' })
      .getByRole('link', { name: 'History' })
      .click();
    await expect(page).toHaveURL(new RegExp(`/objects/${canonicalId}/history`));
    await expect(page.getByTestId('object-history-page')).toBeVisible();
    // The Workflow panel belongs to Overview only; subpages keep the header badge.
    await expect(page.getByTestId('object-workflow-panel')).toHaveCount(0);
    await expect(
      page.getByRole('navigation', { name: 'Object sections' }).getByRole('link', { name: 'History' }),
    ).toHaveAttribute('aria-current', 'page');
  });

  test('similar is a full subpage that defers the vector search', async ({ page }) => {
    await goto(page, `/objects/${encodeURIComponent(canonicalId)}/similar`);
    await expectAppPage(page, new RegExp(key));

    await expect(
      page.getByRole('navigation', { name: 'Object sections' }).getByRole('link', { name: /Similar objects/ }),
    ).toHaveAttribute('aria-current', 'page');
    // The deferred fragment eventually swaps itself for the real card.
    await expect(page.getByTestId('object-similar-deferred')).toHaveCount(0, { timeout: 15_000 });
  });

  test('status control updates the object from its detail page', async ({ page }) => {
    await goto(page, `/objects/${encodeURIComponent(canonicalId)}`);
    await expectAppPage(page, new RegExp(key));

    const { menu } = await openDropdown(page, 'object-status-control', 'Change status');
    await expect(menu.getByRole('menuitem', { name: 'Done' })).toBeVisible();
    await menu.getByRole('menuitem', { name: 'Done' }).click();

    await expect(page.getByTestId('object-status-control')).toContainText('Done');
    await expect(page).toHaveURL(/updated=1/);
  });

  test('assignee control updates the object from its detail page', async ({ page }) => {
    const membersResp = await page.request.get('/api/members');
    if (!membersResp.ok()) {
      throw new Error(`GET /api/members failed (HTTP ${membersResp.status()})`);
    }
    const members = (await membersResp.json()) as MemberDto[];
    const target = memberLabel(members[0] ?? {});
    expect(target, 'a project member must exist to reassign').toBeTruthy();

    await goto(page, `/objects/${encodeURIComponent(canonicalId)}`);
    await expectAppPage(page, new RegExp(key));

    const { menu } = await openDropdown(page, 'object-assignee-control', 'Assignee');
    await menu.getByRole('menuitem', { name: target }).click();

    await expect(page.getByTestId('object-assignee-control')).toContainText(target);
    await expect(page).toHaveURL(/updated=1/);

    // Cross-check the work item's HEAD through the API.
    const projectId = readBootstrap()?.projectId;
    const headers = await memoryAuthHeaders(page);
    const resp = await page.request.get(
      `${MEMORY_API_URL}/api/projects/${projectId}/work-items/${encodeURIComponent(canonicalId)}`,
      { headers, failOnStatusCode: false },
    );
    if (resp.ok()) {
      const body = (await resp.json().catch(() => ({}))) as {
        data?: { item?: { assignee?: string } };
      };
      expect(body.data?.item?.assignee).toBe(target);
    }
  });
});
