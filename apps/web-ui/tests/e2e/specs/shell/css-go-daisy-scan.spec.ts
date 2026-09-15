import { test, expect } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

// Regression guard for the go-daisy CSS scan (commit 9fa687f): a Tailwind
// `@source` misconfig dropped the go-daisy-derived rules from the compiled
// stylesheet, which (a) left the mobile hamburger icon blank (the lucide--menu
// mask-image rule was never compiled) and (b) made the hidden #spotlight-toggle
// checkbox visible again (its .modal-toggle layout CSS vanished).
//
// This spec is read-only and runs on the mobile viewport throughout. It checks
// the compiled stylesheet shipped the rules — the exact regression — rather
// than just that elements exist.
test.use({ viewport: { width: 390, height: 844 } });

test.describe('go-daisy compiled CSS on mobile (390x844)', () => {
  test('hamburger icon renders via a mask-image', async ({ page }) => {
    await page.goto('/orgs');
    await expectAppPage(page, /Organizations/);

    // #_topbar-menu-icon is display:inline-block below the lg (64rem)
    // breakpoint, so it must be visible on a 390px viewport.
    const icon = page.locator('#_topbar-menu-icon');
    await expect(icon).toBeVisible();

    // iconify glyphs are drawn with background-color clipped by a mask. The
    // mask comes from .iconify{mask-image:var(--svg)} + .lucide--menu{--svg:...}
    // — both compiled rules. 'none'/'empty' here means the icon renders blank.
    const mask = await icon.evaluate((el) => {
      const s = getComputedStyle(el) as unknown as Record<string, string>;
      return { maskImage: s.maskImage ?? '', webkitMaskImage: s.webkitMaskImage ?? '' };
    });
    const hasMask =
      (mask.maskImage !== '' && mask.maskImage !== 'none') ||
      (mask.webkitMaskImage !== '' && mask.webkitMaskImage !== 'none');
    expect(hasMask, `menu glyph must be drawn via a mask-image, got: ${JSON.stringify(mask)}`).toBe(true);

    const box = await icon.boundingBox();
    expect(box).not.toBeNull();
    expect(box!.width).toBeGreaterThan(0);
    expect(box!.height).toBeGreaterThan(0);
  });

  test('spotlight toggle checkbox stays hidden', async ({ page }) => {
    await page.goto('/orgs');
    await expectAppPage(page, /Organizations/);

    // .modal-toggle{appearance:none;opacity:0;width:0;height:0;position:fixed}
    // — if the rule is dropped the checkbox becomes a visible native input.
    const toggle = page.locator('#spotlight-toggle');
    await expect(toggle).toBeHidden();

    const metrics = await toggle.evaluate((el) => {
      const s = getComputedStyle(el);
      return {
        width: s.width,
        height: s.height,
        offsetWidth: el.offsetWidth,
        offsetHeight: el.offsetHeight,
      };
    });
    expect(metrics.width).toBe('0px');
    expect(metrics.height).toBe('0px');
    expect(metrics.offsetWidth).toBe(0);
    expect(metrics.offsetHeight).toBe(0);
  });
});
