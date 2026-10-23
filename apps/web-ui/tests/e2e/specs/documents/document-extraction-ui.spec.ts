import { test, expect, type Page } from '@playwright/test';
import { expectAppPage, goto } from '../../helpers/page';
import { readBootstrap, createProject } from '../../helpers/bootstrap';
import { addProvider, setDefaultEmbeddingModel, setDefaultGenerativeModel } from '../../helpers/providers';

// Document upload + extraction UI (gateway/documents.templ, documents.go, ui.go):
//
//   1. /documents      → the upload form posts multipart to POST /documents
//                        (uiUploadDocument) → PRG /documents?uploaded=1
//                        (or ?duplicate=1 / ?err=…).
//   2. /documents/:id  → the detail page (uiDocument) with the Extraction card.
//   3. "Extract data"  → POST /documents/:id/extract (uiTriggerExtraction) →
//                        PRG /documents/:id?extracted=1 (or ?err=…).
//
// The trigger is gated on a resolvable generative model (gateway/documents.go
// extractionModelMissing → memory's effective model config): with no generative
// model the "Extract data" button is disabled and the server refuses the
// trigger (409 / ?err=). The two extraction tests therefore configure a project
// provider and pin the project default generative model
// (setDefaultGenerativeModel) — the provider-credential auto-pick is not
// deterministic with a litellm base_url — plus the embedding model. Enqueueing
// still needs no LLM *call* — memory's POST /api/admin/extraction-jobs only
// inserts a pending row — but it does need a configured generative model for
// the gateway to offer it. Producing actual extraction RESULTS needs the
// provider to run a real model, so the results test drives that through the
// settings UI and polls for the summary. A third test covers the blocked state
// unconditionally (no LLM key) on a fresh provider-less project.
//
// Env (see tests/e2e/.env.e2e.example), shared with the scenario suite:
//   E2E_SCENARIO_LLM_PROVIDER         default 'openai' (the dev litellm slot)
//   E2E_SCENARIO_LLM_API_KEY          real key; unset → the two extraction tests skip
//   E2E_SCENARIO_LLM_BASE_URL         default 'http://litellm:4000/v1'
//   E2E_SCENARIO_LLM_MODEL            default 'openai/deepseek-v4-flash'
//   E2E_SCENARIO_LLM_EMBEDDING_MODEL  default 'openai/gemini-embedding-001'

const PROVIDER = process.env.E2E_SCENARIO_LLM_PROVIDER || 'openai';
const API_KEY = process.env.E2E_SCENARIO_LLM_API_KEY || '';
const BASE_URL = process.env.E2E_SCENARIO_LLM_BASE_URL || 'http://litellm:4000/v1';
const GENERATIVE_MODEL = process.env.E2E_SCENARIO_LLM_MODEL || 'openai/deepseek-v4-flash';
const EMBEDDING_MODEL =
  process.env.E2E_SCENARIO_LLM_EMBEDDING_MODEL || 'openai/gemini-embedding-001';

const escapeRe = (s: string): string => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

/** Upload one document through the /documents form and wait for the success redirect. */
async function uploadDocument(page: Page, name: string, body: string): Promise<void> {
  await goto(page, '/documents');
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
    // The trigger is gated on a configured generative model, so the smoke test
    // configures a provider first (same shape as the results test). Give the
    // provider save + model-config round-trips headroom.
    test.setTimeout(300_000);
    test.skip(
      !API_KEY,
      'E2E_SCENARIO_LLM_API_KEY is not set — the trigger is gated on a configured ' +
        'generative model, which needs a project provider. Set it in ' +
        'tests/e2e/.env.e2e (defaults target the dev litellm: openai).',
    );

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
    await setDefaultGenerativeModel(page, GENERATIVE_MODEL);
    await setDefaultEmbeddingModel(page, EMBEDDING_MODEL);

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
    await setDefaultGenerativeModel(page, GENERATIVE_MODEL);
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

  test('renders the extraction trace sub-page with summary strip + trace panel', async ({ page }) => {
    // The sub-page renders for any job status (pending shows "waiting for the
    // first spans"), but reaching it needs a triggered extraction, which is
    // gated on a configured generative model. No real LLM call is required —
    // enqueueing only inserts a pending row — so the skip idiom matches the
    // other extraction tests.
    test.setTimeout(300_000);
    test.skip(
      !API_KEY,
      'E2E_SCENARIO_LLM_API_KEY is not set — the trigger is gated on a configured ' +
        'generative model, which needs a project provider. Set it in ' +
        'tests/e2e/.env.e2e (defaults target the dev litellm: openai).',
    );

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
    await setDefaultGenerativeModel(page, GENERATIVE_MODEL);
    await setDefaultEmbeddingModel(page, EMBEDDING_MODEL);

    const stamp = Date.now();
    const name = `e2e-trace-${stamp}.md`;
    const body = `# Trace ${stamp}\n\nSub-page fixture — uniquely identifiable body.`;
    let id: string | undefined;

    try {
      await uploadDocument(page, name, body);
      id = await openUploadedDocument(page, name);
      await triggerExtraction(page, id);

      // The history row links to the dedicated run sub-page via "View trace →".
      await goto(page, `/documents/${id}/extractions`);
      const viewTrace = page.getByTestId('extraction-view-trace').first();
      await expect(viewTrace).toBeVisible();
      await viewTrace.click();
      await page.waitForURL(new RegExp(`/documents/${escapeRe(id)}/extractions/[^/]+$`));

      // The sub-page carries the run header, the back link, the summary strip,
      // and the (polling) trace panel.
      await expect(page.getByTestId('extraction-run-header')).toBeVisible();
      await expect(page.getByRole('link', { name: 'All extractions' })).toBeVisible();
      await expect(page.getByTestId('extraction-summary-strip')).toBeVisible();
      await expect(page.getByTestId('extraction-trace')).toBeVisible();
    } finally {
      await deleteDocument(page, id);
    }
  });

  test('disables extraction when the project has no generative model', async ({ page }) => {
    // Unconditional (no LLM key): a fresh, provider-less project must render the
    // blocked extraction card — disabled button, guidance copy, providers link.
    const bootstrap = readBootstrap();
    test.skip(!bootstrap, 'setup project must run first');
    const orgId = bootstrap!.orgId;

    const base = `E2E Blocked Extraction ${Date.now()}`;
    const name = `e2e-blocked-${Date.now()}.md`;
    const body = `# Blocked ${base}\n\nNo provider configured — extraction must be gated.`;
    let projectId = '';

    try {
      projectId = await createProject(page, orgId, base);

      await uploadDocument(page, name, body);
      await openUploadedDocument(page, name);

      const extract = page.getByRole('button', { name: 'Extract data' });
      await expect(extract).toBeDisabled();
      await expect(page.getByText('Extraction needs a generative model.')).toBeVisible();
      await expect(page.getByRole('link', { name: 'Configure a model' })).toHaveAttribute(
        'href',
        /\/settings\/providers$/,
      );
    } finally {
      // Restore the bootstrap project, then delete the scratch project.
      if (bootstrap?.projectId) {
        await page.request.post(`/api/projects/${bootstrap.projectId}/activate`).catch(() => {});
      }
      if (projectId && orgId) {
        await page.request.post(`/projects/delete?projectId=${projectId}&orgId=${orgId}`).catch(() => {});
      }
    }
  });
});
