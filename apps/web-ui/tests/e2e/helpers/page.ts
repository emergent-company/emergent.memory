import { Page, expect } from '@playwright/test';

/**
 * Assert a page loaded under session auth (no redirect to /auth/login) and
 * shows the expected browser title. The gateway renders `<title><label> — Memory</title>`.
 */
export async function expectAppPage(page: Page, title: RegExp): Promise<void> {
  await expect(page).not.toHaveURL(/\/auth\/login/);
  await expect(page).toHaveTitle(title);
}
