import { test, expect, type Page } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';
import { addProvider, setDefaultEmbeddingModel } from '../../helpers/providers';

// Document upload + extraction UI (gateway/documents.templ, documents.go, ui.go):
//
//   1. /documents      → the upload form posts multipart to POST /documents
//                        (uiUploadDocument) → PRG /documents?uploaded=1
//                        (or ?duplicate=1 / ?err=…).
//   2. /documents/:id  → the detail page (uiDocument) with the Extraction card.
//   3. "Extract data"  → POST /documents/:id/extract (uiTriggerExtraction) →
//                        PRG /documents/:id?extracted=1 (or ?err=…).
//
// The trigger only *enqueues* a job — memory's POST /api/admin/extraction-jobs
// inserts a pending row and does not validate a provider — so the PRG round-trip
// needs no LLM. Producing actual extraction RESULTS does need a working project
// provider (the worker resolves it at run time), so the results test is
// env-gated on the scenario LLM key and configures the provider through the
// settings UI first.
//
// Env (see tests/e2e/.env.e2e.example), shared with the scenario suite:
//   E2E_SCENARIO_LLM_PROVIDER         default 'openai' (the dev litellm slot)
//   E2E_SCENARIO_LLM_API_KEY          real key; unset → the results test skips
//   E2E_SCENARIO_LLM_BASE_URL         default 'http://litellm:4000/v1'
//   E2E_SCENARIO_LLM_EMBEDDING_MODEL  default 'openai/gemini-embedding-001'

const PROVIDER = process.env.E2E_SCENARIO_LLM_PROVIDER || 'openai';
const API_KEY = process.env.E2E_SCENARIO_LLM_API_KEY || '';
const BASE_URL = process.env.E2E_SCENARIO_LLM_BASE_URL || 'http://litellm:4000/v1';
const EMBEDDING_MODEL =
  process.env.E2E_SCENARIO_LLM_EMBEDDING_MODEL || 'openai/gemini-embedding-001';

const escapeRe = (s: string): string => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

/** Upload one document through the /documents form and wait for the success redirect. */
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

/**
 * Find the list row for the freshly uploaded document, click into its detail
 * page, and return the document id parsed from the URL.
 */
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

/** Trigger extraction on the open detail page and wait for the PRG success redirect. */
async function triggerExtraction(page: Page, id: string): Promise<void> {
  await expect(page.getByRole('heading', { name: 'Extraction', exact: true })).toBeVisible();
  const extract = page.getByRole('button', { name: 'Extract data' });
  await expect(extract).toBeVisible();
  await extract.click();
  await page.waitForURL(new RegExp(`/documents/${escapeRe(id)}\\?extracted=1`));
}

/** Best-effort cleanup through the gateway's session-scoped delete route. */
async function deleteDocument(page: Page, id: string | undefined): Promise<void> {
  if (!id) return;
  await page.request.post(`/documents/${id}/delete`).catch(() => undefined);
}

test.describe('Document upload + extraction', () => {
  test('uploads a document and triggers extraction from the detail page', async ({ page }) => {
    const stamp = Date.now();
    const name = `e2e-doc-${stamp}.md`;
    // Unique body per run so memory's content dedup never collapses the upload.
    const body = `# E2E ${stamp}\n\nExtraction smoke fixture — uniquely identifiable body.`;
    let id: string | undefined;

    try {
      await uploadDocument(page, name, body);
      await expect(
        page.locator('#toast-container [role="alert"]').filter({ hasText: 'Document uploaded.' }),
      ).toBeVisible();

      id = await openUploadedDocument(page, name);
      await triggerExtraction(page, id);

      await expect(
        page.locator('#toast-container [role="alert"]').filter({ hasText: 'Extraction triggered.' }),
      ).toBeVisible();
    } finally {
      await deleteDocument(page, id);
    }
  });

  test('extracts entities with a working project provider', async ({ page }) => {
    // Extraction runs a real model plus a poll for results; give it headroom.
    test.setTimeout(300_000);
    test.skip(
      !API_KEY,
      'E2E_SCENARIO_LLM_API_KEY is not set — extraction runs a real model through the ' +
        "project's provider, so results can only be asserted with a working provider. " +
        'Set it in tests/e2e/.env.e2e (defaults target the dev litellm: openai).',
    );

    // Configure the provider through the settings UI. The save is live-validated
    // by memory, so a rejection is an environment problem, not a regression.
    const saved = await addProvider(
      page,
      PROVIDER,
      API_KEY,
      PROVIDER === 'openai' ? BASE_URL : undefined,
    );
    if (saved !== 'saved') {
      test.skip(true, 'provider save rejected by the memory backend (catalog unsynced or invalid key)');
      return;
    }
    await setDefaultEmbeddingModel(page, EMBEDDING_MODEL);

    const stamp = Date.now();
    const name = `e2e-extract-${stamp}.md`;
    const body =
      `# Extraction fixture ${stamp}\n\n` +
      'Ada Lovelace worked with Charles Babbage on the Analytical Engine in London.';
    let id: string | undefined;

    try {
      await uploadDocument(page, name, body);
      id = await openUploadedDocument(page, name);
      await triggerExtraction(page, id);

      // The "Extraction results" section renders only once a COMPLETED
      // extraction summary exists — the deterministic proof the provider ran.
      // The detail page does not poll, so reload until the summary lands.
      await expect
        .poll(
          async () => {
            await page.reload();
            return page
              .getByRole('heading', { name: 'Extraction results' })
              .isVisible()
              .catch(() => false);
          },
          { timeout: 240_000, intervals: [3_000, 5_000, 10_000] },
        )
        .toBe(true);

      await expect(page.getByRole('heading', { name: 'Extraction results' })).toBeVisible();
      await expect(page.getByText(/\d+ object(s)?/).first()).toBeVisible();
    } finally {
      await deleteDocument(page, id);
    }
  });
});
