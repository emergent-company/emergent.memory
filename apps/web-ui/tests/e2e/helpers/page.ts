import { Page, expect, type Response } from '@playwright/test';
import { scopedPath } from './scope';

/**
 * Assert a page loaded under session auth (no redirect to /auth/login) and
 * shows the expected browser title. The gateway renders `<title><label> — Memory</title>`.
 */
export async function expectAppPage(page: Page, title: RegExp): Promise<void> {
  await expect(page).not.toHaveURL(/\/auth\/login/);
  await expect(page).toHaveTitle(title);
}

/**
 * Navigate to an app path under the bootstrap's scoped URL (scoped-web-urls).
 *
 * This is the single navigation entry point for session-mode specs: it routes
 * the path through `scopedPath` so project app surfaces carry the
 * `/o/{org}/p/{project}` prefix and legacy `/orgs/{id}` pages land on `/o/{id}`.
 * Global surfaces (`/orgs`, `/members`, `/profile`, `/auth/…`, API/asset paths)
 * and already-scoped `/o/…` paths pass through unchanged.
 *
 * `scope` overrides the bootstrap ids — used by `auth.setup.ts`, which
 * navigates (to install the blueprint) before `writeBootstrap` persists them.
 */
export async function goto(
  page: Page,
  path: string,
  scope?: { orgId?: string; projectId?: string },
): Promise<Response | null> {
  return page.goto(scopedPath(path, scope));
}
