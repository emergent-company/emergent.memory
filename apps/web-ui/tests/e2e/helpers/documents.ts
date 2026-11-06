import { Page, expect } from '@playwright/test';
import { expectAppPage, goto } from './page';

/**
 * Helpers for the documents browser (`/documents`): seeding documents through
 * the gateway's own upload API and the locators the page's controls expose.
 *
 * Seeded documents are named with a per-test token so a spec can narrow to its
 * own rows with the page's search box instead of depending on a clean project;
 * the shared e2e project keeps documents from every previous run.
 */

/** Stable locators for the browser's surfaces. */
export const DOCUMENTS_LIST = '#documents-list';
/** The count line above the table: documents the active filters match. */
export const DOCUMENTS_COUNT = '[data-testid="documents-count"]';
export const DOCUMENTS_TABLE = '[data-testid="documents-table"]';
export const DOCUMENTS_MOBILE_LIST = '#documents-mobile-list';
export const DOCUMENTS_STATS = '[data-testid="documents-stats"]';
export const DOCUMENTS_STATUS_FILTER = '[data-testid="documents-status-filter"]';
export const DOCUMENTS_LOAD_MORE = '[data-testid="documents-load-more"]';
export const DOCUMENTS_SEARCH_INPUT = 'input[name="q"]';
/** The header button that opens the upload dialog. */
export const DOCUMENTS_UPLOAD_OPEN = '[data-testid="documents-upload-open"]';
/** The upload dialog (closed until the header button is used). */
export const DOCUMENTS_UPLOAD_MODAL = '[data-testid="documents-upload-modal"]';

/** A filename fragment unique to one test run. */
export function uniqueDocumentToken(prefix = 'e2e-doc-browser'): string {
  return `${prefix}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
}

/**
 * Upload a markdown document through the gateway's documented upload API and
 * return its id. The body is unique per call so the server's content dedup does
 * not collapse two seeded documents into one.
 */
export async function uploadTestDocument(
  page: Page,
  filename: string,
  body = `# ${filename}\n\nSeeded by the documents browser spec.`,
): Promise<string> {
  const resp = await page.request.post('/api/documents', {
    multipart: {
      file: {
        name: filename,
        mimeType: 'text/markdown',
        buffer: Buffer.from(body),
      },
    },
  });
  expect(resp.ok(), `upload ${filename} failed: ${await resp.text()}`).toBeTruthy();
  const id = (await resp.json()).id as string;
  expect(id, `upload ${filename} returned no id`).toBeTruthy();
  return id;
}

/**
 * Upload a document through the page's own upload form: open the header's
 * upload dialog, choose the file, submit, and wait for the PRG redirect. Specs
 * that seed a later step (detail, delete, extraction) upload the way a user does
 * so a regression in the form breaks them instead of hiding behind the API.
 */
export async function uploadDocumentViaForm(
  page: Page,
  name: string,
  body = `# ${name}\n\nSeeded by the documents e2e suite.`,
): Promise<void> {
  await goto(page, '/documents');
  await expectAppPage(page, /Documents/);
  await page.locator(DOCUMENTS_UPLOAD_OPEN).click();
  const modal = page.locator(DOCUMENTS_UPLOAD_MODAL);
  await expect(modal).toBeVisible();
  await modal.locator('input[type="file"][name="file"]').setInputFiles({
    name,
    mimeType: 'text/markdown',
    buffer: Buffer.from(body),
  });
  await modal.getByRole('button', { name: 'Upload' }).click();
  await page.waitForURL(/\/documents\?uploaded=1/);
}

/** The value of one stat in the stats strip, e.g. "documents" or "failed". */
export async function statValue(page: Page, title: string): Promise<number> {
  const raw = await page
    .locator(`${DOCUMENTS_STATS} .stat`, { hasText: title })
    .locator('.stat-value')
    .first()
    .innerText();
  return Number(raw.replace(/[^0-9]/g, ''));
}

/** The table's document ids, in render order. */
export async function documentRowIDs(page: Page): Promise<string[]> {
  return page
    .locator(`${DOCUMENTS_LIST} tr[data-document-id]`)
    .evaluateAll((rows) => rows.map((r) => r.getAttribute('data-document-id') ?? ''));
}
