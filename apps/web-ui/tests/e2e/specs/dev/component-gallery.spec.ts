import { test, expect } from '@playwright/test';

// The component gallery (/dev/components) is a dev-gated surface rendered by the
// gateway (gateway/devgallery.go + devgallery.templ). It only serves when
// MEMORY_COMPONENT_GALLERY=on, so it runs under the dedicated flag-on harness
// (`dev-gallery.config.ts`, which starts `run-e2e.sh`) rather than the
// session-mode suite — `playwright.config.ts` excludes `specs/dev` because the
// live gateway leaves the flag off and the route 404s. This spec therefore
// asserts the surface for real: catalog shell, a layer section, and that at
// least one lazy preview iframe renders non-empty isolated content.
// Run:  npx playwright test --config=dev-gallery.config.ts

test('renders the component gallery with an isolated preview', async ({ page }) => {
  const response = await page.goto('/dev/components');
  expect(response?.status(), 'gallery route should serve (MEMORY_COMPONENT_GALLERY=on)').toBe(200);

  // The catalog shell anchors the page.
  await expect(page.getByTestId('component-gallery')).toBeVisible();

  // At least one layer section exists — L0 (go-daisy primitives) is rendered first.
  await expect(page.getByTestId('layer-L0')).toBeVisible();

  // One preview iframe per listed slug, tagged component-preview-<slug>.
  const previews = page.locator('iframe[data-testid^="component-preview-"]');
  await expect(previews.first()).toBeAttached();
  expect(await previews.count()).toBeGreaterThanOrEqual(2);

  // Scroll the first preview into view so the lazy iframe loads, then assert it
  // renders isolated content inside its sandboxed frame: the preview canvas is
  // present and non-empty (a child element or non-whitespace text), not merely a
  // non-empty body.
  await previews.first().scrollIntoViewIfNeeded();
  const frame = page
    .frameLocator('iframe[data-testid^="component-preview-"]')
    .first();
  const canvas = frame.getByTestId('preview-canvas');
  await expect(canvas).toBeVisible();
  await expect(canvas).not.toBeEmpty();
});

test('opens an interactive overlay preview in a script-enabled sandbox', async ({ page }) => {
  const response = await page.goto('/dev/components');
  expect(response?.status(), 'gallery route should serve (MEMORY_COMPONENT_GALLERY=on)').toBe(200);

  // The overlay entry (SecretRevealModal) is the one scripted preview: its
  // iframe is tagged data-interactive and sandboxed with allow-scripts only.
  const interactive = page.locator(
    'iframe[data-interactive="true"][data-testid="component-preview-secret-reveal-modal"]'
  );
  await expect(interactive).toBeAttached();

  const sandbox = (await interactive.getAttribute('sandbox')) ?? '';
  expect(sandbox).toContain('allow-scripts');
  expect(sandbox).not.toContain('allow-same-origin');
  expect(sandbox).not.toContain('allow-forms');

  // Scroll the lazy frame into view so it loads, then assert the overlay is
  // actually open: app.js auto-opens dialog[data-dialog-autoopen].
  await interactive.scrollIntoViewIfNeeded();
  const frame = page.frameLocator('iframe[data-testid="component-preview-secret-reveal-modal"]');
  const dialog = frame.locator('dialog[data-dialog-autoopen]');
  await expect(dialog).toBeVisible();
  await expect(dialog).toHaveAttribute('open', '');

  // Opaque origin: the frame cannot read the session cookie even though scripts
  // run. An opaque origin throws on document.cookie (treated here as empty);
  // either way it must never surface a cookie value.
  const cookieRead = await frame.locator('body').evaluate(() => {
    try {
      return document.cookie;
    } catch {
      return '';
    }
  });
  expect(cookieRead).toBe('');
});

test('surfaces adoption gaps with per-file counts and findings', async ({ page }) => {
  const response = await page.goto('/dev/components');
  expect(response?.status(), 'gallery route should serve (MEMORY_COMPONENT_GALLERY=on)').toBe(200);

  // The adoption-gaps section renders at the top of the gallery.
  const gaps = page.getByTestId('adoption-gaps');
  await expect(gaps).toBeVisible();

  // The total count badge reports the detector's finding count. A fully-adopted
  // tree can legitimately reach zero, so assert the section's coherent states
  // rather than a non-zero count (which would fail once every gap is closed).
  const total = page.getByTestId('adoption-gap-total');
  await expect(total).toBeVisible();
  const totalText = (await total.textContent())?.trim() ?? '0';
  const totalCount = Number(totalText);
  expect(Number.isNaN(totalCount), 'total badge should be numeric').toBe(false);

  // Per-file blocks are tagged adoption-gap-<file>, one per offending template.
  const fileBlocks = page.locator('details[data-testid^="adoption-gap-"]');
  if (totalCount > 0) {
    expect(await fileBlocks.count()).toBeGreaterThan(0);

    // Expanding the first block reveals finding rows carrying a marker
    // (class:<name> or tag:<name>) and the file:line -> marker -> owners chain.
    await fileBlocks.first().click();
    const sectionText = (await gaps.textContent()) ?? '';
    expect(sectionText).toMatch(/(class:|tag:)/);
    expect(sectionText).toContain('→');
  } else {
    // Clean tree: the section renders its explicit empty state and no blocks.
    await expect(page.getByTestId('adoption-gaps-empty')).toBeVisible();
    expect(await fileBlocks.count()).toBe(0);
  }
});
