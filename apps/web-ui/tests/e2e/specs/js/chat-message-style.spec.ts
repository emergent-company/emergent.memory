import { test, expect, type Page } from '@playwright/test';
import { readFileSync } from 'node:fs';
import path from 'node:path';

// Hermetic DOM spec for the transcript's message styling. Two contracts:
//
//   1. Paseo-style turns — assistant replies are plain full-width markdown (no
//      bubble/avatar/name header), user turns keep a right-aligned bubble
//      without a "You" label, copy lives in the assistant action row, and
//      Sources mount inside the assistant turn after the markdown.
//   2. One left edge — every left-side row (assistant reply, tool step,
//      question/approval card, A2UI card) starts at the transcript's content
//      edge. No row carries an avatar gutter or a name header, and no row
//      places its content in a chat-grid column.
const core = (name: string) =>
  path.resolve(__dirname, '../../../../gateway/webui/static/js', name);
const appCss = path.resolve(__dirname, '../../../../gateway/webui/css/app.css');

// Stand-in for the daisyUI component rules the transcript sits on, which the
// hermetic page would otherwise not have: a two-column grid with an avatar
// column. A left-side row that still carries an avatar (a regression) then
// indents its content, so the alignment assertion has something to catch.
const CHAT_GRID_STANDIN = `
  .chat { display: grid; grid-auto-rows: min-content; column-gap: 12px; }
  .chat-start { grid-template-columns: auto 1fr; }
  .chat-start > .chat-image { grid-column-start: 1; }
  .chat-start > .chat-image + * { grid-column-start: 2; }
  .chat-image { width: 40px; height: 40px; }
`;

async function load(page: Page): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));
  await page.setContent(
    '<!doctype html><html><head><style id="grid">' +
      CHAT_GRID_STANDIN +
      '</style><style id="app"></style></head><body><div id="d"></div></body></html>',
  );
  // The shipped stylesheet, unbuilt: the plain rules (the left-side row block,
  // the card border tints) are what this spec exercises; the Tailwind/daisyUI
  // directives it imports are ignored by the browser.
  await page.locator('#app').evaluate((el, css) => {
    (el as HTMLStyleElement).textContent = css;
  }, readFileSync(appCss, 'utf8'));
  for (const name of ['chat-transport.js', 'chat-components.js', 'chat-host.js', 'chat-stream.js']) {
    await page.addScriptTag({ path: core(name) });
  }
  return errors;
}

async function render(page: Page): Promise<void> {
  await page.evaluate(() => {
    const w = window as unknown as {
      MemoryChatComponents: {
        attachSources: (wrap: Element, citations: unknown[]) => void;
      };
      MemoryChatStream: { createEngine: (ctx: unknown) => unknown };
    };
    const messages = document.getElementById('d') as HTMLElement;
    const badgeCtx = { messages, hideEmpty() {}, scrollToBottom() {} };
    const stream = w.MemoryChatStream.createEngine({
      messages,
      badgeCtx,
      hideEmpty() {},
      scrollToBottom() {},
      setStreaming() {},
      onStreamFinish() {},
      onStreamFail() {},
      answerQuestion() {},
      currentAgentName: () => 'Memory',
    }) as unknown as {
      addUserMessage: (t: string) => void;
      addAssistantMessage: (h: string, n: string) => HTMLElement;
      toolChip: (tool: string, status: string, detail: string, payload: unknown) => unknown;
      renderQuestion: (evt: unknown) => void;
    };
    stream.addUserMessage('hello');
    const reply = stream.addAssistantMessage('<p>plain <strong>markdown</strong></p>', 'Memory');
    w.MemoryChatComponents.attachSources(reply, [
      { id: '11111111-1111-4111-8111-111111111111', type: 'Thing', label: 'A source' },
    ]);
    stream.toolChip('web_search', 'ok', 'searched', { tool: 'web_search', status: 'ok' });
    stream.renderQuestion({
      questionId: 'q-1',
      question: 'Pick one',
      interactionType: 'buttons',
      options: [{ label: 'First', value: 'first' }],
    });
  });
}

test('assistant turns are plain markdown; user turns keep a labelled-free bubble', async ({ page }) => {
  const errors = await load(page);
  await render(page);

  const user = page.locator('#d .chat-end');
  await expect(user.locator('.chat-bubble.chat-bubble-primary')).toContainText('hello');
  await expect(user.locator('.chat-header')).toHaveCount(0);

  const assistant = page.locator('#d .chat-start.memory-assistant').last();
  await expect(assistant.locator('.chat-bubble')).toHaveCount(0);
  await expect(assistant.locator('.chat-header')).toHaveCount(0);
  await expect(assistant.locator('.chat-image')).toHaveCount(0);
  await expect(assistant.locator('> .memory-md')).toContainText('plain markdown');

  // Copy is in the action/footer row, never the removed header.
  const copy = assistant.locator('.memory-assistant-actions .memory-copy-msg');
  await expect(copy).toHaveCount(1);
  await expect(copy).toHaveAttribute('aria-label', 'Copy message');

  // Sources are a sibling after the markdown inside the assistant turn.
  const sources = assistant.locator('[data-testid="chat-sources"]');
  await expect(sources).toHaveCount(1);
  const order = await assistant.evaluate((el) => {
    const md = el.querySelector('.memory-md');
    const src = el.querySelector('[data-testid="chat-sources"]');
    if (!md || !src) return null;
    return !!(md.compareDocumentPosition(src) & Node.DOCUMENT_POSITION_FOLLOWING);
  });
  expect(order).toBe(true);
  expect(errors).toEqual([]);
});

test('every left-side row starts at the same content edge (no avatar gutter)', async ({ page }) => {
  const errors = await load(page);
  await render(page);

  const toolRow = page.locator('#d > .memory-aux');
  const questionRow = page.locator('#d > .memory-card');
  const assistant = page.locator('#d .chat-start.memory-assistant').last();

  // Tool steps and question/approval cards are left-side rows, not agent
  // "bubbles": no avatar, no name header, no chat-grid column placement.
  await expect(toolRow).toHaveCount(1);
  await expect(toolRow.locator('[data-tool="web_search"]')).toBeVisible();
  await expect(questionRow).toHaveCount(1);
  await expect(questionRow.locator('[data-testid="chat-question-card"]')).toBeVisible();

  for (const row of [assistant, toolRow, questionRow]) {
    await expect(row.locator('.chat-image')).toHaveCount(0);
    await expect(row.locator('.chat-header')).toHaveCount(0);
  }
  for (const node of [
    toolRow.locator('.memory-tool-chip'),
    questionRow.locator('[data-testid="chat-question-card"]'),
  ]) {
    const cls = (await node.getAttribute('class')) || '';
    expect(cls).not.toMatch(/\bcol-start-|\brow-start-/);
  }

  // The alignment itself: the assistant's markdown, the tool chip and the
  // question card all begin at the same x. The daisyUI grid stand-in above is
  // what a row would fall back onto if its content were placed in the avatar
  // column, so this fails on a reintroduced gutter.
  const lefts = await page.evaluate(() => {
    const pick = (sel: string) => {
      const el = document.querySelector(sel);
      return el ? Math.round(el.getBoundingClientRect().left) : null;
    };
    return {
      assistant: pick('#d > .memory-assistant > .memory-md'),
      tool: pick('#d > .memory-aux .memory-tool-chip'),
      question: pick('#d > .memory-card > [data-testid="chat-question-card"]'),
    };
  });
  expect(lefts.assistant).not.toBeNull();
  expect(lefts.tool).not.toBeNull();
  expect(lefts.question).not.toBeNull();
  // The chip and the card carry a 1px border; allow that and sub-pixel rounding.
  expect(Math.abs((lefts.tool as number) - (lefts.assistant as number))).toBeLessThanOrEqual(2);
  expect(Math.abs((lefts.question as number) - (lefts.assistant as number))).toBeLessThanOrEqual(2);
  expect(errors).toEqual([]);
});
