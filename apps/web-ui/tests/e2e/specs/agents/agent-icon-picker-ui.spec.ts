import { test, expect, type Page } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';
import { createAgentViaModal } from '../../helpers/agents';

// Regression coverage for the go-daisy IconPicker + ColorPicker inside the
// per-agent Appearance card (/agents/<id>/settings). Agents reuse the exact
// picker components object types use (see schema-icon-picker-ui.spec.ts), with
// the agent default falling back to `lucide--bot`. Every test below asserts on
// real DOM state (hidden input value, labels, ARIA, the rendered list/detail
// markup), so a regression in the picker wiring or the icon+color rendering
// fails here instead of only in a browser.
//
// The General settings form auto-saves (it has no Save button), so the picker
// interactions must reach the server through the debounced auto-save — a colour
// chosen from a preset swatch sets the text field programmatically and must
// still persist (the "colour assigned but not saved" report).

const ROOT = '[data-gd-icon-picker]';
const HIDDEN = '#agent-settings-icon';
const TRIGGER = '#agent-settings-icon-trigger';
const PANEL = '#agent-settings-icon-panel';
const SEARCH = '#agent-settings-icon-search';
const LABEL = '#agent-settings-icon-label';
const RESET = '[data-gd-icon-reset]';
const COLOR = '#agent-settings-color';

function option(page: Page, value: string) {
  return page.locator(`${PANEL} [data-gd-icon-option][data-gd-icon-value="${value}"]`);
}

async function openPicker(page: Page): Promise<void> {
  await page.locator(TRIGGER).click();
  await expect(page.locator(PANEL)).toHaveAttribute('data-gd-popover-open', 'true');
  await expect(page.locator(TRIGGER)).toHaveAttribute('aria-expanded', 'true');
  // The runtime clears any stale search on open from a requestAnimationFrame;
  // assert the search input is empty (a deterministic precondition) rather than
  // sleeping for the frame to settle.
  await expect(page.locator(SEARCH)).toHaveValue('');
}

async function pick(page: Page, value: string): Promise<void> {
  await openPicker(page);
  await option(page, value).click();
  await expect(page.locator(PANEL)).toHaveAttribute('data-gd-popover-open', 'false');
}

async function openSettings(page: Page, id: string): Promise<void> {
  await page.goto(`/agents/${id}/settings`);
  await expectAppPage(page, /Settings/);
  await expect(page.locator(HIDDEN)).toHaveCount(1);
}

// The General settings form auto-saves (no Save button); poll the API until the
// debounced POST has persisted the expected appearance.
async function expectAppearance(page: Page, id: string, want: Record<string, string>): Promise<void> {
  await expect
    .poll(
      async () => {
        const resp = await page.request.get(`/api/agents/${id}`);
        if (resp.status() !== 200) return null;
        return ((await resp.json()).uiConfig ?? {}) as Record<string, string>;
      },
      { timeout: 15_000 },
    )
    .toEqual(want);
}

test('creates an agent, picks an icon and colour, and renders them on list + detail', async ({ page }) => {
  test.setTimeout(120_000);
  const name = `E2E Appearance ${Date.now()}`;
  const color = '#2563EB';
  let id = '';

  try {
    id = await createAgentViaModal(page, name);

    // --- editor: pickers render and carry the agent default ---
    await openSettings(page, id);
    await expect(page.locator(`${ROOT} ${HIDDEN}[data-gd-icon-input][name="icon"]`)).toHaveCount(1);
    await expect(page.locator(HIDDEN)).toHaveAttribute('type', 'hidden');
    // A free-text input named `icon` would shadow the hidden contract.
    await expect(page.locator('input[type="text"][name="icon"]')).toHaveCount(0);
    await expect(page.locator(TRIGGER)).toHaveAttribute('aria-expanded', 'false');

    // The default agent tile is the bot glyph; the picker's "Default icon"
    // affordance carries the agent default class.
    await expect(page.locator(ROOT)).toHaveAttribute('data-gd-icon-picker-default-class', 'lucide--bot');

    // --- pick a real icon + colour (auto-saves) ---
    await pick(page, 'database');
    await expect(page.locator(HIDDEN)).toHaveValue('database');
    await expect(page.locator(LABEL)).toHaveText('Database');
    await page.locator(COLOR).fill(color);
    await expectAppearance(page, id, { icon: 'database', color });

    // --- settings page round-trips the choice ---
    await page.goto(`/agents/${id}/settings`);
    await expect(page.locator(HIDDEN)).toHaveValue('database');
    await expect(page.locator(LABEL)).toHaveText('Database');
    await expect(page.locator(COLOR)).toHaveValue(color);

    // --- the agents list renders the picked icon + colour on the row tile ---
    await page.goto('/agents');
    const row = page.locator('#main-content a[href^="/agents/"]').filter({ hasText: name }).first();
    await expect(row).toBeVisible();
    const tile = row.locator('div[style*="color:#2563EB"]').first();
    await expect(tile).toBeVisible();
    await expect(tile.locator('.iconify.lucide--database')).toHaveCount(1);

    // --- the agent detail summary card renders the same accent ---
    await page.goto(`/agents/${id}`);
    await expectAppPage(page, new RegExp(name));
    const detailTile = page.locator('#main-content div[style*="color:#2563EB"]').first();
    await expect(detailTile).toBeVisible();
    await expect(detailTile.locator('.iconify.lucide--database')).toHaveCount(1);
  } finally {
    if (id) await page.request.delete(`/api/agents/${id}`).catch(() => {});
  }
});

test('picking a colour preset on the settings form auto-saves and persists it', async ({ page }) => {
  test.setTimeout(120_000);
  const name = `E2E Appearance Preset ${Date.now()}`;
  const presetColor = '#10B981';
  let id = '';

  try {
    id = await createAgentViaModal(page, name);
    await openSettings(page, id);

    // A preset swatch sets the colour text field programmatically (no native
    // input/change); the auto-save wiring must still persist it.
    const preset = page
      .locator(`form[data-agent-autosave] [data-gd-color-preset][data-gd-color-value="${presetColor}"]`)
      .first();
    await expect(preset).toBeVisible();
    await preset.click();
    await expect(page.locator(COLOR)).toHaveValue(presetColor);
    await expectAppearance(page, id, { color: presetColor });

    // Survives a full reload.
    await page.goto(`/agents/${id}/settings`);
    await expect(page.locator(COLOR)).toHaveValue(presetColor);
  } finally {
    if (id) await page.request.delete(`/api/agents/${id}`).catch(() => {});
  }
});

test('resetting the icon returns the agent to the default bot tile', async ({ page }) => {
  test.setTimeout(120_000);
  const name = `E2E Appearance Reset ${Date.now()}`;
  let id = '';

  try {
    id = await createAgentViaModal(page, name);
    await openSettings(page, id);

    await pick(page, 'database');
    await expect(page.locator(HIDDEN)).toHaveValue('database');

    await openPicker(page);
    await expect(page.locator(`${PANEL} ${RESET}`)).toBeVisible();
    await page.locator(`${PANEL} ${RESET}`).click();
    await expect(page.locator(HIDDEN)).toHaveValue('');
    await expect(page.locator(LABEL)).toHaveText('Default icon');
  } finally {
    if (id) await page.request.delete(`/api/agents/${id}`).catch(() => {});
  }
});
