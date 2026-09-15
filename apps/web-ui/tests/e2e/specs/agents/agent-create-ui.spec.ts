import { test } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';
import { createAgentViaModal } from '../../helpers/agents';

/**
 * Creates an agent through the real UI (Agents page → "New agent" modal,
 * driven by createAgentViaModal) and then smoke-tests the four detail pages
 * the entity unlocks. The agent is deleted via API afterwards so the reused
 * tenant doesn't accumulate entities.
 */
test('creates an agent via the form and renders its detail pages', async ({ page }) => {
  const name = `E2E Agent ${Date.now()}`;
  let id = '';

  try {
    // createAgentViaModal submits with a name only — no model, so the "Auto —
    // default model" option stays selected, mirroring the empty
    // tool/skill/config fixture a bare agent needs — and returns the new
    // agent's id parsed from the reloaded list's row link.
    id = await createAgentViaModal(page, name);

    // Each detail page titles itself with the agent name; an error state would
    // instead render a static label (no name), so matching the name also catches
    // failed loads.
    const title = new RegExp(name);

    await page.goto(`/agents/${id}`);
    await expectAppPage(page, title);

    await page.goto(`/agents/${id}/settings`);
    await expectAppPage(page, title);

    await page.goto(`/agents/${id}/sandbox`);
    await expectAppPage(page, title);

    await page.goto(`/agents/${id}/sessions`);
    await expectAppPage(page, title);
  } finally {
    await page.request.delete(`/api/agents/${id}`).catch(() => {});
  }
});
