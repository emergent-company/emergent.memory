import { test, expect, type Page } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

// Regression coverage for project-owned schema override packs.
//
// Editing a blueprint-derived object type in /schema copies the edit into a
// project-owned schema pack literally named `project-schema-overrides` (v1.0.0)
// that shadows the blueprint's type. That pack is a schema pack, NOT a
// blueprint, so it must never render as an installed/available BLUEPRINT, nor
// leak into "Add schema". Before the fix, /schema additionally rendered a
// duplicate (shadowed) row for the same type name because the compiled-types
// merge keeps both the losing blueprint entry and the winning override entry.
//
// The seeded `personal-memory` bundled pack defines `Person`, so that is the
// type under test. The spec self-restores the shared dev tenant afterwards.

const TYPE = 'Person';
const BLUEPRINT = 'personal-memory'; // seeded bundled pack that defines Person
const OVERRIDE_PACK = 'project-schema-overrides';
const OBJECT_TYPE_LINK = `a[href="/schema/object-types/${TYPE}"]`;

/** Open the object-type editor and wait for it to prefill (mirrors the edit spec). */
async function openEditor(page: Page): Promise<void> {
  await page.goto(`/schema/object-types/${TYPE}/edit`);
  await expectAppPage(page, new RegExp(`Edit ${TYPE}`));
  await expect(page.locator('#object-type-name')).toHaveValue(TYPE);
  await expect(page.locator('#object-type-description')).toBeVisible();
}

/**
 * Submit the editor, confirming the blueprint-derived gate if it appears.
 * Returns whether the gate was armed — i.e. the type was still blueprint-derived
 * and the save copied the edit into the override pack (design D1).
 */
async function submitEditor(page: Page): Promise<boolean> {
  await page.getByRole('button', { name: 'Save changes' }).click();
  const modal = page.locator('#derive-warning-modal');
  let gated = false;
  try {
    await modal.waitFor({ state: 'visible', timeout: 4000 });
    gated = true;
    await modal.getByRole('button', { name: 'Save anyway' }).click();
  } catch {
    // Project-authored / already-overridden type: the form posted directly.
  }
  await page.waitForURL(new RegExp(`/schema/object-types/${TYPE}\\?updated=1$`));
  return gated;
}

/**
 * Scope a locator to a titled card section. `cardList` renders
 * `<section><h2>{title}</h2>…</section>` (gateway/ui.templ:711).
 */
function section(page: Page, title: string) {
  return page
    .locator('section')
    .filter({ has: page.getByRole('heading', { name: title, exact: true }) });
}

test('project schema override pack does not duplicate, shadow-install, or leak', async ({ page }) => {
  test.setTimeout(120_000);

  const description = `e2e override shadow ${Date.now()}`;

  await openEditor(page);
  const originalDescription = await page.locator('#object-type-description').inputValue();

  try {
    // 1. Ensure the override exists: submit a unique description, handling both
    //    editor states (blueprint-derived gate vs. direct write).
    await page.locator('#object-type-description').fill(description);
    await submitEditor(page);

    // 2. /schema renders exactly one object-type row for the name — the
    //    shadowed (losing) duplicate must be gone.
    await page.goto('/schema');
    await expectAppPage(page, /Schema/);
    await expect(page.locator(OBJECT_TYPE_LINK)).toHaveCount(1);

    // 3. The effective owner is the project override pack (correct by design:
    //    the override was assigned after the blueprint, so it wins the merge).
    await expect(page.getByText(OVERRIDE_PACK).first()).toBeVisible();

    // 4. /schema/add must not offer the internal override pack as installable.
    await page.goto('/schema/add');
    await expectAppPage(page, /Add schema/);
    await expect(page.locator('body')).not.toContainText(OVERRIDE_PACK);

    // 5. /blueprints must not list it in any section (Installed/Drafts/Available/
    //    Upgrades), and the blueprint that owns Person stays installed.
    await page.goto('/blueprints');
    await expectAppPage(page, /Blueprints/);
    await expect(page.locator('body')).not.toContainText(OVERRIDE_PACK);
    await expect(
      section(page, 'Installed').getByRole('link', { name: BLUEPRINT, exact: true }),
    ).toBeVisible();
  } finally {
    // Best-effort restore so the shared dev tenant is left as found.
    try {
      await openEditor(page);
      await page.locator('#object-type-description').fill(originalDescription);
      await submitEditor(page);
    } catch (err) {
      console.warn(`[schema-overrides-shadowing-ui] cleanup failed: ${(err as Error).message}`);
    }
  }
});
