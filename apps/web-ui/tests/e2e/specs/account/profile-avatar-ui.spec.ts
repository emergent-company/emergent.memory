import { test, expect, Page } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

// Minimal 1x1 PNG. The backend validates by magic bytes (http.DetectContentType),
// so any buffer with the PNG signature is accepted as image/png.
const PNG = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=',
  'base64',
);

/** Open the profile-photo editor (click avatar → modal) and return the dialog. */
async function openModal(page: Page) {
  await page.getByRole('button', { name: 'Edit profile photo' }).click();
  const dialog = page.locator('dialog#profile-avatar-modal');
  await expect(dialog).toBeVisible();
  return dialog;
}

test('uploads and removes a profile photo via the modal', async ({ page }) => {
  await page.goto('/profile');
  await expectAppPage(page, /Profile/);

  // Idempotent cleanup: remove any leftover photo from a prior (possibly failed)
  // run, so the test always starts from a known-clean state.
  let dialog = await openModal(page);
  if ((await dialog.getByRole('button', { name: 'Remove photo' }).count()) > 0) {
    await dialog.getByRole('button', { name: 'Remove photo' }).click();
    await page.waitForURL(/\/profile\?avatar=removed/);
    dialog = await openModal(page);
  }

  // Set: upload a photo.
  await dialog.locator('input[name="file"]').setInputFiles({
    name: 'avatar.png',
    mimeType: 'image/png',
    buffer: PNG,
  });
  await dialog.getByRole('button', { name: 'Upload photo' }).click();
  // uiUploadAvatar redirects to /profile?avatar=1 on success.
  await page.waitForURL(/\/profile\?avatar=1/);

  // Verify the override is now present.
  dialog = await openModal(page);
  await expect(dialog.getByRole('button', { name: 'Remove photo' })).toBeVisible();

  // Cleanup: remove again, leaving the account in its original (no-photo) state.
  await dialog.getByRole('button', { name: 'Remove photo' }).click();
  await page.waitForURL(/\/profile\?avatar=removed/);

  dialog = await openModal(page);
  await expect(dialog.getByRole('button', { name: 'Remove photo' })).toHaveCount(0);
});
