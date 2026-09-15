import { test, expect } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

// Spotlight (⌘K) palette: read-only, no session/org mutation. Runs in the
// `chromium` project (parallel) against the shared bootstrap session.
//
// Viewports are managed per test because the trigger + palette render
// differently above/below the 768px (md) breakpoint:
//   desktop ≥768px → pill button ("Search…" + ⌘K) → centered max-w-lg dialog
//   mobile  <768px → icon-only 36x36 square → full-screen sheet + ✕ close
test.describe('Spotlight palette (⌘K search)', () => {
  test('desktop: pill trigger opens palette; Esc label and Escape key close it', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 720 });
    await page.goto('/orgs');
    await expectAppPage(page, /Organizations/);

    const trigger = page.locator('button[aria-label="Search"]');
    const palette = page.getByRole('dialog');
    const input = page.locator('#spotlight-input');
    const closeLabel = page.locator('.modal-box label[for="spotlight-toggle"]');

    // Desktop trigger shows the search pill text and the ⌘K hint.
    await expect(trigger).toBeVisible();
    await expect(trigger.locator('span.hidden.grow')).toHaveText('Search…');
    await expect(trigger.locator('kbd')).toBeVisible();

    // Clicking the trigger checks the hidden toggle and focuses the input.
    await trigger.click();
    await expect(palette).toBeVisible();
    await expect(input).toBeFocused();

    // The modal-header "Esc" label unchecks the toggle → palette closes.
    await closeLabel.click();
    await expect(palette).toBeHidden();

    // Reopen, then Escape must close it again (spotlight-escape regression).
    await trigger.click();
    await expect(palette).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(palette).toBeHidden();
  });

  test('mobile: icon-only trigger opens a full-screen sheet; ✕ closes it', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto('/orgs');
    await expectAppPage(page, /Organizations/);

    const trigger = page.locator('button[aria-label="Search"]');
    const palette = page.getByRole('dialog');
    const input = page.locator('#spotlight-input');

    // Below md the pill text and ⌘K kbd are display:none → icon-only square.
    await expect(trigger).toBeVisible();
    await expect(trigger.locator('span.hidden.grow')).toBeHidden();
    await expect(trigger.locator('kbd')).toBeHidden();

    const box = await trigger.boundingBox();
    expect(box).not.toBeNull();
    // Icon-only square button (btn-outline adds ~1px border each side).
    expect(Math.abs(box!.width - 36)).toBeLessThanOrEqual(4);
    expect(Math.abs(box!.height - 36)).toBeLessThanOrEqual(4);

    await trigger.click();
    await expect(palette).toBeVisible();
    await expect(input).toBeFocused();

    // Mobile scoped CSS (max-width:767px) fills the viewport edge to edge.
    // Scope to the dialog: other hidden modal-boxes exist on the page. The
    // full-height 100dvh rule is the mobile-only discriminator (the desktop
    // palette is content-sized and centered); headless dvh can report a few
    // percent under the set viewport, so assert proportionally.
    const sheetBox = await palette.locator('.modal-box').boundingBox();
    expect(sheetBox).not.toBeNull();
    expect(sheetBox!.height).toBeGreaterThanOrEqual(844 * 0.9);
    expect(sheetBox!.width).toBeGreaterThanOrEqual(390 * 0.9);

    // Close affordance becomes a square ✕: Esc text is font-size:0 and the
    // ✕ glyph is drawn through the label's ::after pseudo-element.
    const closeLabel = page.locator('.modal-box label[for="spotlight-toggle"]');
    const styles = await closeLabel.evaluate((el) => {
      const s = getComputedStyle(el);
      const after = getComputedStyle(el, '::after') as CSSStyleDeclaration & { content: string };
      return { fontSize: s.fontSize, afterContent: after.content };
    });
    expect(styles.fontSize).toBe('0px');
    expect(styles.afterContent).toContain('✕');

    await closeLabel.click();
    await expect(palette).toBeHidden();
  });

  test('desktop: typing filters the palette results', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 720 });
    await page.goto('/orgs');
    await expectAppPage(page, /Organizations/);

    await page.locator('button[aria-label="Search"]').click();
    const palette = page.getByRole('dialog');
    await expect(palette).toBeVisible();

    // filterPaletteItems hides non-matching rows; typing a substring of the
    // static "Agents" navigate row keeps at least that row visible.
    const results = page.locator('#spotlight-results');
    await page.locator('#spotlight-input').fill('ag');
    await expect(results).toBeVisible();
    const visibleRows = await results.locator('[data-label]:visible').count();
    expect(visibleRows, 'at least one filtered row must remain').toBeGreaterThanOrEqual(1);
  });
});
