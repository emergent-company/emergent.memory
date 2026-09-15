import { Page, expect } from '@playwright/test';

export type ProviderSaveResult = 'saved' | 'error';

// The failed-save error copy surfaced by the re-rendered form's auto-opened
// modal ("Couldn't save provider" heading, body starting "could not save
// provider: …") — matches the assertion language of the mutation specs.
const PROVIDER_SAVE_ERROR = /couldn't save provider|could not save provider/i;

/**
 * Open the add-provider form (/settings/providers/new) and wait for its
 * controls to render: the provider select, the API-key field, and the save
 * button.
 */
export async function openProviderForm(page: Page): Promise<void> {
  await page.goto('/settings/providers/new');
  await expect(page.locator('#provider-type')).toBeVisible();
  await expect(page.locator('input[name="api_key"]')).toBeVisible();
  await expect(page.locator('#provider-save-btn')).toBeVisible();
}

/** Select the provider type and fill the API key on the open add form.
 *  When baseUrl is given, also fill the OpenAI-compatible base URL field
 *  (rendered only for the openai provider). */
export async function fillProviderForm(page: Page, provider: string, apiKey: string, baseUrl?: string): Promise<void> {
  await page.locator('#provider-type').selectOption(provider);
  await page.locator('input[name="api_key"]').fill(apiKey);
  if (baseUrl !== undefined) {
    const url = page.locator('input[name="base_url"]');
    await url.waitFor({ state: 'visible' });
    await url.fill(baseUrl);
  }
}

/**
 * Click the save button and wait for the POST's outcome:
 *  - success: PRG-redirect to /settings/providers?updated=1 → 'saved';
 *  - failure: the form re-renders at HTTP 200 with an auto-opened
 *    "Couldn't save provider" modal → 'error'.
 * Whichever fires first wins; if neither does within the timeouts, throw.
 */
export async function saveProviderForm(page: Page): Promise<ProviderSaveResult> {
  await page.locator('#provider-save-btn').click();

  const saved = page
    .waitForURL(/\/settings\/providers\?updated=1/, { timeout: 10_000 })
    .then(() => 'saved' as ProviderSaveResult)
    .catch(() => null);
  const failed = page
    .getByText(PROVIDER_SAVE_ERROR)
    .first()
    .waitFor({ state: 'visible', timeout: 10_000 })
    .then(() => 'error' as ProviderSaveResult)
    .catch(() => null);

  const outcome = await Promise.race([saved, failed]);
  if (outcome) return outcome;
  throw new Error(
    'saveProviderForm: neither the /settings/providers?updated=1 redirect nor the ' +
      `save-error modal appeared after saving; landed on ${page.url()}`,
  );
}

/** openProviderForm + fillProviderForm + saveProviderForm. */
export async function addProvider(page: Page, provider: string, apiKey: string, baseUrl?: string): Promise<ProviderSaveResult> {
  await openProviderForm(page);
  await fillProviderForm(page, provider, apiKey, baseUrl);
  return saveProviderForm(page);
}

/**
 * Pin the project's default embedding model via the Settings → Providers
 * "Default models" panel. That panel exists once the active project has ≥1
 * configured provider; model is the prefixed "provider/model" catalog value
 * (e.g. "openai/gemini-embedding-001"). Selecting an option fires htmx
 * (change, 400ms delay) which POSTs the whole form to
 * /settings/providers/model-config (swap none, no redirect). Confirmation:
 * wait for the POST's 200, then reload and assert the select keeps the value
 * (the server re-renders it selected from the stored project model config).
 */
export async function setDefaultEmbeddingModel(page: Page, model: string): Promise<void> {
  await page.goto('/settings/providers');
  const emb = page.locator('select[name="embedding_model"]');
  await expect(emb).toBeVisible();

  // The option list is built from the provider's cached catalog — give the
  // server a moment if the catalog was only just synced by the provider save.
  await expect(emb.locator(`option[value="${model}"]`)).toHaveCount(1, { timeout: 10_000 });

  const posted = page.waitForResponse(
    (r) => r.url().includes('/settings/providers/model-config') && r.request().method() === 'POST',
    { timeout: 10_000 },
  );
  await emb.selectOption(model);
  await posted;

  // No redirect/swap — assert persistence by reloading and checking the
  // server-rendered selected value.
  await page.reload();
  await expect(emb).toBeVisible();
  await expect(emb).toHaveValue(model);
}
