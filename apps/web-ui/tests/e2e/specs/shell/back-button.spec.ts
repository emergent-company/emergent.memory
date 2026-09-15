import { test, expect } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

// Regression guard for the htmx v4 browser back/forward bug.
//
// On popstate htmx issues a GET with `HX-History-Restore-Request: true` and
// outerSync-swaps the response's [hx-history-elt] into #main-content. If the
// server answers with a bare fragment, or the shell carries no hx-history-elt,
// htmx falls back to swapping document.body — wiping the stylesheet <link> and
// the scripts that live in <body>. The result is an unstyled, non-interactive
// page with no sidebar.
//
// The sequence must reach an htmx-managed history entry: htmx only handles
// popstate for entries it pushed itself, so do at least two boosted sidebar
// navigations after the initial full page load, then go Back.
test.describe('Shell history restore (browser Back/Forward)', () => {
  test('Back restores the full shell: CSS, sidebar and JS intact', async ({ page }) => {
    // Full page load → a native (non-htmx) history entry.
    await page.goto('/agents');
    await expectAppPage(page, /Agents/);

    // Two boosted sidebar navigations → htmx pushState entries.
    await page.getByRole('link', { name: 'Chat', exact: true }).click();
    await expect(page).toHaveURL(/\/chat$/);
    await page.getByRole('link', { name: 'Agents', exact: true }).click();
    await expect(page).toHaveURL(/\/agents$/);
    await expect(page.getByTestId('page-agents')).toBeVisible();

    // The next popstate must fetch the full shell; capture that response.
    const restore = page.waitForResponse(
      (res) => res.request().headers()['hx-history-restore-request'] === 'true',
      { timeout: 15_000 },
    );

    await page.evaluate(() => history.back());
    await expect(page).toHaveURL(/\/chat$/);
    await expect(page.getByTestId('page-chat')).toBeVisible();

    // Server contract: history restore returns the full document, not a fragment.
    const restoreRes = await restore;
    expect(restoreRes.status()).toBe(200);
    expect(await restoreRes.text(), 'history restore must return the full shell').toContain('<html');

    // CSS survived: the stylesheet <link> is still in the DOM and loaded.
    const css = await page.evaluate(() => {
      const link = document.querySelector('link[rel="stylesheet"][href*="app.css"]');
      let rules = 0;
      for (const sheet of Array.from(document.styleSheets)) {
        try {
          if (sheet.href && sheet.href.includes('app.css')) rules += sheet.cssRules.length;
        } catch {
          /* cross-origin sheet */
        }
      }
      return { linkPresent: link !== null, rules, bg: getComputedStyle(document.body).backgroundColor };
    });
    expect(css.linkPresent, 'app.css <link> must survive history restore').toBe(true);
    expect(css.rules, 'app.css must still have compiled rules').toBeGreaterThan(0);
    expect(css.bg).not.toBe('rgba(0, 0, 0, 0)');
    expect(css.bg).not.toBe('rgb(255, 255, 255)');

    // JS survived: htmx is still live (sidebar/spotlight depend on it).
    expect(
      await page.evaluate(() => typeof (window as unknown as { htmx?: unknown }).htmx),
    ).toBe('object');

    // Shell chrome is present, visible, and marks the htmx history target.
    await expect(page.getByRole('link', { name: 'Agents', exact: true })).toBeVisible();
    await expect(page.locator('#main-content')).toHaveAttribute('hx-history-elt', 'true');

    // Forward navigation back to the later entry is also a full-shell restore.
    const forwardRestore = page.waitForResponse(
      (res) => res.request().headers()['hx-history-restore-request'] === 'true',
      { timeout: 15_000 },
    );
    await page.evaluate(() => history.forward());
    await expect(page).toHaveURL(/\/agents$/);
    const forwardRes = await forwardRestore;
    expect(await forwardRes.text(), 'forward restore must return the full shell').toContain('<html');
    await expect(page.getByTestId('page-agents')).toBeVisible();
    await expect(page.locator('#main-content')).toHaveAttribute('hx-history-elt', 'true');
  });
});
