import { test, expect } from '@playwright/test';

// Browser smoke for the documents surface (#564): the upload form lives in a
// dialog now (#2287). Runs in the gateway-smoke suite, i.e. on the `dev -> main`
// promotion PR and on pushes to `main` — not on a feature PR. The mutations
// suite caught that regression only by reading the specs; this drives it
// against the hermetic dev-mode harness and fails if the opener, dialog, file
// input, or submit button drift apart again.
test.describe('Documents upload dialog', () => {
  test('the header opener owns a real dialog with the file input and submit', async ({
    page,
  }) => {
    await page.goto('/documents');

    const opener = page.getByTestId('documents-upload-open');
    await expect(opener).toBeVisible();

    const dialog = page.locator('#document-upload-modal');
    await expect(dialog).toBeHidden();

    await opener.click();
    await expect(dialog).toBeVisible();
    await expect(dialog).toHaveAttribute('open', '');

    // The form controls must live inside the dialog, not behind the opener.
    await expect(dialog.getByTestId('documents-upload-input')).toBeVisible();
    await expect(dialog.getByTestId('documents-upload-submit')).toBeVisible();
    // A real modal, not a CSS-revealed panel behind the opener.
    expect(
      await dialog.evaluate((el) => (el as HTMLDialogElement).matches(':modal')),
    ).toBe(true);

    await dialog.locator('[data-dialog-close="document-upload-modal"]').click();
    await expect(dialog).toBeHidden();
    await expect(opener).toBeVisible();
  });
});
