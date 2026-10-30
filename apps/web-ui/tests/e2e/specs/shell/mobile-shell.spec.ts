import { test, expect } from '@playwright/test';
import { expectAppPage, goto } from '../../helpers/page';

// Mobile app-shell regression suite (web-app-shell capability).
//
// Guards the shell-level fixes: zoomable viewport, no horizontal overflow at a
// phone width, a topbar whose controls stay inside the viewport, and the chat
// session rail behaving as a bounded overlay instead of an in-flow block.
//
// Runs in the `chromium` project against the shared bootstrap session.
const PHONE = { width: 390, height: 844 };

async function documentOverflow(page: import('@playwright/test').Page) {
  return page.evaluate(() => ({
    scrollWidth: document.documentElement.scrollWidth,
    clientWidth: document.documentElement.clientWidth,
  }));
}

test.describe('Mobile app shell', () => {
  test('viewport allows pinch zoom and keeps safe-area cover', async ({ page }) => {
    await page.setViewportSize(PHONE);
    await goto(page, '/orgs');
    await expectAppPage(page, /Organizations/);

    const content = await page
      .locator('meta[name="viewport"]')
      .getAttribute('content');
    expect(content).toBeTruthy();
    expect(content!).toContain('viewport-fit=cover');
    expect(content!).not.toMatch(/user-scalable\s*=\s*no/i);
    expect(content!).not.toMatch(/maximum-scale\s*=\s*1(\.0)?\b/i);
  });

  test('topbar keeps the account menu inside a 390px viewport', async ({ page }) => {
    await page.setViewportSize(PHONE);
    await goto(page, '/agents');
    await expectAppPage(page, /Agents/);

    const trigger = page.getByTestId('account-menu-trigger');
    await expect(trigger).toBeVisible();

    const box = await trigger.boundingBox();
    expect(box).not.toBeNull();
    expect(box!.x).toBeGreaterThanOrEqual(0);
    expect(box!.x + box!.width).toBeLessThanOrEqual(PHONE.width + 1);

    // Shell-level overflow guard: the topbar must never widen the document.
    const { scrollWidth, clientWidth } = await documentOverflow(page);
    expect(scrollWidth).toBeLessThanOrEqual(clientWidth + 1);
  });

  test('primary topbar controls meet the 44px touch target', async ({ page }) => {
    await page.setViewportSize(PHONE);
    await goto(page, '/agents');
    await expectAppPage(page, /Agents/);

    // The root font size is 15px, so a rem-based "11" would be 41.25px. Assert
    // the literal 44px contract on every visible topbar control.
    const controls = page.locator('#_layout-topbar .btn:visible');
    const count = await controls.count();
    expect(count).toBeGreaterThan(0);
    for (let i = 0; i < count; i++) {
      const box = await controls.nth(i).boundingBox();
      expect(box).not.toBeNull();
      expect(box!.height, `topbar control ${i} height`).toBeGreaterThanOrEqual(43.5);
    }
  });

  test('sidebar trigger is a keyboard-operable button', async ({ page }) => {
    await page.setViewportSize(PHONE);
    await goto(page, '/agents');
    await expectAppPage(page, /Agents/);

    const trigger = page.locator(
      '#_layout-topbar label[for="_layout-sidebar-toggle-trigger"]',
    );
    // Decoration is runtime JS emitted by appSidebar (before the topbar), so
    // this also guards the DOM-order bug where it ran too early and no-op'd.
    await expect(trigger).toHaveAttribute('role', 'button');
    await expect(trigger).toHaveAttribute('tabindex', '0');
    await expect(trigger).toHaveAttribute('aria-controls', '_layout-sidebar');

    const toggle = page.locator('#_layout-sidebar-toggle-trigger');
    await expect(toggle).not.toBeChecked();
    await trigger.focus();
    await page.keyboard.press('Enter');
    await expect(toggle).toBeChecked();
    await expect(trigger).toHaveAttribute('aria-expanded', 'true');

    await page.keyboard.press('Enter');
    await expect(toggle).not.toBeChecked();
    await expect(trigger).toHaveAttribute('aria-expanded', 'false');
  });

  test('list and workspace pages do not overflow horizontally', async ({ page }) => {
    const routes = ['/agents', '/objects', '/documents', '/inbox', '/board'];

    for (const width of [320, 390]) {
      await page.setViewportSize({ width, height: 800 });
      for (const route of routes) {
        await goto(page, route);
        await expect(page.locator('#main-content')).toBeVisible();
        await expect(page).not.toHaveURL(/\/auth\/login/);
        const { scrollWidth, clientWidth } = await documentOverflow(page);
        expect(
          scrollWidth,
          `${route} overflows horizontally at ${width}px`,
        ).toBeLessThanOrEqual(clientWidth + 1);
      }
    }
  });

  test('chat session rail opens as a bounded overlay', async ({ page }) => {
    await page.setViewportSize(PHONE);
    await goto(page, '/chat');
    await expect(page.locator('#main-content')).toBeVisible();
    await expect(page).not.toHaveURL(/\/auth\/login/);

    const rail = page.locator('#chat-rail');
    const backdrop = page.locator('#chat-rail-backdrop');
    await expect(rail).toBeHidden();
    await expect(backdrop).toBeHidden();

    await page.getByRole('button', { name: 'Toggle session list' }).click();
    await expect(rail).toBeVisible();
    await expect(backdrop).toBeVisible();

    const position = await rail.evaluate(
      (el) => getComputedStyle(el).position,
    );
    expect(position).toBe('fixed');

    // The rail only covers part of the viewport; click the exposed strip on
    // the right rather than the backdrop's centre (which sits under the rail).
    await backdrop.click({ position: { x: PHONE.width - 12, y: 200 } });
    await expect(rail).toBeHidden();
    await expect(backdrop).toBeHidden();
  });
});
