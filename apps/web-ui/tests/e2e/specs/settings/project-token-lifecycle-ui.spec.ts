import { test, expect } from '@playwright/test';
import {
  cleanupTokens,
  createTokenViaUi,
  dismissReveal,
  expectNoLiveTokens,
  expectScopeAreaBadge,
  expectTokenAuthenticated,
  expectTokenRejected,
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

// Project-scoped API token lifecycle (/settings/tokens +
// /settings/tokens/:tokenId/{edit,scopes,regenerate,revoke}) driven entirely
// through the real UI. See gateway/api_tokens_handlers.go.
//
// Product behavior the assertions encode:
//   - create / regenerate render the plaintext exactly once in a one-shot
//     reveal panel (no redirect); the value never appears on any list again.
//   - revoking keeps the row as an audit trail with a "Revoked" badge and no
//     actions — "the token is gone" means no LIVE row remains.
//   - regenerate atomically revokes the original and inserts a replacement with
//     the same name + scopes, so two rows briefly share the name.
const NAME_PREFIX = 'E2E project token';

test('project token: create → edit scopes → regenerate → revoke', async ({ page }) => {
  test.setTimeout(120_000);
  const name = uniqueTokenName(NAME_PREFIX);
  let secret: string;
  let regenerated: string;

  try {
    // 1. CREATE — the create page re-renders with the one-shot plaintext panel.
    await test.step('create', async () => {
      secret = await createTokenViaUi(page, 'project', name, ['data:read']);
      await expect(page.getByText('API token created')).toBeVisible();
      await expect(page.getByText('This value is shown only once. Store it somewhere safe.').first()).toBeVisible();
      expect(secret).toMatch(/^emt_[0-9a-f]{64}$/);

      // Navigating back to the list must NOT reveal the secret again.
      await dismissReveal(page);
      await expect(page.getByText(secret)).toHaveCount(0);

      const created = liveTokenRowsNamed(page, name);
      await expect(created).toHaveCount(1);
      await expect(created.getByText(`${secret.slice(0, 12)}…`)).toBeVisible();
      await expectScopeAreaBadge(created, 'Data', ['data:read']);
    });

    // 2. EDIT SCOPES — POST /settings/tokens/:id/scopes (PRG back to the list).
    await test.step('edit scopes', async () => {
      await openScopesEditor(page, name);
      await expect(page.locator('input[name="scopes"][value="data:read"]')).toBeChecked();
      await expect(page.locator('input[name="scopes"][value="search"]')).not.toBeChecked();
      await setScopes(page, ['data:read', 'search']);
      await saveScopes(page);

      const rescoped = liveTokenRowsNamed(page, name);
      await expect(rescoped).toHaveCount(1);
      // data:read stays in Data; search lands in Graph (one badge per area)
      await expectScopeAreaBadge(rescoped, 'Data', ['data:read']);
      await expectScopeAreaBadge(rescoped, 'Graph', ['search']);

      // Persistence: the edit page pre-checks the new set on reload.
      await openScopesEditor(page, name);
      await expect(page.locator('input[name="scopes"][value="data:read"]')).toBeChecked();
      await expect(page.locator('input[name="scopes"][value="search"]')).toBeChecked();
      await page.goto(tokenBasePath('project'));
    });

    // 3. REGENERATE — POST /settings/tokens/:id/regenerate. The original is
    // revoked and a distinctly-valued replacement is shown once.
    await test.step('regenerate', async () => {
      regenerated = await regenerateTokenViaUi(page, name);
      await expect(page.getByText('API token regenerated')).toBeVisible();
      expect(regenerated).not.toBe(secret);

      // Two rows now share the name: the revoked original + the live replacement,
      // which inherits the edited scopes.
      await expect(tokenRowsNamed(page, name)).toHaveCount(2);
      await expect(liveTokenRowsNamed(page, name)).toHaveCount(1);
      await expectScopeAreaBadge(liveTokenRowsNamed(page, name), 'Graph', ['search']);
      await expect(revokedTokenRowsNamed(page, name)).toHaveCount(1);

      // The replacement plaintext is not persisted to the list either.
      await dismissReveal(page);
      await expect(page.getByText(regenerated)).toHaveCount(0);
    });

    // 3b. The pre-regeneration secret is really dead: the memory API rejects it
    // with 401 while the replacement is still accepted (else the stale check
    // could pass trivially).
    await test.step('regenerate invalidates the previous secret', async () => {
      await expectTokenAuthenticated(page.request, regenerated);
      await expectTokenRejected(page.request, secret);
    });

    // 4. REVOKE — POST /settings/tokens/:id/revoke (confirm-gated PRG).
    await test.step('revoke', async () => {
      await revokeTokenViaUi(page, name);
      await expect(liveTokenRowsNamed(page, name)).toHaveCount(0);
      await expect(revokedTokenRowsNamed(page, name)).toHaveCount(2);
      await expect(page.getByRole('button', { name: `Revoke ${name}` })).toHaveCount(0);
      await expect(page.getByRole('button', { name: `Regenerate ${name}` })).toHaveCount(0);
    });
  } finally {
    await cleanupTokens(page, 'project', [name]);
  }
});

test('project token: no live E2E project tokens left behind (self-cleanup guard)', async ({ page }) => {
  await expectNoLiveTokens(page, 'project', NAME_PREFIX);
});
