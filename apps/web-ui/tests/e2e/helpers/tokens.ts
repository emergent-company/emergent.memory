import { Page, Locator, expect } from '@playwright/test';

/**
 * Shared helper for the API token management surfaces:
 *   - project tokens: /settings/tokens  (active project's scoped tokens)
 *   - account tokens: /profile/tokens   (signed-in user's account tokens)
 *
 * Both surfaces share one management model (see gateway/api_tokens_handlers.go):
 * a TABLE list, a standalone create page at <base>/new that renders the plaintext
 * exactly once in a one-shot reveal panel, a per-token edit-scopes page at
 * <base>/:tokenId/edit, and regenerate / revoke actions on each live row.
 *
 * Locators are semantic where the gateway already provides a stable anchor —
 * the reveal panel's #api-token-secret, the scope checkboxes' name/value, and
 * the per-row actions' aria-labels ("Regenerate <name>", "Edit scopes of
 * <name>", "Revoke <name>"). The list is scoped to the token table inside
 * #main-content so the surrounding chrome / profile rail can never match.
 */
export type TokenSurface = 'project' | 'profile';

/** Base path of a token surface. */
export function tokenBasePath(surface: TokenSurface): string {
  return surface === 'profile' ? '/profile/tokens' : '/settings/tokens';
}

/**
 * Token names use the `emt_` prefix + 64 hex chars; the list shows only the
 * first 12 chars (see server domain/apitoken service.go getTokenPrefix).
 */
const TOKEN_VALUE = /^emt_[0-9a-f]{64}$/;

/** A stable, unique name per call so a spec only ever matches its own tokens. */
export function uniqueTokenName(prefix: string): string {
  const nonce = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`;
  return `${prefix} ${nonce}`;
}

/**
 * Token table rows on a list page (both surfaces render exactly one table).
 * Rows are dynamic and, after a regenerate, two rows share the same name — so
 * they carry a `data-testid="token-row"` anchor. The `.or(...)` keeps the suite
 * working against gateway builds from before that anchor landed (the CSS
 * fallback is structural, so `or` de-duplicates to one match per row).
 */
export function tokenRows(page: Page): Locator {
  return page
    .getByTestId('token-row')
    .or(page.locator('#main-content table tbody tr'));
}

/**
 * The "Revoked" badge. The state is the only way to tell the revoked original
 * from its live replacement after a regenerate, so it is anchored by
 * `data-testid="token-revoked-badge"`; the label-based fallback covers pre-anchor
 * gateway builds.
 */
export function revokedBadge(page: Page): Locator {
  return page
    .getByTestId('token-revoked-badge')
    .or(page.locator('span.badge').filter({ hasText: 'Revoked' }));
}

/** Rows whose content contains `name` (a revoked row keeps the name, so match may return several). */
export function tokenRowsNamed(page: Page, name: string): Locator {
  return tokenRows(page).filter({ hasText: name });
}

/** Revoked rows for `name` — the audit-trail rows left behind by a revoke. */
export function revokedTokenRowsNamed(page: Page, name: string): Locator {
  return tokenRowsNamed(page, name).filter({ has: revokedBadge(page) });
}

/** Live (non-revoked) rows for `name` — the ones that carry the row actions. */
export function liveTokenRowsNamed(page: Page, name: string): Locator {
  return tokenRowsNamed(page, name).filter({ hasNot: revokedBadge(page) });
}

/**
 * Read the one-time plaintext from the reveal panel shown after a create or
 * regenerate POST. Asserts the panel is visible and the value has the `emt_`
 * shape, then returns it. The value is never persisted anywhere else.
 *
 * The panel itself is anchored by `data-testid="token-secret-panel"`; the value
 * always has `#api-token-secret`. Pre-anchor gateway builds only expose the
 * latter, so the panel visibility check falls back to it.
 */
export async function readOneTimeSecret(page: Page): Promise<string> {
  const byTestId = page.getByTestId('token-secret-panel');
  const panel = (await byTestId.count()) > 0 ? byTestId : page.locator('#api-token-secret');
  await expect(panel).toBeVisible();
  const secret = ((await page.locator('#api-token-secret').textContent()) ?? '').trim();
  if (!TOKEN_VALUE.test(secret)) {
    throw new Error(`readOneTimeSecret: reveal panel value is not an emt_ token (got ${JSON.stringify(secret)})`);
  }
  return secret;
}

/** Check exactly the given scope checkboxes on the create/edit form. */
export async function setScopes(page: Page, scopes: string[]): Promise<void> {
  const boxes = page.locator('input[name="scopes"]');
  const total = await boxes.count();
  expect(total, 'scope picker rendered no scope checkboxes').toBeGreaterThan(0);
  for (let i = 0; i < total; i++) {
    const box = boxes.nth(i);
    const value = (await box.getAttribute('value')) ?? '';
    if (scopes.includes(value)) {
      await box.check();
    } else {
      await box.uncheck();
    }
  }
}

/**
 * Create a token through the real UI: <base>/new → name + scopes → submit. The
 * create page re-renders with the one-shot reveal panel (no redirect, the
 * plaintext must reach the browser); returns the plaintext value.
 */
export async function createTokenViaUi(
  page: Page,
  surface: TokenSurface,
  name: string,
  scopes: string[],
): Promise<string> {
  await page.goto(`${tokenBasePath(surface)}/new`);
  const nameInput = page.locator('#api-token-name');
  await expect(nameInput).toBeVisible();
  await nameInput.fill(name);
  await setScopes(page, scopes);
  await page.getByRole('button', { name: 'Create token' }).click();
  return readOneTimeSecret(page);
}

/** Follow the reveal panel's "Done" link back to the token list. */
export async function dismissReveal(page: Page): Promise<void> {
  await page.getByRole('link', { name: 'Done' }).click();
  await expect(page.getByRole('link', { name: 'New token' })).toBeVisible();
}

/** Open a live token's edit-scopes page from the list. */
export async function openScopesEditor(page: Page, name: string): Promise<void> {
  await liveTokenRowsNamed(page, name)
    .first()
    .getByRole('link', { name: `Edit scopes of ${name}` })
    .click();
  await expect(page.getByRole('button', { name: 'Save scopes' })).toBeVisible();
  await expect(page.locator('input[name="scopes"]').first()).toBeAttached();
}

/**
 * Save the currently checked scope picker on the edit page. The scope POST is a
 * PRG flow (redirect to the list with ?updated=1). Waits for that round-trip.
 */
export async function saveScopes(page: Page): Promise<void> {
  await page.getByRole('button', { name: 'Save scopes' }).click();
  await page.waitForURL(/\?updated=1/, { timeout: 15_000 });
}

/**
 * Regenerate a live token from its list row. The form is gated by a native
 * confirm() dialog (hx-confirm on a hx-boosted form); accept it, then read the
 * replacement plaintext from the reveal panel on the re-rendered list page.
 */
export async function regenerateTokenViaUi(page: Page, name: string): Promise<string> {
  const row = liveTokenRowsNamed(page, name).first();
  await expect(row).toBeVisible();
  page.once('dialog', (dialog) => dialog.accept());
  await row.getByRole('button', { name: `Regenerate ${name}` }).click();
  return readOneTimeSecret(page);
}

/**
 * Revoke a live token from its list row via the confirm-gated revoke form.
 * Resolves once no live row for `name` remains (the revoked row stays in the
 * list with a "Revoked" badge and no actions).
 */
export async function revokeTokenViaUi(page: Page, name: string): Promise<void> {
  const row = liveTokenRowsNamed(page, name).first();
  await expect(row).toBeVisible();
  page.once('dialog', (dialog) => dialog.accept());
  await row.getByRole('button', { name: `Revoke ${name}` }).click();
  await expect(liveTokenRowsNamed(page, name)).toHaveCount(0);
}

/**
 * Best-effort cleanup: revoke every live token this run created. Tokens cannot
 * be deleted (revoked rows remain as an audit trail), so "cleaned up" means no
 * live row is left behind. Never throws — safe to call from `finally`.
 */
export async function cleanupTokens(
  page: Page,
  surface: TokenSurface,
  names: string[],
): Promise<void> {
  try {
    const base = tokenBasePath(surface);
    await page.goto(base).catch(() => {});
    for (const name of names) {
      // Bounded: a name can have at most one live row per regenerate cycle.
      for (let guard = 0; guard < 10; guard++) {
        const live = liveTokenRowsNamed(page, name);
        if ((await live.count().catch(() => 0)) === 0) break;
        const href = await live
          .first()
          .getByRole('link', { name: `Edit scopes of ${name}` })
          .getAttribute('href', { timeout: 5_000 })
          .catch(() => null);
        const id = href?.match(/\/tokens\/([^/]+)\/edit$/)?.[1];
        if (!id) break;
        const resp = await page.request
          .post(`${base}/${encodeURIComponent(id)}/revoke`)
          .catch(() => null);
        if (!resp || !resp.ok()) break;
        await page.reload().catch(() => {});
      }
    }
  } catch {
    // Best-effort only: the spec's own finally must not mask the real failure.
  }
}

/**
 * Guard: assert no LIVE token carrying `prefix` remains on the surface. Revoked
 * rows are expected (the product keeps them); only live leaks are a failure.
 */
export async function expectNoLiveTokens(
  page: Page,
  surface: TokenSurface,
  prefix: string,
): Promise<void> {
  await page.goto(tokenBasePath(surface));
  const leaked = tokenRows(page)
    .filter({ hasText: prefix })
    .filter({ hasNot: revokedBadge(page) });
  await expect(leaked).toHaveCount(0);
}
