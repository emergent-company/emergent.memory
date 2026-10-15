import { readBootstrap } from './bootstrap';

/**
 * Scoped URL helpers (scoped-web-urls). Org/project context lives in the path:
 * org pages at `/o/{orgId}`, project pages at `/o/{orgId}/p/{projectId}`.
 *
 * The global-surface list mirrors the gateway's `globalScopePath` (scope.go):
 * these paths are never org/project-scoped, so an unscoped GET is valid and the
 * helpers pass them through unchanged. Everything else — the app surfaces —
 * gets the project scope prefix, and legacy `/orgs/{id}` pages map onto the
 * canonical `/o/{id}` org form (mirrors the gateway's `scopedPathFor`).
 */
const GLOBAL_PREFIXES = [
  '/o/',
  '/api/',
  '/auth/',
  '/share/',
  '/static/',
  '/assets/',
  '/webhooks/',
  '/internal/',
  '/dev/',
  '/members',
  '/invites',
  '/profile',
  '/partial/invite-user-search',
];

/** Canonical org-scoped URL: `/o/{orgId}` + optional sub-path. */
export function orgScopedPath(orgId: string, subPath = ''): string {
  return `/o/${orgId}${subPath}`;
}

/**
 * Scope an app path for the bootstrap (or explicit) org/project. Legacy
 * `/orgs/{id}...` pages map onto `/o/{id}...`; `/orgs` and `/orgs/new` stay
 * global. Global surfaces pass through unchanged. Falls back to the path
 * unchanged when no ids are available (e.g. dev-mode harness) or the path is
 * global/already-scoped.
 */
export function scopedPath(
  appPath: string,
  ids?: { orgId?: string; projectId?: string },
): string {
  const bootstrap = readBootstrap();
  const orgId = ids?.orgId ?? bootstrap?.orgId;
  const projectId = ids?.projectId ?? bootstrap?.projectId;
  if (!appPath || !appPath.startsWith('/') || appPath.startsWith('//')) {
    return appPath;
  }
  // Legacy org-scoped pages move to the canonical /o/{id} form (org context,
  // not project); the org list and create page stay global.
  if (appPath.startsWith('/orgs/') && appPath !== '/orgs/new') {
    return `/o/${appPath.slice('/orgs/'.length)}`;
  }
  if (appPath === '/orgs' || appPath === '/orgs/new') {
    return appPath;
  }
  if (!orgId || !projectId) {
    return appPath;
  }
  if (appPath === '/') {
    return `/o/${orgId}/p/${projectId}`;
  }
  if (GLOBAL_PREFIXES.some((prefix) => appPath.startsWith(prefix))) {
    return appPath;
  }
  return `/o/${orgId}/p/${projectId}${appPath}`;
}
