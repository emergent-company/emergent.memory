import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Hermetic DOM spec for Paseo-style contiguous thinking.
//
// Live thinking merges every frame into one row until an activity boundary;
// history merges adjacent reasoning items in the same run. Any tool call,
// assistant text, question/approval, A2UI card or run boundary ends the block.
const core = (name: string) =>
  path.resolve(__dirname, '../../../../gateway/webui/static/js', name);

async function load(page: Page): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));
  await page.setContent('<!doctype html><html><body><div id="d"></div></body></html>');
  for (const name of ['chat-transport.js', 'chat-components.js', 'chat-host.js', 'chat-stream.js']) {
    await page.addScriptTag({ path: core(name) });
  }
  return errors;
}

async function bootLive(page: Page): Promise<void> {
  await page.evaluate(() => {
    const w = window as unknown as {
      __thinking: {
        handle: (e: unknown) => void;
        break: () => void;
        clear: () => void;
        finalize: () => void;
      };
      MemoryChatComponents: { createThinkingHost: (o: unknown) => unknown };
    };
    const messages = document.getElementById('d') as HTMLElement;
    messages.innerHTML = '';
    const badgeCtx = { messages, hideEmpty() {}, scrollToBottom() {} };
    w.__thinking = w.MemoryChatComponents.createThinkingHost({
      badgeCtx,
      scrollToBottom() {},
    }) as typeof w.__thinking;
  });
}

async function think(page: Page, id: string, text: string, done = false): Promise<void> {
  await page.evaluate(
    ({ id, text, done }) =>
      (window as unknown as { __thinking: { handle: (e: unknown) => void } }).__thinking.handle({
        id,
        text,
        done,
        role: 'operator',
      }),
    { id, text, done },
  );
}

const T0 = '2026-09-30T10:00:00.000Z';
const T1 = '2026-09-30T10:00:01.000Z';

function renderHistory(page: Page, items: unknown[]): Promise<void> {
  return page.evaluate((list) => {
    const w = window as unknown as {
      MemoryChatComponents: { renderTimeline: (i: unknown, c: unknown, f: unknown) => void };
    };
    const messages = document.getElementById('d') as HTMLElement;
    messages.innerHTML = '';
    const badgeCtx = { messages, hideEmpty() {}, scrollToBottom() {} };
    w.MemoryChatComponents.renderTimeline(
      list,
      {
        container: messages,
        badgeCtx,
        getAgentName: () => 'Memory',
        addUserMessage() {},
        addAssistantMessage(html: string) {
          const el = document.createElement('div');
          el.innerHTML = html || '';
          messages.appendChild(el);
          return el;
        },
        renderThinkingBlock(text: string, html: string | null) {
          const wrap = document.createElement('div');
          wrap.className = 'chat chat-start memory-rise memory-aux';
          const badge = document.createElement('div');
          badge.className = 'memory-thinking';
          const body = document.createElement('p');
          body.className = 'memory-thinking-body';
          if (html) body.innerHTML = html;
          else body.textContent = text;
          badge.appendChild(body);
          wrap.appendChild(badge);
          messages.appendChild(wrap);
          return { body };
        },
        toolChip() {
          const chip = document.createElement('div');
          chip.className = 'memory-tool-chip';
          messages.appendChild(chip);
          return chip;
        },
        renderHistoryQuestion() {},
      },
      {},
    );
  }, items);
}

test.describe('contiguous thinking — live', () => {
  test('adjacent frames merge into one row even when ids differ', async ({ page }) => {
    const errors = await load(page);
    await bootLive(page);
    await think(page, 't1', 'first ');
    await think(page, 't1', 'second');
    await think(page, 't2', ' third', true);

    const rows = page.locator('#d .memory-thinking');
    await expect(rows).toHaveCount(1);
    await expect(rows.locator('.memory-thinking-body')).toHaveText('first second third');
    await expect(rows.locator('.memory-thinking-role')).toHaveCount(0);
    expect(errors).toEqual([]);
  });

  test('an activity boundary closes the block; the next frame starts a new row', async ({ page }) => {
    await load(page);
    await bootLive(page);
    await think(page, 't1', 'before tool');
    await page.evaluate(() =>
      (window as unknown as { __thinking: { break: () => void } }).__thinking.break(),
    );
    await think(page, 't2', 'after tool');

    const rows = page.locator('#d .memory-thinking');
    await expect(rows).toHaveCount(2);
    await expect(rows.nth(0).locator('.memory-thinking-body')).toHaveText('before tool');
    await expect(rows.nth(1).locator('.memory-thinking-body')).toHaveText('after tool');
  });

  test('live_replay frames render through the same contiguous host', async ({ page }) => {
    await load(page);
    await page.evaluate(() => {
      const w = window as unknown as {
        __handle: (raw: string) => void;
        MemoryChatComponents: { createThinkingHost: (o: unknown) => { handle: (e: unknown) => void } };
        MemoryChatHost: { makeHandleEvent: (h: unknown) => (raw: string) => void };
      };
      const messages = document.getElementById('d') as HTMLElement;
      const badgeCtx = { messages, hideEmpty() {}, scrollToBottom() {} };
      const thinking = w.MemoryChatComponents.createThinkingHost({ badgeCtx, scrollToBottom() {} });
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
        onThinking: thinking.handle,
      });
    });
    await page.evaluate(() =>
      (window as unknown as { __handle: (raw: string) => void }).__handle(
        JSON.stringify({
          type: 'live_replay',
          frames: [
            { type: 'thinking', id: 'a', text: 'replayed ' },
            { type: 'thinking', id: 'b', text: 'reasoning', done: true },
          ],
        }),
      ),
    );
    await expect(page.locator('#d .memory-thinking')).toHaveCount(1);
    await expect(page.locator('#d .memory-thinking-body')).toHaveText('replayed reasoning');
  });
});

test.describe('contiguous thinking — history', () => {
  test('adjacent reasoning items in the same run merge into one row', async ({ page }) => {
    await load(page);
    await renderHistory(page, [
      { kind: 'run_start', run_id: 'r1', run_status: 'working', created_at: T0 },
      {
        kind: 'message',
        role: 'operator',
        content: { text: 'one', function_calls: [{ name: 'x' }] },
        run_id: 'r1',
        created_at: T0,
      },
      {
        kind: 'message',
        role: 'operator',
        content: { text: 'two', function_calls: [{ name: 'x' }] },
        run_id: 'r1',
        created_at: T1,
      },
    ]);
    await expect(page.locator('#d .memory-thinking')).toHaveCount(1);
    await expect(page.locator('#d .memory-thinking-body')).toContainText('one');
    await expect(page.locator('#d .memory-thinking-body')).toContainText('two');
  });

  test('a reasoning-plus-answer item keeps its answer below the Thinking row', async ({ page }) => {
    await load(page);
    await renderHistory(page, [
      { kind: 'run_start', run_id: 'r1', run_status: 'working', created_at: T0 },
      {
        kind: 'message',
        role: 'agent',
        content: { reasoning: 'why it works', html: '<p>the answer</p>' },
        run_id: 'r1',
        created_at: T0,
      },
      {
        kind: 'message',
        role: 'operator',
        content: { text: 'later reasoning', function_calls: [{ name: 'x' }] },
        run_id: 'r1',
        created_at: T1,
      },
    ]);
    // The answer sits between the two reasoning blocks, so they stay separate.
    await expect(page.locator('#d .memory-thinking')).toHaveCount(2);
    await expect(page.locator('#d .memory-thinking-body').first()).toContainText('why it works');
    await expect(page.locator('#d')).toContainText('the answer');
    await expect(page.locator('#d .memory-thinking-body').last()).toContainText('later reasoning');
  });

  test('a tool call between reasoning items starts a second Thinking row', async ({ page }) => {
    await load(page);
    await renderHistory(page, [
      { kind: 'run_start', run_id: 'r1', run_status: 'working', created_at: T0 },
      {
        kind: 'message',
        role: 'operator',
        content: { text: 'before', function_calls: [{ name: 'x' }] },
        run_id: 'r1',
        created_at: T0,
      },
      {
        kind: 'tool_call',
        id: 'c1',
        run_id: 'r1',
        tool_name: 'entity-query',
        tool_status: 'completed',
        created_at: T1,
      },
      {
        kind: 'message',
        role: 'operator',
        content: { text: 'after', function_calls: [{ name: 'x' }] },
        run_id: 'r1',
        created_at: T1,
      },
    ]);
    await expect(page.locator('#d .memory-thinking')).toHaveCount(2);
  });
});
