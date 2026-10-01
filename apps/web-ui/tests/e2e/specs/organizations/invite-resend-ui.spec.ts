import { test, expect, Page } from '@playwright/test';
import { readBootstrap } from '../../helpers/bootstrap';

// Coverage for the sent-invite resend surface (org_members_ui.templ →
// POST /invites/:id/resend, uiResendInvite). A pending invitation renders, next
// to its "Not responded" lifecycle badge, an email-delivery badge (default
// "Sent" before any Mailgun event) plus a Resend submit control beside Revoke.
// The resend POST is guarded by a native confirm() and PRG-redirects back to the
// same surface with ?resent=1 and a success flash. Invitation creation reuses
// the same single-test-identity flow as invite-revoke-ui.spec.ts, so the whole
// lifecycle is coverable without a second Zitadel user.

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

test.describe('Invite resend', () => {
  test('resends a pending invite and stays pending with the default Sent badge', async ({ page }) => {
    const bootstrap = readBootstrap();
    expect(bootstrap, 'setup project must run first').toBeTruthy();

    // Invitations target the session's active project; pin it to the bootstrap
    // project so this spec is independent of whatever ran before it.
    if (bootstrap?.projectId) {
      const activate = await page.request.post(`/api/projects/${bootstrap.projectId}/activate`);
      expect(
        activate.ok(),
        `activating the bootstrap project failed (HTTP ${activate.status()})`,
      ).toBeTruthy();
    }

    const email = `e2e-invite-resend-${Date.now()}-${Math.floor(Math.random() * 1e4)}@example.com`;

    try {
      // Create the pending invitation from the invite form.
      await page.goto('/members/new');
      await page.locator('#invite-email').fill(email);
      await page.locator('#invite-role').selectOption('project_user');
      await page.getByRole('button', { name: 'Send invitation' }).click();
      await page.waitForURL(/\/members(\?|$)/, { timeout: 20_000 });
      await page.waitForLoadState('domcontentloaded');

      // Resulting state: a pending row with the subdued "Not responded"
      // lifecycle badge plus the default "Sent" delivery badge (no Mailgun
      // event has arrived for this brand-new address). The delivery-badge
      // assertion is scoped to the fresh invite only — a real delivery event
      // landing later would legitimately flip it to Delivered/Opened/etc.
      const pendingRow = rowForInvite(page, email);
      await expect(pendingRow).toBeVisible({ timeout: 15_000 });
      await expect(pendingRow.getByText('Not responded', { exact: true })).toBeVisible();
      await expect(pendingRow.getByText('Sent', { exact: true })).toBeVisible();

      // The resend control sits beside Revoke, labelled with the target email.
      const resendButton = pendingRow.getByRole('button', { name: `Resend invitation to ${email}` });
      await expect(resendButton).toBeVisible();

      // Resend: accept the native confirm() guard, then POST /invites/:id/resend
      // → PRG back to the same surface with ?resent=1 and a success flash.
      page.on('dialog', (d) => d.accept());
      const resendResp = page.waitForResponse(
        (r) => /\/invites\/[^/]+\/resend$/.test(r.url()) && r.request().method() === 'POST',
        { timeout: 20_000 },
      );
      await resendButton.click();
      await resendResp;
      await page.waitForURL(/\/members\?resent=1/, { timeout: 20_000 });
      await page.waitForLoadState('domcontentloaded');

      // Success flash surfaces (auto-dismisses after ~4s, so poll immediately).
      await expect
        .poll(async () => (await page.locator('body').innerText()).includes('Invitation resent.'), {
          timeout: 8000,
          intervals: [100, 200, 200, 500],
        })
        .toBeTruthy();

      // Resulting state: the invite is STILL present and STILL pending — a
      // resend must not remove the row or flip its lifecycle status. Assert the
      // DOM row and the API status, not merely a transient absence.
      const stillRow = rowForInvite(page, email);
      await expect(stillRow).toBeVisible({ timeout: 15_000 });
      await expect(stillRow.getByText('Not responded', { exact: true })).toBeVisible();
      await expect
        .poll(
          async () =>
            (await listInvites(page)).some(
              (inv) => inv.email === email && inv.status === 'pending',
            ),
          { timeout: 15_000, intervals: [200, 300, 500] },
        )
        .toBe(true);
    } finally {
      await cancelInviteByEmail(page, email).catch((e) =>
        console.warn(`[invite-resend-ui] cleanup skipped: ${(e as Error).message}`),
      );
    }
  });

  test('invite resend: no pending E2E invites left behind (self-cleanup guard)', async ({ page }) => {
    const pending = (await listInvites(page)).filter(
      (inv) => inv.email.startsWith('e2e-invite-resend-') && inv.status === 'pending',
    );
    expect(pending.map((p) => p.email)).toEqual([]);
  });
});
