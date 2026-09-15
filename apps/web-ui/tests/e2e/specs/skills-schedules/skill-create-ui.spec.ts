import { test } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

test('creates a skill and opens its detail', async ({ page }) => {
  // Skill names are slugs: lowercase, digits, hyphen-separated.
  const slug = `e2e-skill-${Date.now()}`;
  let skillId = '';

  try {
    await page.goto('/skills/new');
    await page.locator('#skill-name').fill(slug);
    await page.locator('#skill-description').fill('E2E test skill');
    await page.locator('#skill-content').fill('Summarize the input concisely.');
    await page.getByRole('button', { name: 'Create skill' }).click();

    // uiCreateSkill redirects back to the list on success.
    await page.waitForURL(/\/skills\?created=1/);
    await page.getByRole('link', { name: `Open ${slug}` }).click();
    // The skill detail titles itself with the skill name (slug).
    await expectAppPage(page, new RegExp(slug));
    skillId = new URL(page.url()).pathname.split('/').pop()!;
  } finally {
    if (skillId) await page.request.post(`/skills/${skillId}/delete`).catch(() => {});
  }
});
