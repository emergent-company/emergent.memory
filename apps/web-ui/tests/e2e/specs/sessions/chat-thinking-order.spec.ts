import { test, expect } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

test.describe('Chat live badge ordering', () => {
  test('thinking and tool badges stay below the in-progress assistant bubble', async ({ page }) => {
    await page.goto('/chat');
    await expectAppPage(page, /Chat/);

    // The live stream order must be chronological: assistant bubble (typing →
    // answer) first, then thinking, then tool chips. The debug hooks below drive
    // the exact same code path the SSE stream uses (openAssistantBubble →
    // handleThinkingEvent → handleToolEvent) without needing a live LLM, so the
    // old insertBefore hoisting (badge above the bubble) cannot silently return.
    await page.waitForFunction(() => {
      const w = window as unknown as {
        MemoryChat?: {
          _debugOpenBubble?: () => void;
          _debugThinking?: (p: unknown) => void;
          _debugTool?: (p: unknown) => void;
        };
      };
      return !!w.MemoryChat && !!document.getElementById('chat-messages');
    });

    const order = await page.evaluate(() => {
      const w = window as unknown as {
        MemoryChat: {
          _debugOpenBubble: () => void;
          _debugThinking: (p: Record<string, unknown>) => void;
          _debugTool: (p: Record<string, unknown>) => void;
        };
      };
      w.MemoryChat._debugOpenBubble();
      w.MemoryChat._debugThinking({ id: 't1', role: 'operator', text: 'step 1', done: true });
      w.MemoryChat._debugTool({ tool: 'web_search', status: 'running' });

      const messages = document.getElementById('chat-messages');
      if (!messages) return null;
      return Array.from(messages.children).map((el) => {
        if (el.querySelector('.memory-thinking')) return 'thinking';
        if (el.querySelector('.memory-tool-chip')) return 'tool';
        if (el.querySelector('.chat-bubble')) return 'bubble';
        return 'other';
      });
    });

    expect(order).toEqual(['bubble', 'thinking', 'tool']);
  });
});
