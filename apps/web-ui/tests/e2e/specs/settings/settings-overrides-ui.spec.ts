import { test, expect } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

// Project agent-definition overrides (gateway/project_settings.templ
// agentOverridesPanel + settings_handlers.go):
//   POST /settings/overrides                       add/update (PRG; writes an
//                                                  existing name, creates a new one)
//   POST /settings/overrides/:agentName/delete     per-row delete (PRG)
//
// The override is a name-keyed project setting on the memory side — memory does
// not require the agent to exist — so a scratch `E2E override …` name is enough
// (no scratch agent needed). The spec asserts the list reflects create then
// delete, with a full reload between each state, and self-cleans the override in
// cleanup (pass or fail) plus a trailing guard test.
//
// Locators: the row has no stable wrapper id, but the delete control carries a
// unique accessible name ("Delete override for <agentName>") and the agent name
// renders as its own text node, so no new data-testid anchors are needed.

const OVERRIDES = '/settings/overrides';

test('project overrides: create then delete, each reflected in the list and after reload', async ({
  page,
}) => {
  test.setTimeout(120_000);
  const stamp = Date.now();
  const agentName = `E2E override ${stamp}`;
  const model = `e2e/override-${stamp}`;
  const deleteBtn = () => page.getByRole('button', { name: `Delete override for ${agentName}` });

  try {
    await page.goto(OVERRIDES);
    await expectAppPage(page, /Agent overrides/);
    await expect(deleteBtn()).toHaveCount(0);

    // CREATE: the form upserts by agent name and PRG-redirects back to the list.
    await page.locator('#override-agent').fill(agentName);
    await page.locator('#override-prompt').fill('E2E override prompt');
    await page.locator('#override-model').fill(model);
    await page.getByRole('button', { name: 'Save override' }).click();
    await page.waitForURL(/\/settings\/overrides\?updated=1/);
    await expectAppPage(page, /Agent overrides/);

    // List reflects the create: our row (unique delete control + name + summary).
    await expect(deleteBtn()).toHaveCount(1);
    await expect(page.getByText(agentName, { exact: true })).toBeVisible();
    await expect(page.getByText(`model ${model}`)).toBeVisible();

    // Persisted, not just an in-page POST: the row survives a full reload.
    await page.reload();
    await expectAppPage(page, /Agent overrides/);
    await expect(deleteBtn()).toHaveCount(1);
    await expect(page.getByText(agentName, { exact: true })).toBeVisible();
    await expect(page.getByText(`model ${model}`)).toBeVisible();

    // DELETE via the row action (native PRG form).
    await deleteBtn().click();
    await page.waitForURL(/\/settings\/overrides\?updated=1/);
    await expectAppPage(page, /Agent overrides/);
    await expect(deleteBtn()).toHaveCount(0);
    await expect(page.getByText(agentName, { exact: true })).toHaveCount(0);

    // The deletion survives a full reload too.
    await page.reload();
    await expectAppPage(page, /Agent overrides/);
    await expect(deleteBtn()).toHaveCount(0);
    await expect(page.getByText(agentName, { exact: true })).toHaveCount(0);
  } finally {
    // Belt-and-braces: never leave the scratch override behind (deleting an
    // absent override is a no-op on the gateway).
    await page.request
      .post(`/settings/overrides/${encodeURIComponent(agentName)}/delete`)
      .catch(() => {});
  }
});

test('leaves no E2E overrides behind (self-cleanup guard)', async ({ page }) => {
  await page.goto(OVERRIDES);
  await expectAppPage(page, /Agent overrides/);
  await expect(page.getByRole('button', { name: /^Delete override for E2E override / })).toHaveCount(
    0,
  );
});
