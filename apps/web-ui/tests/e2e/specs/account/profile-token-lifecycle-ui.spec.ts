import { test, expect } from '@playwright/test';
import {
  cleanupTokens,
  createTokenViaUi,
  dismissReveal,
  expectNoLiveTokens,
  liveTokenRowsNamed,
  openScopesEditor,
  regenerateTokenViaUi,
  revokedTokenRowsNamed,
  revokeTokenViaUi,
  saveScopes,
  setScopes,
  tokenBasePath,
  tokenRowsNamed,
  uniqueTokenName,
} from '../../helpers/tokens';

// Account-scoped API token lifecycle (/profile/tokens +
// /profile/tokens/:tokenId/{edit,scopes,regenerate,revoke}) — the signed-in
// user's personal tokens, driven through the real UI. Same management model as
// the project surface (see gateway/api_tokens_handlers.go) with "Account"
// headings and the profile rail.
//
// Product behavior the assertions encode:
//   - create / regenerate render the plaintext exactly once in a one-shot
//     reveal panel (no redirect); the value never appears on any list again.
//   - revoking keeps the row as an audit trail with a "Revoked" badge and no
//     actions — "the token is gone" means no LIVE row remains.
//   - regenerate atomically revokes the original and inserts a replacement with
//     the same name + scopes, so two rows briefly share the name.
const NAME_PREFIX = 'E2E account token';

test('account token: create → edit scopes → regenerate → revoke', async ({ page }) => {
  test.setTimeout(120_000);
  const name = uniqueTokenName(NAME_PREFIX);

  try {
    // 1. CREATE — the create page re-renders with the one-shot plaintext panel.
    const secret = await createTokenViaUi(page, 'profile', name, ['projects:read']);
    await expect(page.getByText('Account token created')).toBeVisible();
    await expect(page.getByText('This value is shown only once. Store it somewhere safe.').first()).toBeVisible();
    expect(secret).toMatch(/^emt_[0-9a-f]{64}$/);

    // Navigating back to the list must NOT reveal the secret again.
    await dismissReveal(page);
    await expect(page.getByText(secret)).toHaveCount(0);

    const created = liveTokenRowsNamed(page, name);
    await expect(created).toHaveCount(1);
    await expect(created.getByText(`${secret.slice(0, 12)}…`)).toBeVisible();
    await expect(created.getByText('projects:read', { exact: true })).toBeVisible();

    // 2. EDIT SCOPES — POST /profile/tokens/:id/scopes (PRG back to the list).
    await openScopesEditor(page, name);
    await expect(page.locator('input[name="scopes"][value="projects:read"]')).toBeChecked();
    await expect(page.locator('input[name="scopes"][value="journal:read"]')).not.toBeChecked();
    await setScopes(page, ['projects:read', 'journal:read']);
    await saveScopes(page);

    const rescoped = liveTokenRowsNamed(page, name);
    await expect(rescoped).toHaveCount(1);
    await expect(rescoped.getByText('journal:read', { exact: true })).toBeVisible();
    await expect(rescoped.getByText('projects:read', { exact: true })).toBeVisible();

    // Persistence: the edit page pre-checks the new set on reload.
    await openScopesEditor(page, name);
    await expect(page.locator('input[name="scopes"][value="projects:read"]')).toBeChecked();
    await expect(page.locator('input[name="scopes"][value="journal:read"]')).toBeChecked();
    await page.goto(tokenBasePath('profile'));

    // 3. REGENERATE — POST /profile/tokens/:id/regenerate. The original is
    // revoked and a distinctly-valued replacement is shown once.
    const regenerated = await regenerateTokenViaUi(page, name);
    await expect(page.getByText('Account token regenerated')).toBeVisible();
    expect(regenerated).not.toBe(secret);

    // Two rows now share the name: the revoked original + the live replacement,
    // which inherits the edited scopes.
    await expect(tokenRowsNamed(page, name)).toHaveCount(2);
    await expect(liveTokenRowsNamed(page, name)).toHaveCount(1);
    await expect(liveTokenRowsNamed(page, name).getByText('journal:read', { exact: true })).toBeVisible();
    await expect(revokedTokenRowsNamed(page, name)).toHaveCount(1);

    // The replacement plaintext is not persisted to the list either.
    await dismissReveal(page);
    await expect(page.getByText(regenerated)).toHaveCount(0);

    // 4. REVOKE — POST /profile/tokens/:id/revoke (confirm-gated PRG).
    await revokeTokenViaUi(page, name);
    await expect(liveTokenRowsNamed(page, name)).toHaveCount(0);
    await expect(revokedTokenRowsNamed(page, name)).toHaveCount(2);
    await expect(page.getByRole('button', { name: `Revoke ${name}` })).toHaveCount(0);
    await expect(page.getByRole('button', { name: `Regenerate ${name}` })).toHaveCount(0);
  } finally {
    await cleanupTokens(page, 'profile', [name]);
  }
});

test('account token: no live E2E account tokens left behind (self-cleanup guard)', async ({ page }) => {
  await expectNoLiveTokens(page, 'profile', NAME_PREFIX);
});
