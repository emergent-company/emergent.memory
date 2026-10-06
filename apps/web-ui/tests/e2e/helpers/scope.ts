import { readBootstrap } from './bootstrap';

/**
 * Scoped URL helpers (scoped-web-urls). Org/project context lives in the path:
 * org pages at `/o/{orgId}`, project pages at `/o/{orgId}/p/{projectId}`.
 *
 * Global surfaces (api/auth/share/static/assets/webhooks/invites/profile) and
 * the org list stay unscoped; already-scoped paths pass through.
 */
const GLOBAL_PATH =
  /^\/(api|auth|share|static|assets|webhooks|internal|dev|invites|profile)(\/|$)|^\/orgs(\/|$)|^\/o\//;

/** Canonical org-scoped URL: `/o/{orgId}` + optional sub-path. */
export function orgScopedPath(orgId: string, subPath = ''): string {
  return `/o/${orgId}${subPath}`;
}

/**
 * Scope a project app path for the bootstrap (or explicit) org/project.
 * Falls back to the path unchanged when no ids are available or it is global.
 */
export function scopedPath(
  appPath: string,
  ids?: { orgId?: string; projectId?: string },
): string {
  const bootstrap = readBootstrap();
  const orgId = ids?.orgId ?? bootstrap?.orgId;
  const projectId = ids?.projectId ?? bootstrap?.projectId;
  if (!orgId || !projectId) return appPath;
  if (!appPath.startsWith('/') || GLOBAL_PATH.test(appPath)) return appPath;
  return `/o/${orgId}/p/${projectId}${appPath}`;
}
