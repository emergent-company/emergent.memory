import { test, expect, type Page } from '@playwright/test';
import { expectTokenAuthenticated, expectTokenRejected } from '../../helpers/tokens';

// MCP Sharing lifecycle: gateway/mcp_shares_handlers.go + mcp_shares.templ,
// the project-scoped share surface at /settings/mcp-servers/shares. A share
// mints a scoped API key exposing a chosen set of the platform's memory tools
// to an outside MCP client.
//
// One comprehensive test walks the full lifecycle — create (with the one-time
// key reveal, no redirect) → list row → edit/update (?updated=1) → rotate
// (second one-time key) → revoke (?revoked=1) — and self-cleans via the JSON
// API in `finally`, so the shared bootstrap tenant is never left with a live
// share. The create step requires at least one catalog tool; the spec probes
// the picker and skips with a stated reason when the backend reports an empty
// catalog (the same defensive pattern as agent-mcp-keys-ui.spec.ts).

const SHARES = '/settings/mcp-servers/shares';
const SHARES_NEW = `${SHARES}/new`;
const SHARES_EDIT = (id: string) => `${SHARES}/${encodeURIComponent(id)}/edit`;

/** Read the one-time key from the reveal modal's secret element (#mcp-share-key). */
async function revealToken(page: Page): Promise<string> {
  const secret = page.locator('#mcp-share-key');
  await expect(secret).toBeVisible();
  const token = ((await secret.textContent()) ?? '').trim();
  expect(token, 'the reveal must carry a non-empty raw key').not.toBe('');
  return token;
}

test('MCP share lifecycle: create, reveal once, edit, rotate, revoke', async ({ page }) => {
  const name = `E2E Share ${Date.now()}`;
  const description = 'read-only analyst access';
  const updatedDescription = 'updated access note';
  let id = '';

  try {
    // --- create ---
    await page.goto(SHARES_NEW);
    await expect(page.locator('#mcp-share-name')).toBeVisible();

    // The share requires ≥1 tool; skip cleanly when the catalog is empty.
    const toolBoxes = page.locator('[data-mcp-tool-option] input[name="tools"]');
    if ((await toolBoxes.count()) === 0) {
      test.skip(true, 'memory tool catalog is empty — cannot create a share');
    }

    await page.locator('#mcp-share-name').fill(name);
    await page.locator('#mcp-share-description').fill(description);
    await toolBoxes.first().check();
    await page.getByRole('button', { name: 'Create share' }).click();

    // Success re-renders the list with the reveal modal open (no redirect).
    const createdToken = await revealToken(page);

    // The new share row is present in the list behind the modal; close the
    // modal and read the row's id.
    await page.getByRole('button', { name: "Done — I've saved the key" }).click();
    const row = page.locator('[data-mcp-share-row]').filter({ hasText: name }).first();
    await expect(row).toBeVisible();
    id = (await row.getAttribute('data-mcp-share-row')) ?? '';
    expect(id, 'the share row must carry its id').not.toBe('');

    // --- revealed once ---
    // A plain list load never carries the key.
    await page.goto(SHARES);
    await expect(page.locator('#mcp-share-key')).toHaveCount(0);
    await expect(page.locator('body')).not.toContainText(createdToken);

    // --- edit/update ---
    await page.goto(SHARES_EDIT(id));
    await expect(page.locator('#mcp-share-name')).toHaveValue(name);
    await page.locator('#mcp-share-description').fill(updatedDescription);
    await page.getByRole('button', { name: 'Save changes' }).click();
    await page.waitForURL(/\/shares\?updated=1/);
    await page.goto(SHARES);
    await expect(
      page.locator('[data-mcp-share-row]').filter({ hasText: name }),
    ).toContainText(updatedDescription);

    // --- rotate ---
    // Rotation issues a new key shown once; the previous key is invalidated.
    const rotateRow = page.locator('[data-mcp-share-row]').filter({ hasText: name }).first();
    await rotateRow.getByRole('button', { name: `Rotate key for ${name}` }).click();
    await page
      .locator('#mcp-share-rotate-' + id)
      .getByRole('button', { name: 'Rotate key' })
      .click();
    const rotatedToken = await revealToken(page);
    expect(rotatedToken).not.toBe(createdToken);

    // Rotation must invalidate the previous key, not merely issue a new one: the
    // memory API rejects the stale key with 401 while the replacement is still
    // accepted (else the stale check could pass trivially).
    await expectTokenAuthenticated(page.request, rotatedToken);
    await expectTokenRejected(page.request, createdToken);

    // --- revoke ---
    await page.getByRole('button', { name: "Done — I've saved the key" }).click();
    const revokeRow = page.locator('[data-mcp-share-row]').filter({ hasText: name }).first();
    await revokeRow.getByRole('button', { name: `Revoke ${name}` }).click();
    await page
      .locator('#mcp-share-revoke-' + id)
      .getByRole('button', { name: 'Revoke share' })
      .click();
    await page.waitForURL(/\/shares\?revoked=1/);
    await expect(page.locator('[data-mcp-share-row]').filter({ hasText: name })).toHaveCount(0);
    id = ''; // already revoked — skip the API cleanup
  } finally {
    if (id) {
      await page.request.delete(`/api/mcp-shares/${id}`).catch(() => {});
    }
  }
});
