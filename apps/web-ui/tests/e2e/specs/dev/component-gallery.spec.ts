import { test, expect } from '@playwright/test';

// The component gallery (/dev/components) is a dev-gated surface rendered by the
// gateway (gateway/devgallery.go + devgallery.templ). The external session-mode
// gateway this suite targets leaves MEMORY_COMPONENT_GALLERY off by default, so
// the route returns 404 and this spec skips fast rather than failing. When the
// flag is on, it asserts the catalog shell, a layer section, and that at least
// one lazy preview iframe renders non-empty isolated content.

test('renders the component gallery with an isolated preview', async ({ page }) => {
  const response = await page.goto('/dev/components');
  test.skip(
    !response || response.status() === 404,
    'component gallery is disabled (MEMORY_COMPONENT_GALLERY off on the target gateway)',
  );

  // The catalog shell anchors the page.
  await expect(page.getByTestId('component-gallery')).toBeVisible();

  // At least one layer section exists — L0 (go-daisy primitives) is rendered first.
  await expect(page.getByTestId('layer-L0')).toBeVisible();

  // One preview iframe per listed slug, tagged component-preview-<slug>.
  const previews = page.locator('iframe[data-testid^="component-preview-"]');
  await expect(previews.first()).toBeAttached();
  expect(await previews.count()).toBeGreaterThan(0);

  // Scroll the first preview into view so the lazy iframe loads, then assert it
  // renders non-empty isolated content inside its sandboxed frame: the preview
  // canvas is present and the frame body is non-empty (an element or text).
  await previews.first().scrollIntoViewIfNeeded();
  const frame = page
    .frameLocator('iframe[data-testid^="component-preview-"]')
    .first();
  await expect(frame.getByTestId('preview-canvas')).toBeVisible();
  await expect(frame.locator('body')).not.toBeEmpty();
});
