import { test, expect } from '@playwright/test';

test('creates a schedule for an agent', async ({ page }) => {
  const name = `E2E Sched ${Date.now()}`;

  // A schedule needs an agent definition; create one first (and clean it up).
  const agentResp = await page.request.post('/api/agents', {
    data: { name: `E2E Sched Agent ${Date.now()}`, tools: [], skills: [], config: {} },
  });
  expect(agentResp.ok(), `create agent failed: ${await agentResp.text()}`).toBeTruthy();
  const agent = await agentResp.json();
  const agentId = agent.id as string;

  try {
    await page.goto('/schedules/new');
    await page.locator('#sched-name').fill(name);
    await page.locator('#sched-cron').fill('0 8 * * *');
    await page.locator('#sched-def').selectOption(agentId);
    await page.getByRole('button', { name: 'Create schedule' }).click();

    await page.waitForURL(/\/schedules/);
    await expect(page.getByText(name).first()).toBeVisible();
  } finally {
    // Best-effort cleanup: delete the schedule(s) we created, then the agent.
    try {
      const scheds = (await (await page.request.get('/api/schedules')).json()) as Array<{
        id: string;
        name: string;
      }>;
      for (const s of scheds ?? []) {
        if (s.name === name) await page.request.delete(`/api/schedules/${s.id}`);
      }
    } catch {
      /* ignore */
    }
    await page.request.delete(`/api/agents/${agentId}`).catch(() => {});
  }
});
