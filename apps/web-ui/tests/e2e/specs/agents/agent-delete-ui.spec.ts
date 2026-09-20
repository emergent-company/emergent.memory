import { test, expect } from '@playwright/test';
import { createAgentViaModal } from '../../helpers/agents';

// Agent delete: the agents-list row action opens a confirm dialog (ui.templ
// `deleteConfirmDialog`, id `delete-confirm-modal`); confirming drives app.js
// `confirmDelete`, which issues `DELETE /api/agents/:id` and reloads the list on
// a 204. The spec asserts the resulting state — the row is gone — rather than
// the transient "Agent deleted" toast. It creates one scratch agent and deletes
// it again in `finally` only when the UI flow aborted early.

test('deleting an agent from the list removes it', async ({ page }) => {
  const name = `E2E Delete ${Date.now()}`;
  const id = await createAgentViaModal(page, name);
  let deleted = false;

  try {
    // The list row action is labelled "Delete <name>"; clicking it opens the
    // confirm dialog pre-filled with the agent's name.
    await page.getByRole('button', { name: `Delete ${name}` }).click();
    await expect(page.locator('#delete-confirm-modal')).toBeVisible();
    await expect(page.locator('#delete-agent-name')).toHaveText(name);

    // Confirm → JS DELETE + reload. Wait for the row to disappear from the list.
    await page.locator('#delete-agent-go').click();
    await expect(
      page.locator('#main-content a[href^="/agents/"]').filter({ hasText: name }),
    ).toHaveCount(0, { timeout: 15000 });
    deleted = true;
  } finally {
    if (!deleted) {
      await page.request.delete(`/api/agents/${id}`).catch(() => {});
    }
  }
});
