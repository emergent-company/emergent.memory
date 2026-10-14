import { test, expect } from '@playwright/test';
import { createAgentViaModal } from '../../helpers/agents';

// Agent Work settings subpage: gateway/agent.templ `agentWorkSettingsForm` →
// POST /agents/:id/settings/work (agent.go `uiAgentUpdateWork` +
// `applyAgentWorkSection`). It persists the object-driven work configuration —
// status phases, the review gate, failure/revision budgets, retry backoff, the
// deliverable contract, and the trigger statuses that wake the agent.
//
// One deterministic round-trip test: set the ready status, failure limit,
// review gate, and trigger statuses, save, reload, and assert the values
// persisted. Blueprint-derived agents gate the save behind a confirmation
// dialog, which is not exercised here (creating one requires blueprint install
// state outside the modal helper's scope).
//
// Creates one scratch agent and deletes it in `finally`.

const WORK = (id: string) => `/agents/${encodeURIComponent(id)}/settings/work`;

test('work configuration persists across a full reload', async ({ page }) => {
  const name = `E2E Work ${Date.now()}`;
  const id = await createAgentViaModal(page, name);

  try {
    await page.goto(WORK(id));

    const ready = page.locator('input[name="workStatus.ready"]');
    await expect(ready).toBeVisible();
    await ready.fill('todo');

    const failureLimit = page.locator('input[name="failureLimit"]');
    await expect(failureLimit).toBeVisible();
    await failureLimit.fill('5');

    const requiresReview = page.locator('input[name="workRequiresReview"]');
    await expect(requiresReview).toBeVisible();
    await requiresReview.check();

    const triggerStatuses = page.locator('input[name="triggerStatus"]');
    await expect(triggerStatuses).toBeVisible();
    await triggerStatuses.fill('todo, rework');

    await page.getByRole('button', { name: 'Save changes' }).click();
    await page.waitForURL(/\/settings\/work\?updated=1/);

    await page.reload();
    await expect(page.locator('input[name="workStatus.ready"]')).toHaveValue('todo');
    await expect(page.locator('input[name="failureLimit"]')).toHaveValue('5');
    await expect(page.locator('input[name="workRequiresReview"]')).toBeChecked();
    await expect(page.locator('input[name="triggerStatus"]')).toHaveValue('todo, rework');
  } finally {
    await page.request.delete(`/api/agents/${id}`).catch(() => {});
  }
});
