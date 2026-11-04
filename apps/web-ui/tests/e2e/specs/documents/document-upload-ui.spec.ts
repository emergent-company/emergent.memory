import { test, expect } from '@playwright/test';
import { expectAppPage, goto } from '../../helpers/page';
import { uploadDocumentViaForm } from '../../helpers/documents';

test.describe('Document upload', () => {

test('uploads a document via the form', async ({ page }) => {
  const name = `e2e-doc-${Date.now()}`;
  // The form lives in the header's upload dialog; the helper opens it, chooses
  // the file and submits, then waits for uiUploadDocument's success redirect.
  await uploadDocumentViaForm(page, `${name}.md`, '# E2E\n\nHello from the e2e suite.');
  await expect(page.locator('#toast-container [role="alert"]').filter({ hasText: 'Document uploaded.' })).toBeVisible();
});

test('opens a document detail', async ({ page }) => {
  const name = `e2e-doc-${Date.now()}`;
  const resp = await page.request.post('/api/documents', {
    multipart: {
      file: {
        name: `${name}.md`,
        mimeType: 'text/markdown',
        buffer: Buffer.from('# E2E\n\nHello from the e2e suite.'),
      },
    },
  });
  expect(resp.ok(), `upload failed: ${await resp.text()}`).toBeTruthy();
  const id = (await resp.json()).id as string;

  await goto(page, `/documents/${id}`);
  await expectAppPage(page, new RegExp(name));
});
});
