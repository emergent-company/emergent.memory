import { test, expect, type Page } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

// Document delete (gateway/documents.templ + gateway/ui.go):
//
//   POST /documents          upload the file → PRG /documents?uploaded=1
//   GET  /documents/:id      document detail (chunks + extraction + header delete)
//   POST /documents/:id/delete (uiDeleteDocument) → PRG /documents?deleted=1
//
// Covers the delete route end to end: upload through the real form, open the
// detail page, delete from the detail header (the hx-confirm-gated boosted
// form), and assert the resulting state — the document is gone from the list
// and its detail/chunk view is no longer reachable. The document is deleted in
// cleanup too, pass or fail, and every assertion targets the one uniquely-named
// document this run created (never list counts/ordering), because the mutation
// project shares the bootstrap tenant.

const escapeRe = (s: string): string => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

// uploadDocument submits the /documents form and waits for the success redirect.
async function uploadDocument(page: Page, name: string, body: string): Promise<void> {
  await page.goto('/documents');
  await expectAppPage(page, /Documents/);
  await page.setInputFiles('input[type="file"][name="file"]', {
    name,
    mimeType: 'text/markdown',
    buffer: Buffer.from(body),
  });
  await page.getByRole('button', { name: 'Upload' }).click();
  await page.waitForURL(/\/documents\?uploaded=1/);
}

// openUploadedDocument clicks the list row for the freshly uploaded document
// and returns the id parsed from its href.
async function openUploadedDocument(page: Page, name: string): Promise<string> {
  const row = page.getByRole('link', { name: new RegExp(escapeRe(name)) });
  await expect(row).toBeVisible();
  const href = await row.getAttribute('href');
  if (!href) throw new Error(`upload row for "${name}" has no href`);
  const id = href.split('/').pop();
  if (!id) throw new Error(`could not parse document id from href "${href}"`);
  await row.click();
  await page.waitForURL(new RegExp(`/documents/${escapeRe(id)}$`));
  return id;
}

// deleteDocument is the best-effort cleanup through the gateway's
// session-scoped delete route; never throws so it is safe from `finally`.
async function deleteDocument(page: Page, id: string | null): Promise<void> {
  if (!id) return;
  await page.request.post(`/documents/${id}/delete`).catch(() => undefined);
}

test.describe('Document delete', () => {
  test('deletes an uploaded document from its detail page and removes it from the list', async ({
    page,
  }) => {
    const stamp = Date.now();
    const name = `e2e-doc-delete-${stamp}.md`;
    // Unique body per run so memory's content dedup never collapses the upload.
    const body = `# E2E delete ${stamp}\n\nDelete-lifecycle fixture — uniquely identifiable body.`;
    let id: string | null = null;

    try {
      // 1. UPLOAD through the real form.
      await uploadDocument(page, name, body);

      // 2. OPEN the new document's detail page (its title is the file name).
      id = await openUploadedDocument(page, name);
      await expectAppPage(page, new RegExp(escapeRe(name)));

      // 3. DELETE from the detail header. The form is hx-confirm-gated (a
      // native confirm() under hx-boost), so accept the dialog; the POST then
      // lands an HX-Redirect to the list.
      page.once('dialog', (dialog) => dialog.accept());
      await page.getByRole('button', { name: `Delete ${name}` }).click();
      await page.waitForURL(/\/documents\?deleted=1/, { timeout: 30_000 });
      await expectAppPage(page, /Documents/);

      // 4. RESULTING STATE: gone from the list …
      await expect(page.getByRole('link', { name: new RegExp(escapeRe(name)) })).toHaveCount(0);

      // … and its detail / chunk view is no longer reachable — the load fails
      // into the page's error state rather than rendering the document.
      await page.goto(`/documents/${id}`);
      await expect(
        page.getByRole('heading', { name: 'Document unavailable', exact: true }),
      ).toBeVisible();
      await expect(page.getByRole('heading', { name: 'Chunks', exact: true })).toHaveCount(0);

      id = null; // deleted: nothing for cleanup to do.
    } finally {
      await deleteDocument(page, id);
    }
  });
});
