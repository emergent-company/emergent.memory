import { test, expect } from '@playwright/test';

// Per-PR mobile shell smoke (#564). These are the browser contracts the
// session-mode shell suite pins but which never ran per PR: pinch-zoom meta,
// no horizontal overflow at a phone width, a keyboard-operable sidebar trigger,
// and the chat session rail as a bounded overlay.
const PHONE = { width: 390, height: 844 };

async function noHorizontalOverflow(page: import('@playwright/test').Page) {
  const widths = await page.evaluate(() => ({
    scroll: document.documentElement.scrollWidth,
    client: document.documentElement.clientWidth,
  }));
  expect(widths.scroll).toBeLessThanOrEqual(widths.client + 1);
}

test.describe('Mobile shell', () => {
  test('viewport allows pinch zoom and keeps safe-area cover', async ({ page }) => {
    await page.goto('/agents');
    const content = await page
      .locator('meta[name="viewport"]')
      .getAttribute('content');
    expect(content).toBeTruthy();
    expect(content!).toContain('viewport-fit=cover');
    expect(content!).not.toMatch(/user-scalable\s*=\s*no/i);
    expect(content!).not.toMatch(/maximum-scale\s*=\s*1(\.0)?\b/i);
  });

  test('pages do not overflow horizontally at 390px', async ({ page }) => {
    await page.setViewportSize(PHONE);
    for (const path of ['/agents', '/objects', '/documents', '/inbox', '/board', '/data-sources']) {
      await page.goto(path);
      await noHorizontalOverflow(page);
    }
  });

  test('the sidebar trigger is a keyboard-operable button', async ({ page }) => {
    await page.setViewportSize(PHONE);
    await page.goto('/agents');

    const trigger = page.locator(
      '#_layout-topbar label[for="_layout-sidebar-toggle-trigger"]',
    );
    await expect(trigger).toHaveAttribute('role', 'button');
    await expect(trigger).toHaveAttribute('tabindex', '0');
    await expect(trigger).toHaveAttribute('aria-controls', '_layout-sidebar');

    const toggle = page.locator('#_layout-sidebar-toggle-trigger');
    await expect(toggle).not.toBeChecked();
    await trigger.focus();
    await page.keyboard.press('Enter');
    await expect(toggle).toBeChecked();
    await expect(trigger).toHaveAttribute('aria-expanded', 'true');
  });

  test('the chat session rail opens as a bounded overlay', async ({ page }) => {
    await page.setViewportSize(PHONE);
    await page.goto('/chat');

    const rail = page.locator('#chat-rail');
    const backdrop = page.locator('#chat-rail-backdrop');
    await expect(rail).toBeHidden();

    await page.getByRole('button', { name: 'Toggle session list' }).click();
    await expect(rail).toBeVisible();
    await expect(backdrop).toBeVisible();
    expect(await rail.evaluate((el) => getComputedStyle(el).position)).toBe('fixed');

    await backdrop.click({ position: { x: PHONE.width - 12, y: 200 } });
    await expect(rail).toBeHidden();
  });
});
