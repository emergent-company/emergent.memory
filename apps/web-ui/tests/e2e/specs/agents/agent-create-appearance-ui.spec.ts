import { test, expect, type Page } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

// Creation-time appearance coverage for the "New agent" modal (/agents). Agents
// can be created with any combination of the Appearance options — an icon from
// the supported-Lucide picker, a colour typed into the text field or chosen from
// a preset swatch, both, or neither — and every combination must be persisted
// to the agent's `uiConfig` and rendered on the list, detail, and settings
// surfaces. The bug report behind this spec: a colour assigned while adding an
// agent was not preserved; these cases lock the create path (the settings-side
// auto-save path is covered by agent-icon-picker-ui.spec.ts).
//
// Each case creates its agent through the real UI and deletes it via the API.

const COLOR = '#16A34A';
const PRESET_COLOR = '#10B981';

async function openCreateModal(page: Page, name: string): Promise<void> {
  await page.goto('/agents');
  await page.getByRole('button', { name: 'New agent' }).first().click();
  await expect(page.locator('#agent-name')).toBeVisible();
  await page.locator('#agent-name').fill(name);
}

async function pickIcon(page: Page, value: string): Promise<void> {
  await page.locator('#agent-icon-trigger').click();
  await expect(page.locator('#agent-icon-panel')).toHaveAttribute('data-gd-popover-open', 'true');
  await page.locator(`#agent-icon-panel [data-gd-icon-option][data-gd-icon-value="${value}"]`).click();
  await expect(page.locator('#agent-icon')).toHaveValue(value);
}

async function submit(page: Page, name: string): Promise<string> {
  await page.getByRole('button', { name: 'Create agent' }).click();
  const rowLink = page.locator('#main-content a[href^="/agents/"]').filter({ hasText: name }).first();
  await expect(rowLink).toBeVisible();
  const href = await rowLink.getAttribute('href');
  if (!href) throw new Error(`no href for row "${name}"`);
  return new URL(href, 'http://localhost').pathname.split('/').pop()!;
}

async function uiConfig(page: Page, id: string): Promise<Record<string, string>> {
  const resp = await page.request.get(`/api/agents/${id}`);
  expect(resp.status(), `GET /api/agents/${id}`).toBe(200);
  const body = await resp.json();
  return (body.uiConfig ?? {}) as Record<string, string>;
}

test('creating an agent with a typed colour preserves and renders it', async ({ page }) => {
  test.setTimeout(120_000);
  const name = `E2E Create Colour ${Date.now()}`;
  let id = '';

  try {
    await openCreateModal(page, name);
    await page.locator('#agent-color').fill(COLOR);
    id = await submit(page, name);

    expect(await uiConfig(page, id)).toEqual({ color: COLOR });

    // The list row tile carries the accent, and the detail summary card too.
    const row = page.locator('#main-content a[href^="/agents/"]').filter({ hasText: name }).first();
    await expect(row.locator(`div[style*="color:${COLOR}"]`).first()).toBeVisible();

    await page.goto(`/agents/${id}`);
    await expectAppPage(page, new RegExp(name));
    await expect(page.locator(`#main-content div[style*="color:${COLOR}"]`).first()).toBeVisible();

    // Settings reflects it as well.
    await page.goto(`/agents/${id}/settings`);
    await expect(page.locator('#agent-settings-color')).toHaveValue(COLOR);
  } finally {
    if (id) await page.request.delete(`/api/agents/${id}`).catch(() => {});
  }
});

test('creating an agent with an icon picker choice and a preset colour preserves both', async ({ page }) => {
  test.setTimeout(120_000);
  const name = `E2E Create Icon Preset ${Date.now()}`;
  let id = '';

  try {
    await openCreateModal(page, name);
    await pickIcon(page, 'database');
    const preset = page.locator(
      `#agent-form-modal [data-gd-color-preset][data-gd-color-value="${PRESET_COLOR}"]`,
    ).first();
    await expect(preset).toBeVisible();
    await preset.click();
    await expect(page.locator('#agent-color')).toHaveValue(PRESET_COLOR);

    id = await submit(page, name);

    expect(await uiConfig(page, id)).toEqual({ icon: 'database', color: PRESET_COLOR });

    const row = page.locator('#main-content a[href^="/agents/"]').filter({ hasText: name }).first();
    const tile = row.locator(`div[style*="color:${PRESET_COLOR}"]`).first();
    await expect(tile).toBeVisible();
    await expect(tile.locator('.iconify.lucide--database')).toHaveCount(1);

    await page.goto(`/agents/${id}/settings`);
    await expect(page.locator('#agent-settings-icon')).toHaveValue('database');
    await expect(page.locator('#agent-settings-color')).toHaveValue(PRESET_COLOR);
  } finally {
    if (id) await page.request.delete(`/api/agents/${id}`).catch(() => {});
  }
});

test('creating an agent with no appearance stores an empty uiConfig and renders the neutral tile', async ({ page }) => {
  test.setTimeout(120_000);
  const name = `E2E Create Plain ${Date.now()}`;
  let id = '';

  try {
    await openCreateModal(page, name);
    id = await submit(page, name);

    expect(await uiConfig(page, id)).toEqual({});

    const row = page.locator('#main-content a[href^="/agents/"]').filter({ hasText: name }).first();
    await expect(row.locator('.iconify.lucide--bot').first()).toBeVisible();
    // No accent colour is painted on the row tile.
    expect(await row.locator('div[style*="color:#"]').count()).toBe(0);
  } finally {
    if (id) await page.request.delete(`/api/agents/${id}`).catch(() => {});
  }
});
