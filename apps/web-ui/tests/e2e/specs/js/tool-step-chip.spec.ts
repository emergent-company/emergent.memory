import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Hermetic DOM-level spec for the tool step chip: how a call names itself, what
// an opened step leads with, and where the raw payload lives.
//
// These are the rules the gateway derives (toolresult) and the chip renders. The
// spec loads the shipped chat-components.js into a blank page and drives its
// public entry points with the exact view model the server sends, so the wire
// contract and the renderer are tested against each other without a server.

const CHAT_COMPONENTS_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/chat-components.js',
);
const CHAT_STREAM_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/chat-stream.js',
);

interface Step {
  tool?: string;
  category?: { family: string; verb: string; runningVerb: string; icon: string };
  subject?: string;
  view?: Record<string, unknown>;
  output?: unknown;
}

const MEMORY_CATEGORY = {
  family: 'memory',
  verb: 'Used memory',
  runningVerb: 'Using memory',
  icon: 'lucide--database',
};

const SEARCH_CATEGORY = {
  family: 'search',
  verb: 'Searched memory',
  runningVerb: 'Searching memory',
  icon: 'lucide--search',
};

const RECORDS_VIEW = {
  kind: 'records',
  outcome: '2 records',
  items: [
    { title: 'Alice', detail: 'Engineer' },
    { title: 'Retrieval notes', detail: 'How answers are found', link: 'https://example.com/notes/retrieval' },
  ],
  moreRecords: 2,
  raw: '{\n  "ok": true,\n  "data": {"total": 4}\n}',
  rawMoreLines: 3,
  rawMoreCharacters: 120,
};

async function bootstrap(page: Page): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));
  await page.setContent('<!doctype html><html><body><div id="d"></div></body></html>');
  await page.addScriptTag({ path: CHAT_COMPONENTS_JS });
  await page.addScriptTag({ path: CHAT_STREAM_JS });
  return errors;
}

// renderStep renders an opened step's detail area and returns a summary of what
// the reader can see: the body text, whether the raw payload is visible, and the
// disclosure's own state.
async function renderStep(page: Page, step: Step) {
  return page.evaluate((payload) => {
    const w = window as unknown as {
      MemoryChatComponents: { renderToolDetails: (el: Element, p: unknown) => void };
    };
    const el = document.getElementById('d');
    if (!el) throw new Error('missing #d mount');
    w.MemoryChatComponents.renderToolDetails(el, payload);

    const details = el.querySelector('details.memory-raw-disclosure') as HTMLDetailsElement | null;
    const pre = details ? details.querySelector('pre') : null;
    const summary = details ? details.querySelector('summary') : null;
    return {
      text: el.textContent || '',
      details: !!details,
      open: details ? details.open : null,
      rawText: pre ? pre.textContent || '' : '',
      summaryHeight: summary ? (summary as HTMLElement).getBoundingClientRect().height : 0,
      sections: Array.from(el.querySelectorAll('section p:first-child')).map((p) => p.textContent),
      items: Array.from(el.querySelectorAll('section li')).map((li) => li.textContent || ''),
      fields: Array.from(el.querySelectorAll('section dt')).map((dt) => dt.textContent || ''),
    };
  }, step);
}

test.describe('tool step chip', () => {
  test('names the call by category and subject, not by wire name', async ({ page }) => {
    const errors = await bootstrap(page);
    const label = await page.evaluate((category) => {
      const w = window as unknown as {
        MemoryChatComponents: {
          toolLabel: (c: unknown, name: string, running: boolean, subject: string) => string;
          toolIcon: (c: unknown) => string;
        };
      };
      return {
        settled: w.MemoryChatComponents.toolLabel(category, 'search-hybrid', false, 'swiftui'),
        running: w.MemoryChatComponents.toolLabel(category, 'search-hybrid', true, 'swiftui'),
        noSubject: w.MemoryChatComponents.toolLabel(category, 'search-hybrid', false, ''),
        fallback: w.MemoryChatComponents.toolLabel(null, 'search-hybrid', false, ''),
        unknown: w.MemoryChatComponents.toolLabel(
          { verb: 'Used a tool', runningVerb: 'Using a tool', icon: 'lucide--wrench' },
          'frobnicate',
          false,
          '',
        ),
        icon: w.MemoryChatComponents.toolIcon(category),
      };
    }, SEARCH_CATEGORY);

    expect(label.settled).toBe('Searched memory · swiftui');
    expect(label.running).toBe('Searching memory · swiftui');
    expect(label.noSubject).toBe('Searched memory');
    expect(label.fallback).toBe('Search hybrid');
    expect(label.unknown).toBe('Used a tool');
    expect(label.icon).toBe('lucide--search');
    expect(errors).toEqual([]);
  });

  // The defect this feature exists to fix: an envelope result used to produce no
  // summary at all, so the chip fell back to raw JSON.
  test('leads with the records an envelope result held', async ({ page }) => {
    await bootstrap(page);
    const step = await renderStep(page, {
      tool: 'search-hybrid',
      category: SEARCH_CATEGORY,
      subject: 'swiftui',
      view: RECORDS_VIEW,
    });

    expect(step.items).toHaveLength(2);
    expect(step.items[0]).toContain('Alice');
    expect(step.items[0]).toContain('Engineer');
    // A record carrying an http(s) link shows the host, not the whole URL.
    expect(step.items[1]).toContain('example.com');
    expect(step.items[1]).not.toContain('https://example.com/notes/retrieval');
    // Every cap states what it hid.
    expect(step.text).toContain('2 more records');
    expect(step.text).toContain('3 more lines');
    expect(step.text).toContain('120 characters cut');
  });

  test('keeps the raw payload closed until asked for', async ({ page }) => {
    await bootstrap(page);
    const step = await renderStep(page, {
      tool: 'search-hybrid',
      category: SEARCH_CATEGORY,
      view: RECORDS_VIEW,
    });

    expect(step.details).toBe(true);
    expect(step.open).toBe(false);
    // Visibility, not presence: a closed <details> keeps its content in the DOM.
    await expect(page.locator('details.memory-raw-disclosure pre')).toBeHidden();
    // The wire name is reachable, and only inside the disclosure.
    expect(step.rawText).toBe(RECORDS_VIEW.raw);
    expect(step.text).toContain('search-hybrid');
    // 44 CSS pixels of tappable height — asserted by measurement, not by class
    // name: the app's root font makes a spacing utility short of it.
    expect(step.summaryHeight).toBeGreaterThanOrEqual(44);

    // Opened by the reader, the payload is visible.
    await page.click('details.memory-raw-disclosure > summary');
    await expect(page.locator('details.memory-raw-disclosure pre')).toBeVisible();
  });

  test('reads a record of fields as label and value', async ({ page }) => {
    await bootstrap(page);
    const step = await renderStep(page, {
      tool: 'session-get',
      view: {
        kind: 'fields',
        fields: [
          { label: 'id', value: 's1' },
          { label: 'status', value: 'completed' },
        ],
        moreFields: 3,
        raw: '{"id":"s1","status":"completed"}',
      },
    });

    expect(step.fields).toEqual(['id', 'status']);
    expect(step.text).toContain('completed');
    expect(step.text).toContain('3 more fields');
  });

  test('shows a body of text with its counted remainder', async ({ page }) => {
    await bootstrap(page);
    const step = await renderStep(page, {
      tool: 'document-get',
      view: {
        kind: 'text',
        text: 'A line of the retrieved document.',
        moreLines: 12,
        moreCharacters: 4,
        raw: '{"content":"…"}',
      },
    });

    expect(step.text).toContain('A line of the retrieved document.');
    expect(step.text).toContain('12 more lines');
    expect(step.text).toContain('4 characters cut');
  });

  test('shows the payload directly when there is no readable shape', async ({ page }) => {
    await bootstrap(page);
    const step = await renderStep(page, {
      tool: 'entity-get',
      view: {
        kind: '',
        raw: '{\n  "object": {\n    "a": 1\n  }\n}',
      },
    });

    // The payload text *is* the result, so it is visible without a second tap —
    // asserted by visibility, since a closed disclosure keeps its text in the DOM.
    expect(step.details).toBe(false);
    await expect(page.locator('#d pre')).toBeVisible();
    await expect(page.locator('#d pre')).toHaveText('{\n  "object": {\n    "a": 1\n  }\n}');
    // No empty body section above it.
    expect(step.sections.filter((s) => s === 'Result')).toHaveLength(1);
  });

  test('renders a chip with no view without throwing', async ({ page }) => {
    const errors = await bootstrap(page);
    // An older server, or a non-tool item: the chip still renders, in the older
    // wire-name language.
    const step = await renderStep(page, {
      tool: 'search-hybrid',
      output: { message: 'hello' },
    });
    expect(step.text).toContain('hello');
    expect(errors).toEqual([]);
  });
  /* ---------- the live path (an mcp_tool frame, not a replayed item) ---------- */

  // handleToolEvent is where a live step is created and settled. Nothing above
  // exercises it, which is how a settle-time crash shipped green: the chip was
  // looked up by id, then asks the *element* for a property it never had.
  async function liveStream(page: Page) {
    await page.evaluate(() => {
      const stream = document.createElement('div');
      stream.id = 'stream';
      document.body.appendChild(stream);
      const w = window as unknown as {
        MemoryChatStream: { createEngine: (ctx: unknown) => { handleToolEvent: (e: unknown) => void } };
      };
      const engine = w.MemoryChatStream.createEngine({
        messages: stream,
        badgeCtx: { messages: stream },
        scrollToBottom: () => {},
        hideEmpty: () => {},
        setStreaming: () => {},
        onStreamFail: () => {},
        currentAgentName: () => 'Agent',
        currentAgentUI: () => ({}),
      });
      (window as unknown as { __engine: unknown }).__engine = engine;
    });
  }

  async function pushFrame(page: Page, frame: Record<string, unknown>) {
    return page.evaluate((f) => {
      const w = window as unknown as {
        __engine: { handleToolEvent: (e: unknown) => void };
      };
      w.__engine.handleToolEvent(f);
      const chips = Array.from(document.querySelectorAll('.memory-tool-chip')) as HTMLElement[];
      const chip = chips[chips.length - 1];
      if (!chip) return null;
      const label = chip.querySelector('.memory-badge-label');
      const secondary = chip.querySelector('.memory-badge-secondary');
      const detail = chip.querySelector('.memory-badge-detail');
      const toggle = chip.querySelector('.memory-badge-toggle') as HTMLElement | null;
      if (toggle) toggle.click(); // open it, so the detail is rendered
      return {
        count: chips.length,
        label: label ? label.textContent : '',
        status: chip.getAttribute('data-status'),
        live: chip.classList.contains('memory-badge-live'),
        secondary:
          secondary && !secondary.classList.contains('hidden') ? (secondary.textContent || '') : '',
        detail: detail ? detail.textContent || '' : '',
      };
    }, frame);
  }

  test('settles a running step in place, keeping its subject', async ({ page }) => {
    const errors = await bootstrap(page);
    await liveStream(page);

    // A running frame carries the call's arguments in `result` — a subject, no view.
    const running = await pushFrame(page, {
      tool: 'search-hybrid',
      id: 'call-1',
      status: 'running',
      result: { query: 'swiftui' },
      toolCategory: SEARCH_CATEGORY,
      toolSubject: 'swiftui',
    });
    expect(running?.label).toBe('Searching memory · swiftui');
    expect(running?.status).toBe('running');
    expect(running?.live).toBe(true);

    // The terminal frame carries the result, so it has the view — and no subject.
    const settled = await pushFrame(page, {
      tool: 'search-hybrid',
      id: 'call-1',
      status: 'completed',
      result: { ok: true, data: { data: RECORDS_VIEW.items, total: 4 } },
      toolCategory: SEARCH_CATEGORY,
      toolView: RECORDS_VIEW,
    });
    expect(errors).toEqual([]);
    expect(settled?.count).toBe(1); // updated in place, not duplicated
    expect(settled?.status).toBe('ok');
    expect(settled?.live).toBe(false);
    expect(settled?.label).toBe('Searched memory · swiftui'); // subject kept
    expect(settled?.secondary).toBe('2 records');
    expect(settled?.detail).toContain('Alice');
  });

  // The real wire for a paused call: a running frame names it, then the policy
  // check pauses the run and the terminal frame carries the executor's sentinel
  // (the gateway sends no subject for it — the sentinel names nothing).
  const PAUSE_SENTINEL = {
    status: 'awaiting_confirmation',
    question_id: 'q1',
    message: 'Tool execution paused, awaiting user confirmation.',
  };

  test('keeps a step waiting for approval pending, with no result body', async ({ page }) => {
    const errors = await bootstrap(page);
    await liveStream(page);

    await pushFrame(page, {
      tool: 'entity-create',
      id: 'call-2',
      status: 'running',
      result: { title: 'Note', type: 'note' }, // the arguments, while it runs
      toolCategory: MEMORY_CATEGORY,
      toolSubject: 'Note',
    });

    const paused = await pushFrame(page, {
      tool: 'entity-create',
      id: 'call-2',
      status: 'awaiting_confirmation',
      result: PAUSE_SENTINEL, // not the arguments, and not a result
      toolCategory: MEMORY_CATEGORY,
      // no toolSubject: the gateway does not name a call from its sentinel
    });

    expect(errors).toEqual([]);
    expect(paused?.count).toBe(1);
    expect(paused?.status).toBe('awaiting_confirmation');
    expect(paused?.label).toBe('Using memory · Note'); // the running subject survives
    expect(paused?.secondary).toBe('Waiting for confirmation');
    expect(paused?.detail).not.toContain('field');
    expect(paused?.detail).not.toContain('Tool execution paused');
    expect(paused?.detail).toContain('Waiting for confirmation');
  });

  // A recorded item is persisted with status "completed" and the sentinel as its
  // output, so the state has to come from the payload, not the status.
  test('reads a recorded paused step from its payload, not its status', async ({ page }) => {
    await bootstrap(page);
    const paused = await page.evaluate((sentinel) => {
      const w = window as unknown as {
        MemoryChatStream: { classifyTool: (s: string, o: unknown, v: unknown) => Record<string, unknown> };
      };
      return w.MemoryChatStream.classifyTool('completed', sentinel, null);
    }, PAUSE_SENTINEL);

    expect(paused.status).toBe('awaiting_confirmation');
    expect(paused.line).toBe('');
    expect(paused.summary).toBe('');
  });

  test('bounds a failed step line but keeps the message in the opened step', async ({ page }) => {
    await bootstrap(page);
    await liveStream(page);

    const long = 'upstream refused the call; '.repeat(30);
    const failed = await pushFrame(page, {
      tool: 'search-hybrid',
      id: 'call-3',
      status: 'failed',
      result: { ok: false, error: long },
      toolCategory: SEARCH_CATEGORY,
      toolView: { kind: 'text', error: long, outcome: long.slice(0, 119) + '…', raw: '{}' },
    });

    // One line of at most 120 characters, ellipsis included.
    expect(failed?.status).toBe('error');
    expect(failed?.secondary.length).toBeLessThanOrEqual(121);
    expect(failed?.secondary).not.toContain('\n');
    expect(failed?.secondary.endsWith('…')).toBe(true);
    // The full message is still there once the step is opened.
    expect(failed?.detail).toContain('upstream refused the call');
    expect((failed?.detail || '').length).toBeGreaterThan(121);
  });
});
