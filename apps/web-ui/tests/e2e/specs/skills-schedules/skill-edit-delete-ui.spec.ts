import { test, expect } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

// Skill edit + delete: gateway/skills.templ + ui.go (uiUpdateSkill / uiDeleteSkill).
// Drives the full mutation lifecycle on a scratch skill — create (form) → edit
// description/content (`POST /skills/:id/update`, `?updated=1`) → delete
// (`POST /skills/:id/delete`, `?deleted=1`) — and asserts resulting state
// (persisted values across a reload, then the skill gone from the list). The
// delete is the final step, so the spec self-cleans; the `finally` revokes any
// leftover only when the flow aborted early.

test('skill edit persists and delete removes it from the list', async ({ page }) => {
  // Skill names are slugs: lowercase, digits, hyphen-separated.
  const slug = `e2e-skill-${Date.now()}`;
  let skillId = '';

  try {
    // --- create ---
    await page.goto('/skills/new');
    await page.locator('#skill-name').fill(slug);
    await page.locator('#skill-description').fill('E2E test skill');
    await page.locator('#skill-content').fill('Summarize the input concisely.');
    await page.getByRole('button', { name: 'Create skill' }).click();

    await page.waitForURL(/\/skills\?created=1/);
    await page.getByRole('link', { name: `Open ${slug}` }).click();
    await expectAppPage(page, new RegExp(slug));
    skillId = new URL(page.url()).pathname.split('/').pop()!;

    // --- edit (description + content are the only editable fields) ---
    await page.locator('#edit-description').fill('Updated description');
    await page.locator('#edit-content').fill('Updated content.');
    await page.getByRole('button', { name: 'Save changes' }).click();
    await page.waitForURL(/\/skills\/[^/]+\?updated=1/);

    // Persisted across a full reload.
    await page.reload();
    await expect(page.locator('#edit-description')).toHaveValue('Updated description');
    await expect(page.locator('#edit-content')).toHaveValue('Updated content.');

    // --- delete ---
    await page.getByRole('button', { name: `Delete ${slug}` }).click();
    await page
      .locator('#skill-delete-modal')
      .getByRole('button', { name: 'Delete skill' })
      .click();
    await page.waitForURL(/\/skills\?deleted=1/);

    // Gone from the list (no "Open <slug>" row remains).
    await expect(page.getByRole('link', { name: `Open ${slug}` })).toHaveCount(0);
    skillId = ''; // already deleted — skip cleanup
  } finally {
    if (skillId) {
      await page.request.post(`/skills/${skillId}/delete`).catch(() => {});
    }
  }
});
