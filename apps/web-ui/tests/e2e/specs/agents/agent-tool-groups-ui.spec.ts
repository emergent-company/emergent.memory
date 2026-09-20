import { test, expect } from '@playwright/test';
import { createAgentViaModal } from '../../helpers/agents';

// Agent Tools settings subpage: gateway/agent.templ `agentToolsSettingsForm` →
// POST /agents/:id/settings/tools (agent.go `uiAgentUpdateTools` +
// `applyAgentToolsSection` + `applyToolGroups`). It persists the default
// approval policy, and — when the memory backend reports a capability taxonomy
// (`AgentDefinition.ToolGroups`) — per-group enable switches and tri-state
// approval policies.
//
// Two tests:
//   1. The default approval policy round-trip, which always runs: the select
//      renders regardless of tool taxonomy, so it is fully deterministic.
//   2. The capability-group policy + enable round-trip, which probes for the
//      server-computed groups and skips with a stated reason when the deployed
//      backend predates the taxonomy (the same defensive pattern as
//      agent-mcp-keys-ui.spec.ts) — the group-level UI simply does not render
//      without `ToolGroups` on the definition.
//
// Both create one scratch agent and delete it in `finally`.

const TOOLS = (id: string) => `/agents/${encodeURIComponent(id)}/settings/tools`;

test('default approval policy persists across a full reload', async ({ page }) => {
  const name = `E2E Tools ${Date.now()}`;
  const id = await createAgentViaModal(page, name);

  try {
    await page.goto(TOOLS(id));

    const policy = page.locator('select[name="defaultToolPolicy"]');
    await expect(policy).toBeVisible();
    await policy.selectOption('ask');

    await page.getByRole('button', { name: 'Save changes' }).click();
    await page.waitForURL(/\/settings\/tools\?updated=1/);

    await page.reload();
    await expect(page.locator('select[name="defaultToolPolicy"]')).toHaveValue('ask');
  } finally {
    await page.request.delete(`/api/agents/${id}`).catch(() => {});
  }
});

test('capability-group approval policy and enable switch persist', async ({ page }) => {
  const name = `E2E ToolGroups ${Date.now()}`;
  const id = await createAgentViaModal(page, name);

  try {
    await page.goto(TOOLS(id));

    // Capability groups render inside `[data-testid="tool-groups"]` as
    // `[data-testid="tool-group"]` elements; the "other" uncovered bucket uses a
    // distinct `tool-group-other` testid, so this selector is unambiguous.
    const groups = page.locator('[data-testid="tool-group"]');
    if ((await groups.count()) === 0) {
      test.skip(true, 'memory backend reports no capability taxonomy — group-level UI unavailable');
    }

    // Pick the first group that carries a policy select (groups with id "other"
    // have only an enable switch, no policy select).
    const policyGroups = groups.filter({
      has: page.locator('[data-testid^="tool-group-policy-"]'),
    });
    if ((await policyGroups.count()) === 0) {
      test.skip(true, 'no capability group exposes an approval-policy select');
    }

    const group = policyGroups.first();
    const groupId = (await group.getAttribute('data-tool-group')) ?? '';
    if (!groupId) {
      test.skip(true, 'capability group is missing its data-tool-group id');
    }

    // Set the group policy to "ask" — deterministic and independent of the
    // group's member tools.
    await page.getByTestId(`tool-group-policy-${groupId}`).selectOption('ask');

    // Toggle the enable switch only when the group has member tools: enabling
    // an empty group is a no-op fan-out, so the switch would not round-trip.
    const toggle = page.getByTestId(`tool-group-enabled-${groupId}`);
    const memberCount = await group.locator('input[name="tool"]').count();
    const wasEnabled = await toggle.isChecked();
    if (memberCount > 0) {
      await toggle.setChecked(!wasEnabled);
    }

    await page.getByRole('button', { name: 'Save changes' }).click();
    await page.waitForURL(/\/settings\/tools\?updated=1/);

    await page.reload();
    await expect(page.getByTestId(`tool-group-policy-${groupId}`)).toHaveValue('ask');
    if (memberCount > 0) {
      await expect(page.getByTestId(`tool-group-enabled-${groupId}`)).toBeChecked({
        checked: !wasEnabled,
      });
    }
  } finally {
    await page.request.delete(`/api/agents/${id}`).catch(() => {});
  }
});
