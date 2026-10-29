import { test, expect } from '@playwright/test';
import { expectAppPage, goto } from '../../helpers/page';
import { DOCUMENTS_UPLOAD_MODAL, DOCUMENTS_UPLOAD_OPEN } from '../../helpers/documents';

test.describe('Documents page', () => {
  test('renders with the upload control', async ({ page }) => {
    await goto(page, '/documents');
    await expectAppPage(page, /Documents/);

    // The form is one click away in a dialog rather than always open on the page.
    await expect(page.locator(DOCUMENTS_UPLOAD_MODAL)).toBeHidden();
    await page.locator(DOCUMENTS_UPLOAD_OPEN).click();
    await expect(page.locator(DOCUMENTS_UPLOAD_MODAL)).toBeVisible();
    await expect(page.locator('input[type="file"][name="file"]')).toBeVisible();
  });
});
