import { test, expect } from '@playwright/test';
import { createAgentViaModal } from '../../helpers/agents';

// Agent name edit: gateway/agent.templ `agentGeneralSettingsForm` (the General
// subpage, the default landing of `/agents/:id/settings`) auto-saves through
// POST `/agents/:id/settings/general/autosave` (`uiAgentAutosaveGeneral` +
// `applyAgentGeneralSection`). The General page no longer renders an explicit
// Save changes button — the client debounces the edit and reports the outcome
// in `[data-testid="agent-settings-autosave-status"]`. Model + tools editing are
// covered by agent-model-switch-ui and agent-tool-groups-ui respectively; this
// spec closes the remaining name-edit gap (task 4.1). One scratch agent, deleted
// in `finally`.

test('editing an agent name auto-saves and persists across a full reload', async ({ page }) => {
  const name = `E2E Edit ${Date.now()}`;
  const renamed = `${name} Renamed`;
  const id = await createAgentViaModal(page, name);

  try {
    // The General section is the default landing of the settings hub.
    await page.goto(`/agents/${id}/settings`);
    await expect(page.locator('#agent-settings-name')).toHaveValue(name);

    // No explicit Save: the field auto-saves after a short debounce and the
    // status node reports the save (it starts at `idle`).
    await page.locator('#agent-settings-name').fill(renamed);
    const status = page.getByTestId('agent-settings-autosave-status');
    await expect(status).toHaveAttribute('data-state', 'saved');

    await page.reload();
    await expect(page.locator('#agent-settings-name')).toHaveValue(renamed);
  } finally {
    await page.request.delete(`/api/agents/${id}`).catch(() => {});
  }
});
