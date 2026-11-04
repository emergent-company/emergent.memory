import { test, expect } from '@playwright/test';
import { goto } from '../../helpers/page';

// The copy control writes through navigator.clipboard, so grant the permission
// up front — otherwise the button shows its failure state and the confirmation
// assertion below could never pass (#1964).
test.use({ permissions: ['clipboard-read', 'clipboard-write'] });

test('creates a token and reveals it in a modal with copy confirmation', async ({ page }) => {
  await goto(page, '/settings/tokens/new');
  await page.locator('#api-token-name').fill(`e2e-token-${Date.now()}`);
  // At least one scope is required.
  await page.locator('input[name="scopes"]').first().check();
  await page.getByRole('button', { name: 'Create token' }).click();

  // The plaintext appears in the one-shot reveal dialog (auto-opened), not as an
  // inline card in the page body (#1954).
  const reveal = page.locator('dialog#api-token-reveal-modal');
  await expect(reveal).toBeVisible();
  await expect(reveal.locator('#api-token-secret')).toBeVisible();

  // Copying confirms on the button itself, so the click never looks ignored
  // (#1964). form.ClipboardCopy rewrites the button's innerHTML to "✓ Copied!"
  // for two seconds, so the accessible name mutates mid-test and a
  // `getByRole('button', { name: 'Copy' })` locator stops matching the moment
  // the confirmation it is waiting for appears. Resolve the control by the
  // target it is wired to instead — that attribute is never rewritten.
  const copy = reveal.locator('button[onclick*="api-token-secret"]');
  await expect(copy).toHaveText('Copy');
  await copy.click();
  await expect(copy).toContainText('Copied');

  // The confirmation only fires after a completed write, but assert the payload
  // too: the token the user was promised is what actually reached the clipboard.
  const token = await reveal.locator('#api-token-secret').inputValue();
  await expect
    .poll(async () => page.evaluate(() => navigator.clipboard.readText()), { timeout: 5_000 })
    .toBe(token);

  // Done leaves the dialog for the token list, so the empty create form is not
  // left waiting to be submitted again.
  await reveal.getByRole('link', { name: "Done — I've saved the key" }).click();
  await expect(page).toHaveURL(/\/tokens$/);
});
