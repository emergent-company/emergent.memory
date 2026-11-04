import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Hermetic DOM spec for the Paseo-style subagent composer track.
const core = (name: string) =>
  path.resolve(__dirname, '../../../../gateway/webui/static/js', name);

async function load(page: Page): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));
  await page.setContent(
    '<!doctype html><html><body><div id="chat-subagents" class="hidden"></div></body></html>',
  );
  for (const name of ['chat-transport.js', 'chat-components.js', 'chat-host.js', 'chat-stream.js']) {
    await page.addScriptTag({ path: core(name) });
  }
  return errors;
}

async function bootTrack(page: Page): Promise<void> {
  await page.evaluate(() => {
    const w = window as unknown as {
      __track: {
        upsert: (e: unknown) => void;
        rebuild: (i: unknown[]) => void;
        reset: () => void;
      };
      MemoryChatComponents: { createSubagentTrack: (o: unknown) => unknown };
    };
    w.__track = w.MemoryChatComponents.createSubagentTrack({
      mount: () => document.getElementById('chat-subagents'),
      openRun: (id: string) => '/runs/' + encodeURIComponent(id),
    }) as typeof w.__track;
  });
}

function upsert(page: Page, evt: Record<string, unknown>): Promise<void> {
  return page.evaluate(
    (e) => (window as unknown as { __track: { upsert: (e: unknown) => void } }).__track.upsert(e),
    evt,
  );
}

const T0 = '2026-09-30T10:00:00.000Z';

test.describe('subagent composer track — live', () => {
  test('upserts by run id and counts working/paused/failed segments', async ({ page }) => {
    const errors = await load(page);
    await bootTrack(page);
    await upsert(page, { type: 'subagent', id: 's1', runId: 'r1', agentName: 'Researcher', task: 'scan docs', status: 'queued' });
    await upsert(page, { type: 'subagent', id: 's2', runId: 'r2', agentName: 'Writer', task: 'draft', status: 'running' });
    await upsert(page, { type: 'subagent', id: 's3', runId: 'r3', agentName: 'Reviewer', task: 'check', status: 'paused' });
    await upsert(page, { type: 'subagent', id: 's4', runId: 'r4', agentName: 'Ran', task: 'x', status: 'failed', error: 'boom' });

    const mount = page.locator('#chat-subagents');
    await expect(mount).not.toHaveClass(/hidden/);
    const pill = mount.getByTestId('chat-subagent-pill');
    await expect(pill).toContainText('2 working');
    await expect(pill).toContainText('1 needs input');
    await expect(pill).toContainText('1 failed');

    // A terminal update replaces that row's working state, never duplicates it.
    await upsert(page, { type: 'subagent', id: 's1', runId: 'r1', agentName: 'Researcher', status: 'completed' });
    await expect(pill).toContainText('1 working');
    await expect(mount.getByTestId('chat-subagent-row')).toHaveCount(4);
    await expect(mount.locator('[data-run-id="r1"]')).toContainText('Researcher');
    expect(errors).toEqual([]);
  });

  test('the pill opens a popover with status dots, tasks and run links', async ({ page }) => {
    await load(page);
    await bootTrack(page);
    await upsert(page, { type: 'subagent', id: 's1', runId: 'r1', agentName: 'Researcher', task: 'scan docs', status: 'running' });
    await upsert(page, { type: 'subagent', id: 's2', runId: 'r2', agentName: 'Ran', task: 'x', status: 'failed', error: 'boom' });

    const mount = page.locator('#chat-subagents');
    await mount.getByTestId('chat-subagent-pill').click();
    const popover = mount.getByTestId('chat-subagent-popover');
    await expect(popover).toBeVisible();
    await expect(popover.getByTestId('chat-subagent-row')).toHaveCount(2);
    await expect(popover.locator('[data-run-id="r1"]')).toHaveAttribute('href', '/runs/r1');
    await expect(popover.locator('[data-run-id="r1"]')).toHaveAttribute('target', '_blank');
    await expect(popover.locator('[data-run-id="r2"] .memory-subagent-error')).toHaveText('boom');
    await expect(popover.locator('.memory-subagent-dot[data-status="working"]')).toHaveCount(1);
    await expect(popover.locator('.memory-subagent-dot[data-status="failed"]')).toHaveCount(1);
  });

  test('a new parent run replaces the rows; reset clears the track', async ({ page }) => {
    await load(page);
    await bootTrack(page);
    await upsert(page, { type: 'subagent', id: 's1', parentRunId: 'p1', runId: 'r1', agentName: 'One', status: 'running' });
    await upsert(page, { type: 'subagent', id: 's2', parentRunId: 'p2', runId: 'r2', agentName: 'Two', status: 'running' });
    const mount = page.locator('#chat-subagents');
    await expect(mount.getByTestId('chat-subagent-row')).toHaveCount(1);
    await expect(mount).toContainText('Two');

    await page.evaluate(() =>
      (window as unknown as { __track: { reset: () => void } }).__track.reset(),
    );
    await expect(mount).toHaveClass(/hidden/);
    await expect(mount.getByTestId('chat-subagent-row')).toHaveCount(0);
  });

  test('live_replay subagent frames render through the shared dispatcher', async ({ page }) => {
    await load(page);
    await page.evaluate(() => {
      const w = window as unknown as {
        __handle: (raw: string) => void;
        MemoryChatComponents: { createSubagentTrack: (o: unknown) => { upsert: (e: unknown) => void } };
        MemoryChatHost: { makeHandleEvent: (h: unknown) => (raw: string) => void };
      };
      const track = w.MemoryChatComponents.createSubagentTrack({
        mount: () => document.getElementById('chat-subagents'),
        openRun: (id: string) => '/runs/' + id,
      });
      w.__handle = w.MemoryChatHost.makeHandleEvent({
        getStreaming: () => true,
        setBubbleHTML() {},
        updateBubbleText() {},
        scrollToBottom() {},
        handleToolEvent() {},
        renderQuestion() {},
        renderApproval() {},
        failStream() {},
        onMeta() {},
        onSubagent: track.upsert,
      });
    });
    await page.evaluate(() =>
      (window as unknown as { __handle: (raw: string) => void }).__handle(
        JSON.stringify({
          type: 'live_replay',
          frames: [
            { type: 'subagent', id: 's1', runId: 'r1', agentName: 'Replayed', status: 'running' },
          ],
        }),
      ),
    );
    await expect(page.locator('#chat-subagents')).not.toHaveClass(/hidden/);
    await expect(page.locator('#chat-subagents')).toContainText('Replayed');
  });
});

test.describe('subagent composer track — history', () => {
  test('rebuilds spawn_agents + trigger_agent rows from the newest run', async ({ page }) => {
    await load(page);
    await bootTrack(page);
    await page.evaluate((t0) => {
      const w = window as unknown as {
        __track: { rebuild: (i: unknown[]) => void };
      };
      w.__track.rebuild([
      { kind: 'run_start', run_id: 'old', run_status: 'completed', created_at: t0 },
      {
        kind: 'tool_call',
        id: 'old-spawn',
        run_id: 'old',
        tool_name: 'spawn_agents',
        tool_status: 'completed',
        tool_input: { agents: [{ agent_name: 'Old', task: 'old task' }] },
        tool_output: { results: [{ agent_name: 'Old', run_id: 'old-child', status: 'completed' }] },
        created_at: t0,
      },
      { kind: 'run_start', run_id: 'new', run_status: 'working', created_at: t0 },
      {
        kind: 'tool_call',
        id: 'spawn-1',
        run_id: 'new',
        tool_name: 'spawn_agents',
        tool_status: 'completed',
        tool_input: {
          agents: [
            { agent_name: 'Researcher', task: 'scan docs' },
            { agent_name: 'Writer', task: 'draft' },
          ],
        },
        tool_output: {
          results: [
            { agent_name: 'Researcher', run_id: 'child-1', status: 'completed' },
            { agent_name: 'Writer', run_id: 'child-2', status: 'running' },
          ],
        },
        created_at: t0,
      },
      {
        kind: 'tool_call',
        id: 'trigger-1',
        run_id: 'new',
        tool_name: 'trigger_agent',
        tool_status: 'completed',
        tool_input: { agent_name: 'Reviewer', message: { instructions: 'check it' } },
        tool_output: { run_id: 'child-3', status: 'paused' },
        created_at: t0,
      },
      ]);
    }, T0);

    const mount = page.locator('#chat-subagents');
    await expect(mount.getByTestId('chat-subagent-row')).toHaveCount(3);
    await expect(mount).toContainText('Researcher');
    await expect(mount).toContainText('scan docs');
    await expect(mount).toContainText('1 working');
    await expect(mount).toContainText('1 needs input');
    await expect(mount.locator('[data-run-id="child-1"]')).toHaveCount(1);
    await expect(mount.locator('[data-run-id="old-child"]')).toHaveCount(0);
  });

  test('drops a queued trigger_agent row rebuilt from persisted history', async ({ page }) => {
    await load(page);
    await bootTrack(page);
    await page.evaluate((t0) => {
      const w = window as unknown as { __track: { rebuild: (i: unknown[]) => void } };
      w.__track.rebuild([
        { kind: 'run_start', run_id: 'r1', run_status: 'working', created_at: t0 },
        {
          kind: 'tool_call',
          id: 't1',
          run_id: 'r1',
          tool_name: 'trigger_agent',
          tool_status: 'completed',
          tool_input: { agent_name: 'Queued', message: 'later' },
          // Persisted runstatus.Queued is "submitted". It has no parent-stream
          // terminal projection, so it must not become a permanent working row.
          tool_output: { run_id: 'c-queued', status: 'submitted' },
          created_at: t0,
        },
      ]);
    }, T0);
    await expect(page.locator('#chat-subagents')).toHaveClass(/hidden/);
    await expect(page.locator('#chat-subagents')).not.toContainText('working');
  });

  test('all-completed rows fall back to the plain subagent count', async ({ page }) => {
    await load(page);
    await bootTrack(page);
    await page.evaluate((t0) => {
      const w = window as unknown as { __track: { rebuild: (i: unknown[]) => void } };
      w.__track.rebuild([
        { kind: 'run_start', run_id: 'r1', run_status: 'completed', created_at: t0 },
        {
          kind: 'tool_call',
          id: 't1',
          run_id: 'r1',
          tool_name: 'trigger_agent',
          tool_status: 'completed',
          tool_input: { agent_name: 'Done', message: 'finish' },
          tool_output: { run_id: 'c1', status: 'completed' },
          created_at: t0,
        },
      ]);
    }, T0);
    await expect(page.locator('#chat-subagents [data-testid="chat-subagent-pill"]')).toContainText('1 subagent');
  });
});
