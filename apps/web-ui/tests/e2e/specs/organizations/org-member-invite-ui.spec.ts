import { test, expect, Page } from '@playwright/test';
import { readBootstrap } from '../../helpers/bootstrap';
import { goto } from '../../helpers/page';

interface InviteRef {
  id: string;
  email: string;
  status: string;
}

async function listInvites(page: Page): Promise<InviteRef[]> {
  const resp = await page.request.get('/api/invites');
  if (!resp.ok()) return [];
  return (await resp.json()) as InviteRef[];
}

async function cancelInviteByEmail(page: Page, email: string) {
  for (const inv of await listInvites(page)) {
    if (inv.email === email) {
      await page.request.delete(`/api/invites/${inv.id}`).catch(() => {});
    }
  }
}

// Organization-level member management: invite a plain member from the org
// members view, see it listed as a not-yet-responded member, then revoke it.
// Direct-add and role change need a second registered identity and are covered
// by the gateway handler tests.
test.describe('Org member invite', () => {
  test('invites a member at the organization level and revokes the pending invite', async ({ page }) => {
    const bootstrap = readBootstrap();
    expect(bootstrap, 'setup project must run first').toBeTruthy();

    const email = `e2e-org-member-${Date.now()}-${Math.floor(Math.random() * 1e4)}@example.com`;

    try {
      await goto(page, `/orgs/${bootstrap!.orgId}/members`);

      // The members view exposes both actions to an org admin.
      await expect(page.getByRole('link', { name: 'Invite member' })).toBeVisible();
      await expect(page.getByRole('link', { name: 'Add member' })).toBeVisible();

      await page.getByRole('link', { name: 'Invite member' }).click();
      await page.locator('#org-invite-email').fill(email);
      await page.locator('#org-invite-role').selectOption('org_member');
      await page.getByRole('button', { name: 'Send invitation' }).click();

      await page.waitForURL(new RegExp(`/orgs/${bootstrap!.orgId}/members`));
      const row = page.locator('tr').filter({ has: page.getByText(email, { exact: true }) });
      await expect(row).toBeVisible();
      await expect(row.getByText('Not responded')).toBeVisible();

      // Revoke from the pending row.
      await row.getByRole('button', { name: `Revoke invite for ${email}` }).click();
      await expect(page.getByText(email, { exact: true })).toHaveCount(0);
    } finally {
      await cancelInviteByEmail(page, email);
    }
  });
});
