import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Hermetic DOM-level spec for a RECORDED pipeline transcript (an extraction run)
// opened in the chat workspace.
//
// The extraction worker writes its stage prompts and stage answers as run
// messages that carry their provenance in the content — `stage` (who produced
// the text) and `stage_kind` (`prompt` / `output` / `error` / `header`) — and the
// gateway's run-history route forwards it. This spec pins what the transcript
// does with it, on the shipped scripts, with a stubbed API:
//
//   1. a stage's INPUT is not a "You" bubble: the operator never typed the
//      document prompt, so it renders as a collapsed, stage-labelled card;
//   2. an assistant stage message is attributed to its STAGE, and the run's own
//      agent (not the selected chat agent) heads the pane;
//   3. no label markup leaks into the text (`**EntityProcessor** · …` rendered
//      literally, and the history renderer's first-line heuristic turned it into
//      a Thinking block containing nothing but the label);
//   4. the pane URL names the run, and that URL reopens the transcript — a run is
//      not a conversation, so `?c=` cannot carry it.

const CHAT_TRANSPORT_JS = path.resolve(__dirname, '../../../../gateway/webui/static/js/chat-transport.js');
const CHAT_COMPONENTS_JS = path.resolve(__dirname, '../../../../gateway/webui/static/js/chat-components.js');
const CHAT_HOST_JS = path.resolve(__dirname, '../../../../gateway/webui/static/js/chat-host.js');
const CHAT_STREAM_JS = path.resolve(__dirname, '../../../../gateway/webui/static/js/chat-stream.js');
const CHAT_JS = path.resolve(__dirname, '../../../../gateway/webui/static/js/chat.js');

const RUN_ID = 'run-extract-1';
const RUN_AGENT = 'Extraction';
const HEADER_TEXT = 'Extraction job job-1 · document doc-1';
const PROMPT_TEXT = 'Extract ONLY these types: Person, Project\n\nThe quarterly report body follows …';

// The transcript as the recorder writes it: a run header, the stage's prompt,
// then the stage's answer. Every message carries stage provenance.
const RUN_ITEMS = [
  {
    kind: 'message',
    role: 'user',
    content: { text: HEADER_TEXT, stage: RUN_AGENT, stage_kind: 'header' },
    step_number: 1,
    created_at: '2026-11-04T09:00:00.000Z',
  },
  {
    kind: 'message',
    role: 'user',
    content: { text: PROMPT_TEXT, stage: 'EntityExtractor', stage_kind: 'prompt' },
    step_number: 2,
    created_at: '2026-11-04T09:00:01.000Z',
  },
  {
    kind: 'message',
    role: 'assistant',
    content: {
      html: '<p>Processed 55 entities with temp_ids</p>',
      stage: 'EntityProcessor',
      stage_kind: 'output',
    },
    step_number: 3,
    created_at: '2026-11-04T09:00:02.000Z',
  },
  {
    kind: 'message',
    role: 'assistant',
    content: {
      html: '<p>quota exceeded</p>',
      stage: 'EntityExtractor',
      stage_kind: 'error',
    },
    step_number: 4,
    created_at: '2026-11-04T09:00:03.000Z',
  },
];

const CHAT_SKELETON = `<!doctype html>
<html>
<head><meta charset="utf-8" /><style>.hidden { display: none !important; }</style></head>
<body>
<div id="chat-root">
  <aside id="chat-rail">
    <div id="chat-rail-resize" role="separator" aria-orientation="vertical" aria-label="Resize session list"></div>
    <select id="chat-agent-filter" aria-label="Filter sessions by agent"><option value="">All agents</option></select>
    <select id="chat-origin-filter" aria-label="Filter sessions by type"><option value="">All types</option></select>
    <div id="chat-rail-list">
      <div data-action="open-run" data-id="${RUN_ID}" data-agent="" data-origin="extraction">Extraction job</div>
    </div>
  </aside>
  <div>
    <div>
      <div id="chat-header-icon"></div>
      <div>
        <div id="chat-header-title">Tester</div>
        <div id="chat-header-desc">The selected chat agent</div>
      </div>
      <div id="chat-run-status" class="memory-run-status hidden" role="status" aria-live="polite"></div>
    </div>
    <div id="chat-log">
      <div id="chat-empty">
        <select id="chat-agent" aria-label="Agent">
          <option value="a1" data-description="The selected chat agent" data-icon="" data-color="" data-warn="">Tester</option>
        </select>
      </div>
      <div id="chat-todos" class="hidden"></div>
      <div id="chat-messages"></div>
    </div>
    <div>
      <div id="chat-queue" class="memory-queue hidden" aria-live="polite"></div>
      <form id="chat-form">
        <textarea id="chat-input" rows="1" aria-label="Message"></textarea>
        <button id="chat-send" type="submit" aria-label="Send">Send</button>
        <span id="chat-stop" class="hidden shrink-0"><button type="button" aria-label="Stop generating">Stop</button></span>
      </form>
    </div>
  </div>
</div>
</body>
</html>`;

async function installStubs(page: Page): Promise<void> {
  await page.evaluate(
    ({ runId, agentName, runItems }) => {
      const w = window as unknown as { fetch: (url: unknown) => Promise<Response>; EventSource: unknown };
      w.fetch = function (url) {
        const u = String(url);
        if (u.indexOf('/api/runs/' + runId + '/history') !== -1) {
          return Promise.resolve(
            new Response(
              JSON.stringify({
                conversation_id: runId,
                agent_name: agentName,
                items: runItems,
                pending_approvals: [],
              }),
              { status: 200, headers: { 'Content-Type': 'application/json' } },
            ),
          );
        }
        if (u.indexOf('/partial/') === 0) return Promise.resolve(new Response('', { status: 200 }));
        return Promise.resolve(new Response('[]', { status: 200 }));
      };
      function FakeEventSource(this: { close: () => void }) {
        this.close = function () {};
      }
      w.EventSource = FakeEventSource;
    },
    { runId: RUN_ID, agentName: RUN_AGENT, runItems: RUN_ITEMS },
  );
}

async function loadChatSurface(page: Page): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));
  await page.addScriptTag({ path: CHAT_TRANSPORT_JS });
  await page.addScriptTag({ path: CHAT_COMPONENTS_JS });
  await page.addScriptTag({ path: CHAT_HOST_JS });
  await page.addScriptTag({ path: CHAT_STREAM_JS });
  await page.addScriptTag({ path: CHAT_JS });
  return errors;
}

test.describe('recorded pipeline transcript in the chat workspace', () => {
  test('labels stage messages, attributes them to the run agent, and drops the label markup', async ({ page }) => {
    // A real HTTP origin is required for the URL half: about:blank's opaque
    // origin makes history.replaceState refuse the normalized /chat?run= URL.
    await page.route('http://memory.test/**', (route) =>
      route.fulfill({ contentType: 'text/html', body: CHAT_SKELETON }),
    );
    await page.goto('http://memory.test/chat');
    await installStubs(page);
    const errors = await loadChatSurface(page);

    await page.locator(`[data-action="open-run"][data-id="${RUN_ID}"]`).click();
    await expect(page.locator(`[data-action="open-run"][data-id="${RUN_ID}"]`)).toHaveAttribute('data-active', 'true');

    const messages = page.locator('#chat-messages');
    // Both stage inputs (the run header and the stage prompt) render as
    // stage-labelled cards, not "You" bubbles: the operator typed neither.
    await expect(messages.locator('.memory-stage-input')).toHaveCount(2);
    await expect(messages.locator('.chat-end')).toHaveCount(0);
    await expect(messages.locator('.memory-stage-input').first()).toContainText(RUN_AGENT);
    await expect(messages.locator('.memory-stage-input').nth(1)).toContainText('EntityExtractor');
    await expect(messages.locator('.memory-stage-input').nth(1)).toContainText('prompt');

    // The stage answer is attributed to the stage that produced it.
    const answer = messages.locator('.chat-start').filter({ hasText: 'Processed 55 entities' });
    await expect(answer.locator('.chat-header')).toHaveText('EntityProcessor');
    await expect(answer.locator('.memory-md')).toContainText('Processed 55 entities');

    // A stage failure states its kind in the bubble header — not as markup in
    // the text, and not as a synthetic Thinking block.
    const failed = messages.locator('.chat-start').filter({ hasText: 'quota exceeded' });
    await expect(failed.locator('.chat-header')).toHaveText('EntityExtractor · error');

    // No synthetic Thinking block: a stage label is provenance, never
    // chain-of-thought, and the recorded text is the answer itself.
    await expect(messages.locator('.memory-thinking')).toHaveCount(0);

    // The label never leaks into the rendered text as markdown.
    await expect(messages).not.toContainText('**');

    // The pane is headed by the RUN's agent — not the selected chat agent.
    await expect(page.locator('#chat-header-title')).toHaveText(RUN_AGENT);
    await expect(page.locator('#chat-header-desc')).toBeHidden();

    // The URL names the run (a run has no conversation id to carry).
    await expect.poll(() => page.evaluate(() => window.location.search)).toContain(`run=${RUN_ID}`);

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('a stage prompt stays inspectable whole behind its card', async ({ page }) => {
    await page.route('http://memory.test/**', (route) =>
      route.fulfill({ contentType: 'text/html', body: CHAT_SKELETON }),
    );
    await page.goto('http://memory.test/chat');
    await installStubs(page);
    const errors = await loadChatSurface(page);

    await page.locator(`[data-action="open-run"][data-id="${RUN_ID}"]`).click();

    const promptCard = page.locator('#chat-messages .memory-stage-input').nth(1);
    // Collapsed by default: the card must not dump the document into the
    // transcript, but the whole prompt is there when asked for.
    await expect(promptCard.locator('pre')).toBeHidden();
    await promptCard.locator('.memory-badge-toggle').click();
    await expect(promptCard.locator('pre')).toBeVisible();
    await expect(promptCard.locator('pre')).toHaveText(PROMPT_TEXT);

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('sending from a run starts a fresh chat owned by the selected agent', async ({ page }) => {
    // Reproduces the stale attribution a run transcript left behind: sending a
    // message abandoned the run scope and cleared the URL and bubbles, but the
    // pane header still named the run's agent — a brand-new chat titled
    // "Extraction" with its own description hidden.
    await page.route('http://memory.test/**', (route) =>
      route.fulfill({ contentType: 'text/html', body: CHAT_SKELETON }),
    );
    await page.goto('http://memory.test/chat');
    await installStubs(page);
    await loadChatSurface(page);

    await page.locator(`[data-action="open-run"][data-id="${RUN_ID}"]`).click();
    await expect(page.locator('#chat-header-title')).toHaveText(RUN_AGENT);

    await page.locator('#chat-input').fill('a fresh question');
    await page.locator('#chat-form').evaluate((f) => (f as HTMLFormElement).requestSubmit());

    await expect(page.locator('#chat-header-title')).toHaveText('Tester');
    await expect(page.locator('#chat-header-desc')).toBeVisible();
    await expect(page.locator('#chat-header-desc')).toHaveText('The selected chat agent');
    await expect.poll(() => page.evaluate(() => window.location.search)).not.toContain('run=');
  });

  test('the pane URL reopens the run transcript', async ({ page }) => {
    await page.route('http://memory.test/**', (route) =>
      route.fulfill({ contentType: 'text/html', body: CHAT_SKELETON }),
    );
    await page.goto(`http://memory.test/chat?run=${RUN_ID}`);
    await installStubs(page);
    const errors = await loadChatSurface(page);

    // No click: the deep link alone must restore the transcript.
    const messages = page.locator('#chat-messages');
    await expect(messages.locator('.memory-stage-input')).toHaveCount(2);
    await expect(page.locator('#chat-header-title')).toHaveText(RUN_AGENT);
    await expect(page.locator(`[data-action="open-run"][data-id="${RUN_ID}"]`)).toHaveAttribute('data-active', 'true');

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });
});
