import { test, expect } from '@playwright/test';
import { readBootstrap } from '../../helpers/bootstrap';

test('creates a project via the switcher and activates it', async ({ page }) => {
  const bootstrap = readBootstrap();
  const projectName = `E2E UI Project ${Date.now()}`;

  await page.goto('/agents');

  await page.getByTestId('project-switcher').click();
  // The "New project" footer row is a link that opens the create dialog
  // (auth_ui.templ projectSwitcher), not a <button>.
  await page.getByRole('link', { name: 'New project' }).click();

  const modal = page.locator('#new-project-modal');
  await expect(modal).toBeVisible();

  // The org select defaults to the current project's org (not the placeholder).
  await expect(page.locator('#new-project-org')).toHaveValue(bootstrap!.orgId);

  await page.locator('#new-project-name').fill(projectName);
  await page.getByRole('button', { name: 'Create', exact: true }).click();

  // uiCreateProject activates the new project and 303s back to /agents — the
  // URL is unchanged, so wait on the switcher content, not the URL.
  await expect(page.getByTestId('project-switcher')).toContainText(projectName, {
    timeout: 15000,
  });

  // Cleanup: projects have no delete endpoint; the bootstrap org teardown
  // cascades to this project, so nothing to do here.
});
