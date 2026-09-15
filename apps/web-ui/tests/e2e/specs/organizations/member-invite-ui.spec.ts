import { test, expect } from '@playwright/test';

test('invites a member via the form', async ({ page }) => {
  const email = `e2e-invite-${Date.now()}@example.com`;

  try {
    await page.goto('/members/new');
    await page.locator('#invite-email').fill(email);
    await page.getByRole('button', { name: 'Send invitation' }).click();

    // Redirects back to the members list; the pending invite shows the email.
    await page.waitForURL(/\/members/);
    await expect(page.getByText(email).first()).toBeVisible();
  } finally {
    // Best-effort cleanup: cancel the pending invite.
    try {
      const invites = (await (await page.request.get('/api/invites')).json()) as Array<{
        id: string;
        email: string;
      }>;
      for (const inv of invites ?? []) {
        if (inv.email === email) await page.request.delete(`/api/invites/${inv.id}`);
      }
    } catch {
      /* ignore */
    }
  }
});
