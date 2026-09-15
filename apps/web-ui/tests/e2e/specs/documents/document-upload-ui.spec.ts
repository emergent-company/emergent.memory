import { test, expect } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

test.describe('Document upload', () => {

test('uploads a document via the form', async ({ page }) => {
  const name = `e2e-doc-${Date.now()}`;
  await page.goto('/documents');
  await page.setInputFiles('input[name="file"]', {
    name: `${name}.md`,
    mimeType: 'text/markdown',
    buffer: Buffer.from('# E2E\n\nHello from the e2e suite.'),
  });
  await page.getByRole('button', { name: 'Upload' }).click();
  // uiUploadDocument redirects back to the list on success.
  await page.waitForURL(/\/documents\?uploaded=1/);
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

  await page.goto(`/documents/${id}`);
  await expectAppPage(page, new RegExp(name));
});
});
