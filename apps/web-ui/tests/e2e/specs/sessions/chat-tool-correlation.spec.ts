import { test, expect } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

// Regression coverage for the tool-call correlation + in-flight replay lanes:
// mcp_tool events now carry a stable call `id` and the client matches chips by
// `data-call-id` first (tool-name fallback only when the id is absent), so
// parallel/repeated same-name calls no longer collide. The debug hooks drive the
// exact same handlers the SSE stream uses (handleToolEvent / dispatchFrame), so
// these assertions exercise the real code path without a live LLM.

type DebugHooks = {
  _debugTool: (p: Record<string, unknown>) => void;
  _debugReplay: (p: Record<string, unknown>) => void;
};

async function boot(page: import('@playwright/test').Page): Promise<void> {
  await page.goto('/chat');
  await expectAppPage(page, /Chat/);
  await page.waitForFunction(() => {
    const w = window as unknown as { MemoryChat?: Partial<DebugHooks> };
    return !!w.MemoryChat && !!document.getElementById('chat-messages');
  });
}

test.describe('Chat tool call correlation', () => {
  test('parallel same-name calls correlate by call id', async ({ page }) => {
    await boot(page);

    const result = await page.evaluate(() => {
      const w = window as unknown as { MemoryChat: DebugHooks };
      // Two parallel invocations of the same tool, distinct call ids.
      w.MemoryChat._debugTool({ tool: 'web_search', status: 'running', id: 'call-a' });
      w.MemoryChat._debugTool({ tool: 'web_search', status: 'running', id: 'call-b' });
      // Terminal events arrive out of order — correlation must be by id, not by
      // "last same-name chip", or one chip would be left running and the other
      // would double-resolve.
      w.MemoryChat._debugTool({ tool: 'web_search', status: 'completed', id: 'call-b', result: { ok: true } });
      w.MemoryChat._debugTool({ tool: 'web_search', status: 'completed', id: 'call-a', result: { ok: true } });

      const chips = Array.from(document.querySelectorAll('.memory-tool-chip'));
      return {
        count: chips.length,
        byId: chips.map((el) => ({
          callId: el.getAttribute('data-call-id'),
          status: el.getAttribute('data-status'),
        })),
      };
    });

    expect(result.count).toBe(2);
    const statusById = Object.fromEntries(result.byId.map((c) => [c.callId, c.status]));
    expect(statusById['call-a']).toBe('ok');
    expect(statusById['call-b']).toBe('ok');
    expect(result.byId.every((c) => c.status === 'ok')).toBe(true);
  });

  test('a terminal event with an id updates its chip and never duplicates', async ({ page }) => {
    await boot(page);

    const result = await page.evaluate(() => {
      const w = window as unknown as { MemoryChat: DebugHooks };
      w.MemoryChat._debugTool({ tool: 'web_search', status: 'running', id: 'call-x' });
      // Terminal with a matching id: update the open chip, don't fabricate a
      // second chip (the old "tool report arrives after the tool responded" gap).
      w.MemoryChat._debugTool({ tool: 'web_search', status: 'completed', id: 'call-x', result: { ok: true } });

      const chips = Array.from(document.querySelectorAll('.memory-tool-chip'));
      return {
        count: chips.length,
        callId: chips[0] ? chips[0].getAttribute('data-call-id') : null,
        status: chips[0] ? chips[0].getAttribute('data-status') : null,
      };
    });

    expect(result.count).toBe(1);
    expect(result.callId).toBe('call-x');
    expect(result.status).toBe('ok');
  });

  test('a live_replay frame renders an in-flight tool chip and thinking badge', async ({ page }) => {
    await boot(page);

    const result = await page.evaluate(() => {
      const w = window as unknown as { MemoryChat: DebugHooks };
      // The gateway replays the active run's open thinking segment + running
      // tool call on reconnect. These render through the same handlers as the
      // live path, in order, with their in-progress affordances intact.
      w.MemoryChat._debugReplay({
        runId: 'debug-run',
        frames: [
          { type: 'mcp_tool', tool: 'web_search', status: 'started', id: 'call-r1' },
          { type: 'thinking', id: 't-r1', role: 'operator', text: 'planning step', done: false },
        ],
      });

      const chips = Array.from(document.querySelectorAll('.memory-tool-chip'));
      const thinkings = Array.from(document.querySelectorAll('.memory-thinking'));
      return {
        toolCount: chips.length,
        toolStatus: chips[0] ? chips[0].getAttribute('data-status') : null,
        toolCallId: chips[0] ? chips[0].getAttribute('data-call-id') : null,
        thinkingCount: thinkings.length,
        thinkingLive: thinkings[0] ? thinkings[0].classList.contains('memory-badge-live') : false,
      };
    });

    expect(result.toolCount).toBe(1);
    expect(result.toolStatus).toBe('running');
    expect(result.toolCallId).toBe('call-r1');
    expect(result.thinkingCount).toBe(1);
    expect(result.thinkingLive).toBe(true);
  });
});
