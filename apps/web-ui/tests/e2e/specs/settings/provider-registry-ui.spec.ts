import { test, expect } from '@playwright/test';
import { goto } from '../../helpers/page';

// Registry-driven provider picker (Settings → Providers → add): the vendor
// `<select id="provider-type">` is rendered from the server's provider registry
// (28 vendors), not a hardcoded list. This spec asserts the picker's contract —
// option presence, per-vendor description metadata, registry ordering, and the
// per-vendor field reveal (base URL + vendor-specific extra fields). It is
// read-only (no save/mutation), so it needs no cleanup and stays always-green.
//
// The vendor list (Order ascending) is asserted by value, not by exact count, so
// adding a future vendor never breaks this spec.

const VENDORS = [
  'generic',
  'anthropic',
  'azure-openai',
  'openai',
  'deepseek',
  'google',
  'google-vertex',
  'openrouter',
  'litellm',
  'cohere',
];

test('provider picker is registry-driven', async ({ page }) => {
  await goto(page, '/settings/providers/new');

  const select = page.locator('#provider-type');
  await expect(select).toBeVisible();

  // Every expected vendor is present as an option with a value, a non-empty
  // text label, and non-empty registry description metadata. (No exact total —
  // future vendors must not fail this.)
  for (const vendor of VENDORS) {
    const option = select.locator(`option[value="${vendor}"]`);
    await expect(option).toHaveCount(1);
    await expect(option).not.toHaveText(/^\s*$/);
    await expect(option).toHaveAttribute('data-description', /^(?!\s*$).+/);
  }

  // Representative subset: read the description back for anthropic/openai.
  await expect(select.locator('option[value="anthropic"]')).toHaveAttribute(
    'data-description',
    /Claude/,
  );
  await expect(select.locator('option[value="openai"]')).toHaveAttribute(
    'data-description',
    /OpenAI API/,
  );

  // Registry order: generic is first; anthropic sorts before google.
  const order = await select.locator('option').evaluateAll((opts) =>
    opts.map((o) => o.getAttribute('value')),
  );
  expect(order[0]).toBe('generic');
  expect(order.indexOf('anthropic')).toBeGreaterThan(-1);
  expect(order.indexOf('google')).toBeGreaterThan(-1);
  expect(order.indexOf('anthropic')).toBeLessThan(order.indexOf('google'));
});

test('vendor selection reveals the right fields', async ({ page }) => {
  await goto(page, '/settings/providers/new');

  const select = page.locator('#provider-type');
  await expect(select).toBeVisible();

  const description = page.locator('#provider-description');
  const baseUrlField = page.locator('#base-url-field');
  const baseUrlInput = page.locator('input[name="base_url"]');
  const azureExtra = page.locator('[data-provider-extra="azure-openai"]');
  const apiVersion = page.locator('[data-testid="provider-extra-api_version"]');

  // azure-openai: user-supplied endpoint ({resource} placeholder) + an
  // api_version extra field.
  await select.selectOption('azure-openai');
  await expect(description).toContainText(/Azure OpenAI/);
  await expect(baseUrlField).toBeVisible();
  await expect(baseUrlInput).toHaveValue(/\{resource\}/);
  await expect(azureExtra).toBeVisible();
  await expect(apiVersion).toBeEnabled();

  // deepseek: concrete default endpoint (no editable base URL) and no azure
  // extra block.
  await select.selectOption('deepseek');
  await expect(baseUrlField).toBeHidden();
  await expect(azureExtra).toBeHidden();
});
