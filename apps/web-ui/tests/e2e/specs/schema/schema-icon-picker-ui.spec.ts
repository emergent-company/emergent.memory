import { test, expect, type Page } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

// Regression coverage for the go-daisy IconPicker inside the schema object-type
// editor (/schema/object-types/Person/edit). The seeded `personal-memory` pack
// defines `Person`.
//
// The bundled go-daisy-icon-picker.js resolves its panel with
// `root.querySelector("[data-gd-icon-panel]")`. A build that omits that attribute
// (go-daisy < d98f93c) still opened the popover — the generic popover runtime
// does not need it — but `commit()` and `filter()` both short-circuited on the
// null panel, so picking an icon and searching silently no-oped. Every test
// below asserts on real DOM state (hidden input value, labels, ARIA, inline
// display, panel open flag), so that class of regression fails here instead of
// only in a browser.

const TYPE = 'Person';
const ROOT = '[data-gd-icon-picker]';
const HIDDEN = '#object-type-icon';
const TRIGGER = '#object-type-icon-trigger';
const PANEL = '#object-type-icon-panel';
const SEARCH = '#object-type-icon-search';
const LABEL = '#object-type-icon-label';
const RESET = '[data-gd-icon-reset]';
const EMPTY = '[data-gd-icon-empty]';
const DEFAULT_OPTION = `${PANEL} [data-gd-icon-option][data-gd-icon-default="1"]`;

function option(page: Page, value: string) {
  return page.locator(`${PANEL} [data-gd-icon-option][data-gd-icon-value="${value}"]`);
}

async function openEditor(page: Page): Promise<void> {
  await page.goto(`/schema/object-types/${TYPE}/edit`);
  await expectAppPage(page, new RegExp(`Edit ${TYPE}`));
  await expect(page.locator('#object-type-name')).toHaveValue(TYPE);
}

async function openPicker(page: Page): Promise<void> {
  await page.locator(TRIGGER).click();
  await expect(page.locator(PANEL)).toHaveAttribute('data-gd-popover-open', 'true');
  await expect(page.locator(TRIGGER)).toHaveAttribute('aria-expanded', 'true');
  // The runtime clears any stale search on open from a requestAnimationFrame;
  // let that settle before typing so the filter observes our value.
  await page.waitForTimeout(150);
}

async function pick(page: Page, value: string): Promise<void> {
  await openPicker(page);
  await option(page, value).click();
  await expect(page.locator(PANEL)).toHaveAttribute('data-gd-popover-open', 'false');
}

async function pickDefault(page: Page): Promise<void> {
  await openPicker(page);
  await page.locator(DEFAULT_OPTION).click();
  await expect(page.locator(PANEL)).toHaveAttribute('data-gd-popover-open', 'false');
}

async function optionDisplay(page: Page, value: string): Promise<string> {
  return option(page, value).evaluate((el) => (el as HTMLElement).style.display);
}

async function visibleOptionCount(page: Page): Promise<number> {
  return page
    .locator(`${PANEL} [data-gd-icon-option]`)
    .evaluateAll((els) => els.filter((el) => (el as HTMLElement).style.display !== 'none').length);
}

async function resetDisplay(page: Page): Promise<string> {
  return page.locator(`${PANEL} ${RESET}`).evaluate((el) => (el as HTMLElement).style.display);
}

/** Restore whatever icon value the editor had before the test (best effort). */
async function restoreIcon(page: Page, value: string): Promise<void> {
  const name = value.replace(/^lucide--/, '');
  if (name === '' || (await option(page, name).count()) === 0) {
    await pickDefault(page);
    return;
  }
  await pick(page, name);
}

/**
 * Submit the editor, confirming the blueprint-derived gate if it appears.
 * A project-authored type posts straight through.
 */
async function submitEditor(page: Page): Promise<void> {
  await page.getByRole('button', { name: 'Save changes' }).click();
  const modal = page.locator('#derive-warning-modal');
  try {
    await modal.waitFor({ state: 'visible', timeout: 4000 });
    await modal.getByRole('button', { name: 'Save anyway' }).click();
  } catch {
    // Project-authored type: the form posted directly, no gate.
  }
  await page.waitForURL(new RegExp(`/schema/object-types/${TYPE}\\?updated=1$`));
}

test('icon field renders as the go-daisy picker, not a free-text input', async ({ page }) => {
  await openEditor(page);
  await expect(page.locator(`${ROOT} ${HIDDEN}[data-gd-icon-input][name="icon"]`)).toHaveCount(1);
  await expect(page.locator(HIDDEN)).toHaveAttribute('type', 'hidden');
  // A free-text input named `icon` would shadow the hidden contract; it must
  // not exist (the pre-picker markup shipped one).
  await expect(page.locator('input[type="text"][name="icon"]')).toHaveCount(0);
});

test('picker opens its panel on trigger click', async ({ page }) => {
  await openEditor(page);
  await expect(page.locator(PANEL)).toBeHidden();
  await expect(page.locator(TRIGGER)).toHaveAttribute('aria-expanded', 'false');
  await openPicker(page);
  await expect(page.locator(PANEL)).toBeVisible();
  await expect(page.locator(TRIGGER)).toHaveAttribute('aria-expanded', 'true');
});

test('search filters options and shows the empty state', async ({ page }) => {
  await openEditor(page);
  await openPicker(page);
  const total = await page.locator(`${PANEL} [data-gd-icon-option]`).count();

  await page.locator(SEARCH).fill('user');
  await expect(option(page, 'user')).toBeVisible();
  expect(await optionDisplay(page, 'database'), 'non-matching option must be display:none').toBe('none');

  await page.locator(SEARCH).fill('zzz-not-a-real-icon');
  await expect(page.locator(EMPTY)).toBeVisible();
  expect(await visibleOptionCount(page), 'no option may survive a nonsense query').toBe(0);

  await page.locator(SEARCH).fill('');
  await expect(page.locator(EMPTY)).toBeHidden();
  expect(await optionDisplay(page, 'database'), 'clearing the search restores the option').toBe('');
  expect(await visibleOptionCount(page)).toBe(total);
});

test('committing a choice updates hidden input, label, selection and closes the panel', async ({ page }) => {
  await openEditor(page);

  // Normalise to the default tile first so the reset-reveal below is meaningful.
  await pickDefault(page);
  await expect(page.locator(HIDDEN)).toHaveValue('');
  await openPicker(page);
  expect(await resetDisplay(page), 'reset is hidden while the default is selected').toBe('none');

  await option(page, 'database').click();
  await expect(page.locator(HIDDEN)).toHaveValue('database');
  await expect(page.locator(LABEL)).toHaveText('Database');
  await expect(option(page, 'database')).toHaveAttribute('aria-selected', 'true');
  await expect(option(page, 'user')).toHaveAttribute('aria-selected', 'false');
  expect(await resetDisplay(page), 'reset is revealed once a real icon is committed').toBe('');
  await expect(page.locator(PANEL)).toHaveAttribute('data-gd-popover-open', 'false');
  await expect(page.locator(TRIGGER)).toHaveAttribute('aria-expanded', 'false');
});

test('reset restores the default value and label', async ({ page }) => {
  await openEditor(page);
  await pick(page, 'database');
  await expect(page.locator(HIDDEN)).toHaveValue('database');

  await openPicker(page);
  await page.locator(`${PANEL} ${RESET}`).click();
  await expect(page.locator(HIDDEN)).toHaveValue('');
  await expect(page.locator(LABEL)).toHaveText('Default icon');
  await expect(page.locator(PANEL)).toHaveAttribute('data-gd-popover-open', 'false');
});

test('picked icon and color persist across save and reload', async ({ page }) => {
  test.setTimeout(120_000);
  await openEditor(page);

  const original = {
    description: await page.locator('#object-type-description').inputValue(),
    icon: await page.locator(HIDDEN).inputValue(),
    color: await page.locator('#object-type-color').inputValue(),
  };
  const color = '#2563EB';
  let saved = false;

  try {
    await pick(page, 'database');
    await page.locator('#object-type-color').fill(color);
    await submitEditor(page);
    saved = true;

    // Full server round-trip: reopen the editor and reload.
    await page.goto(`/schema/object-types/${TYPE}/edit`);
    await expect(page.locator(HIDDEN)).toHaveValue('database');
    await expect(page.locator(LABEL)).toHaveText('Database');
    await expect(page.locator('#object-type-color')).toHaveValue(color);
    await page.reload();
    await expect(page.locator(HIDDEN)).toHaveValue('database');
    await expect(page.locator('#object-type-color')).toHaveValue(color);
  } finally {
    // Best-effort restore so the shared dev tenant is left as found.
    if (saved) {
      try {
        await openEditor(page);
        await restoreIcon(page, original.icon);
        await page.locator('#object-type-color').fill(original.color);
        await page.locator('#object-type-description').fill(original.description);
        await submitEditor(page);
        await page.goto(`/schema/object-types/${TYPE}/edit`);
        await expect(page.locator(HIDDEN)).toHaveValue(original.icon);
        await expect(page.locator('#object-type-color')).toHaveValue(original.color);
      } catch (err) {
        console.warn(`[schema-icon-picker-ui] cleanup failed: ${(err as Error).message}`);
      }
    }
  }
});
