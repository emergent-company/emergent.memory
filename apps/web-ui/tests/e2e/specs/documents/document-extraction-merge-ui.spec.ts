import { test, expect, type Page } from '@playwright/test';
import { goto } from '../../helpers/page';
import { uploadDocumentViaForm } from '../../helpers/documents';
import { addProvider, setDefaultEmbeddingModel, setDefaultGenerativeModel } from '../../helpers/providers';

// Extraction review → merge preview modal (gateway/documents.templ,
// documents_ui.go, webui/static/js/app.js).
//
// The extraction history page's job detail (lazy HTMX partial) renders a
// "Merge into main" action for an unmerged staging branch. Clicking it lazily
// fetches the confirm-before-merge preview into a SIBLING container; the
// response is a <dialog data-dialog-autoopen="true"> that app.js must
// showModal(). Regression guard: the dialog was swapped in but never opened
// because the auto-open scan was scoped to the htmx swap source element (the
// button) instead of the document.
//
// Needs a real provider: extraction runs a model before a staging branch (and
// therefore the merge action) exists. Skips when E2E_SCENARIO_LLM_API_KEY is
// unset, same as document-extraction-ui.spec.ts. The gateway-free wiring gate
// for the same bug is specs/js/merge-preview-modal.spec.ts (runs in CI).
//
// Env (see tests/e2e/.env.e2e.example):
//   E2E_SCENARIO_LLM_PROVIDER         default 'openai' (the dev litellm slot)
//   E2E_SCENARIO_LLM_API_KEY          real key; unset → this spec skips
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

/** Open the freshly uploaded document's detail page and return its id. */
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

/** Trigger extraction on the open detail page and wait for the PRG redirect. */
async function triggerExtraction(page: Page, id: string): Promise<void> {
  await expect(page.getByRole('heading', { name: 'Extraction', exact: true })).toBeVisible();
  const extract = page.getByRole('button', { name: 'Extract data' });
  await expect(extract).toBeVisible();
  await extract.click();
  await page.waitForURL(new RegExp(`/documents/${escapeRe(id)}\\?extracted=1`));
}

/**
 * Poll the extraction history page until the job has produced objects and its
 * lazy detail exposes the merge action. Extraction is asynchronous: the staging
 * branch can exist while it is still empty, so the object count on the job row
 * (not the merge button alone) is the completion signal.
 */
async function waitForMergeAction(page: Page, id: string): Promise<void> {
  const deadline = Date.now() + 240_000;
  let lastRow = '';
  while (Date.now() < deadline) {
    await goto(page, `/documents/${id}/extractions`);
    const row = page.getByTestId('extraction-job-row').first();
    lastRow = (await row.innerText().catch(() => '')).replace(/\s+/g, ' ').trim();
    await page
      .getByTestId('extraction-job-detail')
      .first()
      .waitFor({ state: 'attached', timeout: 20_000 })
      .catch(() => undefined);

    const hasObjects = /[1-9]\d* object/.test(lastRow);
    const mergeVisible = await page
      .getByTestId('extraction-merge-button')
      .isVisible()
      .catch(() => false);
    if (hasObjects && mergeVisible) return;

    await page.waitForTimeout(5_000);
  }
  throw new Error(
    'extraction never produced a staging branch with a merge action; last job row: ' +
      (lastRow || '<none>'),
  );
}

/** Best-effort cleanup through the gateway's session-scoped delete route. */
async function deleteDocument(page: Page, id: string | undefined): Promise<void> {
  if (!id) return;
  await page.request.post(`/documents/${id}/delete`).catch(() => undefined);
}

test.describe('Extraction merge preview', () => {
  test('merge action opens the preview modal and merges after confirmation', async ({ page }) => {
    // Extraction runs a real model before the branch exists, and the review
    // page is polled — give the whole flow headroom.
    test.setTimeout(420_000);
    test.skip(
      !API_KEY,
      'E2E_SCENARIO_LLM_API_KEY is not set — extraction needs a working project ' +
        'provider before a staging branch (and the merge action) exists. Set it in ' +
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
    const name = `e2e-merge-${stamp}.md`;
    // Unique entity names per run: memory dedups identical objects, and a branch
    // whose objects all collide with main predicts zero clean merges.
    const body =
      `# Merge fixture ${stamp}\n\n` +
      `Zephyr${stamp} Quux is a fictional entity created for the merge preview e2e test. ` +
      `Zephyr${stamp} Quux works with Orbital${stamp} Labs in Reykjavik.`;
    let id: string | undefined;

    try {
      await uploadDocumentViaForm(page, name, body);
      id = await openUploadedDocument(page, name);
      await triggerExtraction(page, id);

      await waitForMergeAction(page, id);

      // The button lazily fetches the preview; the response must auto-open.
      const modal = page.getByTestId('merge-preview-modal');
      await expect(modal).toHaveCount(0);
      await page.getByTestId('extraction-merge-button').click();
      await expect(modal).toBeVisible();
      await expect(modal).toHaveJSProperty('open', true);
      await expect(modal.getByRole('heading', { name: 'Review merge' })).toBeVisible();

      // The predicted split is always rendered, whatever the counts are.
      for (const label of [
        'New objects',
        'Existing objects updated',
        'Conflicts',
        'Left for review',
        'Total',
      ]) {
        await expect(modal.getByText(label, { exact: true })).toBeVisible();
      }

      const confirm = modal.getByTestId('merge-preview-confirm');
      await expect(confirm).toBeVisible();
      if (!(await confirm.isEnabled())) {
        test.skip(
          true,
          'preview predicts no clean merge (every object conflicts) — cannot exercise confirm',
        );
        return;
      }

      await confirm.click();

      // uiDocumentMerge PRGs to the history page with ?merged=N (+ ?review=M on
      // a partial merge) and one toast carrying the truthful split.
      await page.waitForURL(new RegExp(`/documents/${escapeRe(id)}/extractions\\?merged=\\d+`));
      const merged = Number(new URL(page.url()).searchParams.get('merged'));
      expect(merged).toBeGreaterThan(0);
      await expect(
        page
          .locator('#toast-container [role="alert"]')
          .filter({ hasText: new RegExp(`Merged ${merged} object`) }),
      ).toBeVisible();
      await expect(modal).toHaveCount(0);

      // A full merge stamps merged_at ("Merged into main"); a partial one keeps
      // the branch open and reports what is left. Either is a truthful outcome.
      await page
        .getByTestId('extraction-job-detail')
        .first()
        .waitFor({ state: 'attached', timeout: 20_000 })
        .catch(() => undefined);
      await expect(page.getByText(/Merged into main|left for review/).first()).toBeVisible();
    } finally {
      await deleteDocument(page, id);
    }
  });
});
