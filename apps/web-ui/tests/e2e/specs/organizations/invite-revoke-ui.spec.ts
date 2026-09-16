import { test, expect, Page } from '@playwright/test';
import { readBootstrap } from '../../helpers/bootstrap';

// Coverage for the sent-invite revoke surface (org_members_ui.templ →
// POST /invites/:id/revoke, uiRevokeInvite). A pending invitation can be
// created for an arbitrary email from /members/new with the single test
// identity, so the whole lifecycle is coverable without a second Zitadel user:
// invite → it renders as pending in the members table → revoke from its row →
// it leaves the pending list, both on the page and in the API.

interface InviteRef {
  id: string;
  email: string;
  status: string; // pending | accepted | declined | revoked | expired
}

async function listInvites(page: Page): Promise<InviteRef[]> {
  const resp = await page.request.get('/api/invites');
  if (!resp.ok()) throw new Error(`list invites failed: ${resp.status()}`);
  return (await resp.json()) as InviteRef[];
}

function rowForInvite(page: Page, email: string) {
  return page.locator('tr').filter({ has: page.getByText(email, { exact: true }) });
}

// Best-effort cleanup: cancel any invitation left behind for this email so a
// failed run never accumulates pending invites across runs (D3).
async function cancelInviteByEmail(page: Page, email: string) {
  const invites = await listInvites(page).catch(() => [] as InviteRef[]);
  for (const inv of invites) {
    if (inv.email === email) {
      await page.request.delete(`/api/invites/${inv.id}`).catch(() => {});
    }
  }
}

test.describe('Invite revoke', () => {
  test('creates a pending invite, then revokes it from the members list', async ({ page }) => {
    const bootstrap = readBootstrap();
    expect(bootstrap, 'setup project must run first').toBeTruthy();

    // Invitations target the session's active project; pin it to the bootstrap
    // project so this spec is independent of whatever ran before it.
    if (bootstrap?.projectId) {
      await page.request.post(`/api/projects/${bootstrap.projectId}/activate`);
    }

    const email = `e2e-invite-revoke-${Date.now()}-${Math.floor(Math.random() * 1e4)}@example.com`;

    try {
      // Create the pending invitation from the invite form.
      await page.goto('/members/new');
      await page.locator('#invite-email').fill(email);
      await page.locator('#invite-role').selectOption('project_user');
      await page.getByRole('button', { name: 'Send invitation' }).click();
      await page.waitForURL(/\/members(\?|$)/, { timeout: 20_000 });
      await page.waitForLoadState('domcontentloaded');

      // Resulting state: the invitation is listed as pending — its email plus
      // the subdued "Not responded" badge, in a row of its own.
      const pendingRow = rowForInvite(page, email);
      await expect(pendingRow).toBeVisible({ timeout: 15_000 });
      await expect(pendingRow.getByText('Not responded', { exact: true })).toBeVisible();

      // Revoke it via the row's labelled action → native POST
      // /invites/:id/revoke → PRG back to the members page.
      const revokeResp = page.waitForResponse(
        (r) => /\/invites\/[^/]+\/revoke$/.test(r.url()) && r.request().method() === 'POST',
        { timeout: 20_000 },
      );
      await pendingRow.getByRole('button', { name: `Revoke invite for ${email}` }).click();
      await revokeResp;
      await page.waitForURL(/\/members\?revoked=1/, { timeout: 20_000 });
      await page.waitForLoadState('domcontentloaded');

      // Resulting state: the pending invite is gone — not merely hidden by a
      // transient toast. The members list no longer renders it, and the API no
      // longer reports it as pending (revoked invites are retained with
      // status "revoked", so assert on the status, not on absence).
      await expect
        .poll(
          async () => page.getByText(email, { exact: true }).count(),
          { timeout: 15_000, intervals: [200, 300, 500] },
        )
        .toBe(0);
      await expect
        .poll(
          async () =>
            (await listInvites(page)).some(
              (inv) => inv.email === email && inv.status === 'pending',
            ),
          { timeout: 15_000, intervals: [200, 300, 500] },
        )
        .toBe(false);
    } finally {
      await cancelInviteByEmail(page, email).catch((e) =>
        console.warn(`[invite-revoke-ui] cleanup skipped: ${(e as Error).message}`),
      );
    }
  });
});
