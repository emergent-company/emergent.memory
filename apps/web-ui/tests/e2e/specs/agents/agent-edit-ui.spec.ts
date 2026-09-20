import { test, expect } from '@playwright/test';
import { createAgentViaModal } from '../../helpers/agents';

// Agent name edit: gateway/agent.templ `agentGeneralSettingsForm` (the General
// subpage, the default landing of `/agents/:id/settings`) → POST
// `/agents/:id/settings/general` (`applyAgentGeneralSection`), PRG-redirecting
// with `?updated=1`. Model + tools editing are covered by
// agent-model-switch-ui and agent-tool-groups-ui respectively; this spec closes
// the remaining name-edit gap (task 4.1). One scratch agent, deleted in
// `finally`.

test('editing an agent name persists across a full reload', async ({ page }) => {
  const name = `E2E Edit ${Date.now()}`;
  const renamed = `${name} Renamed`;
  const id = await createAgentViaModal(page, name);

  try {
    // The General section is the default landing of the settings hub.
    await page.goto(`/agents/${id}/settings`);
    await expect(page.locator('#agent-settings-name')).toHaveValue(name);

    await page.locator('#agent-settings-name').fill(renamed);
    await page.getByRole('button', { name: 'Save changes' }).click();

    // The General section maps to the bare settings route (agentSettingsSectionPath),
    // so the PRG redirect lands on `/agents/:id/settings?updated=1` — not `/settings/general`.
    await page.waitForURL(/\/settings\?updated=1/);
    await page.reload();
    await expect(page.locator('#agent-settings-name')).toHaveValue(renamed);
  } finally {
    await page.request.delete(`/api/agents/${id}`).catch(() => {});
  }
});
