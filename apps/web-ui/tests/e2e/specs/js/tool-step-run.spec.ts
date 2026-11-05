import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Hermetic DOM spec for tool runs: a turn's consecutive steps fold into one
// summary row that states the step count, the run's span (once it reaches a
// second) and any failure. The default is open while a step runs and closed
// once the run settles; the reader's open/closed choice outranks that default
// and survives a settle, which is what stops the history render that replaces
// the live DOM from snapping a run the reader opened shut again.
//
// Both render paths are covered, because both are real: `renderTimeline` paints
// a recorded turn (history, resume, side panel, share) and the live chip path
// paints a streaming one. They share one builder (`createToolRun`) and one
// summary line, so the two paths cannot describe the same run differently.
//
// The scripts are loaded verbatim into a bare page — no gateway, no memory API,
// no auth — and driven through their public entry points.

const core = (name: string) =>
  path.resolve(__dirname, '../../../../gateway/webui/static/js', name);

const T0 = '2026-09-30T10:00:00.000Z';

const SEARCH = {
  family: 'search',
  verb: 'Searched memory',
  runningVerb: 'Searching memory',
  icon: 'lucide--search',
};

const MEMORY = {
  family: 'memory',
  verb: 'Used memory',
  runningVerb: 'Using memory',
  icon: 'lucide--database',
};

interface HistoryStep {
  kind: 'tool_call';
  id: string;
  run_id: string;
  tool_name: string;
  tool_status: string;
  tool_input?: unknown;
  tool_output?: unknown;
  tool_category?: unknown;
  tool_subject?: string;
  created_at: string;
  duration_ms?: number;
}

function step(overrides: Partial<HistoryStep> & { id: string }): HistoryStep {
  return {
    kind: 'tool_call',
    run_id: 'run-1',
    tool_name: 'search_memory',
    tool_status: 'completed',
    tool_input: { query: overrides.id },
    tool_output: { status: 'ok', data: { results: [] } },
    tool_category: SEARCH,
    tool_subject: 'retrieval notes',
    created_at: T0,
    duration_ms: 100,
    ...overrides,
  };
}

async function loadCoreScripts(page: Page): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));
  await page.setContent(
    '<!doctype html><html><head><meta charset="utf-8" /></head><body><div id="d"></div></body></html>',
  );
  for (const name of ['chat-transport.js', 'chat-components.js', 'chat-host.js', 'chat-stream.js']) {
    await page.addScriptTag({ path: core(name) });
  }
  return errors;
}

// renderHistory paints a recorded turn through the shipped renderer, with the
// shipped chip builder underneath (so the run is filled by the real steps).
async function renderHistory(page: Page, items: HistoryStep[]): Promise<void> {
  await page.evaluate((list) => {
    const w = window as unknown as {
      MemoryChatStream: { createEngine: (ctx: unknown) => { toolChip: (...a: unknown[]) => HTMLElement } };
      MemoryChatComponents: { renderTimeline: (i: unknown, c: unknown, f: unknown) => void };
    };
    const messages = document.getElementById('d') as HTMLElement;
    messages.innerHTML = '';
    const badgeCtx = { messages, hideEmpty() {}, scrollToBottom() {} };
    const stream = w.MemoryChatStream.createEngine({
      messages,
      badgeCtx,
      hideEmpty() {},
      scrollToBottom() {},
      setStreaming() {},
      onStreamFinish() {},
      onStreamFail() {},
      currentAgentName: () => 'Memory',
    });
    const ctx = {
      container: messages,
      messages,
      badgeCtx,
      hideEmpty() {},
      scrollToBottom() {},
      addUserMessage() {},
      addAssistantMessage() {
        const d = document.createElement('div');
        messages.appendChild(d);
        return d;
      },
      renderHistoryQuestion() {},
      toolChip(tool: string, status: string, detail: string, payload: unknown) {
        return stream.toolChip(tool, status, detail, payload);
      },
    };
    w.MemoryChatComponents.renderTimeline(list, ctx, {});
  }, items);
}

// liveEngine builds the streaming path's engine; `runId` is what the page knows
// about the turn (a live page learns it from the refresh payload).
async function liveEngine(page: Page, runId = ''): Promise<void> {
  await page.evaluate((id) => {
    const w = window as unknown as {
      __runId: string;
      __stream: { handleToolEvent: (e: unknown) => void; finishStream: (r: string) => void };
      MemoryChatStream: { createEngine: (ctx: unknown) => unknown };
    };
    w.__runId = id;
    const messages = document.getElementById('d') as HTMLElement;
    messages.innerHTML = '';
    const badgeCtx = { messages, hideEmpty() {}, scrollToBottom() {} };
    w.__stream = w.MemoryChatStream.createEngine({
      messages,
      badgeCtx,
      hideEmpty() {},
      scrollToBottom() {},
      setStreaming() {},
      onStreamFinish() {},
      onStreamFail() {},
      currentAgentName: () => 'Memory',
      currentRunId: () => w.__runId || '',
    }) as typeof w.__stream;
  }, runId);
}

async function toolEvent(page: Page, evt: Record<string, unknown>): Promise<void> {
  await page.evaluate((e) => {
    (window as unknown as { __stream: { handleToolEvent: (e: unknown) => void } }).__stream.handleToolEvent(e);
  }, evt);
}

async function finishTurn(page: Page): Promise<void> {
  await page.evaluate(() => {
    (window as unknown as { __stream: { finishStream: (r: string) => void } }).__stream.finishStream('done');
  });
}

interface RunView {
  open: boolean;
  text: string;
  steps: number;
  failed: string | null;
}

// runs reports what a reader (and a screen reader) gets from each run row.
async function runs(page: Page): Promise<RunView[]> {
  return page.evaluate(() => {
    const out = [];
    for (const el of Array.from(document.querySelectorAll('details.memory-tool-run'))) {
      const summary = el.querySelector('summary') as HTMLElement | null;
      const failEl = el.querySelector('.memory-tool-run-failed') as HTMLElement | null;
      const failText = el.querySelector('.memory-tool-run-failed-text');
      const failed =
        failEl && !failEl.classList.contains('hidden') ? (failText?.textContent || '').trim() : null;
      out.push({
        open: (el as HTMLDetailsElement).open,
        text: (summary?.textContent || '').replace(/\s+/g, ' ').trim(),
        steps: el.querySelectorAll('.memory-tool-chip').length,
        failed,
      });
    }
    return out;
  });
}

test.describe('tool runs — recorded turn', () => {
  test('a single step renders as a step, not as a run', async ({ page }) => {
    const errors = await loadCoreScripts(page);
    await renderHistory(page, [step({ id: 'c1' })]);

    expect(await runs(page)).toHaveLength(0);
    expect(await page.locator('.memory-tool-chip').count()).toBe(1);
    // The step still sits in its own chat row, exactly as before runs existed.
    expect(await page.locator('.chat.memory-aux .memory-tool-chip').count()).toBe(1);
    expect(errors).toEqual([]);
  });

  test('a settled turn folds into one collapsed group stating its count and tools', async ({ page }) => {
    const errors = await loadCoreScripts(page);
    await renderHistory(page, [
      step({ id: 'c1', created_at: '2026-09-30T10:00:00.000Z', duration_ms: 400 }),
      step({ id: 'c2', created_at: '2026-09-30T10:00:00.400Z', duration_ms: 300 }),
      step({ id: 'c3', created_at: '2026-09-30T10:00:01.200Z', duration_ms: 400 }),
    ]);

    const [run, ...rest] = await runs(page);
    expect(rest).toHaveLength(0);
    expect(run.steps).toBe(3);
    expect(run.open).toBe(false);
    expect(run.text).toBe('3 tool calls · Search memory ×3');
    expect(run.failed).toBeNull();
    expect(errors).toEqual([]);
  });

  test('a group names up to three tools and summarises the rest', async ({ page }) => {
    await loadCoreScripts(page);
    await renderHistory(page, [
      step({ id: 'c1', tool_name: 'web_search' }),
      step({ id: 'c2', tool_name: 'web_search' }),
      step({ id: 'c3', tool_name: 'read_file' }),
      step({ id: 'c4', tool_name: 'entity-query' }),
      step({ id: 'c5', tool_name: 'memory_search' }),
    ]);

    const [run] = await runs(page);
    expect(run.text).toBe('5 tool calls · Web search ×2, Read file, Entity query, +1 more');
  });

  test('a group with a failure says so in words, beside its count', async ({ page }) => {
    await loadCoreScripts(page);
    await renderHistory(page, [
      step({ id: 'c1' }),
      step({
        id: 'c2',
        tool_status: 'failed',
        tool_output: { status: 'error', message: 'vector index unavailable' },
      }),
    ]);

    const [run] = await runs(page);
    expect(run.text).toContain('2 tool calls');
    expect(run.failed).toBe('1 failed');
    // The indicator is a word plus a decorative glyph, never colour alone.
    expect(await page.locator('.memory-tool-run-failed .iconify.lucide--alert-triangle').count()).toBe(1);
  });

  test('an awaiting-confirmation call renders standalone and splits the group', async ({ page }) => {
    await loadCoreScripts(page);
    await renderHistory(page, [
      step({ id: 'c1' }),
      step({
        id: 'c2',
        tool_status: 'awaiting_confirmation',
        tool_output: { status: 'awaiting_confirmation', question_id: 'q1', message: 'Approve?' },
        duration_ms: 0,
      }),
    ]);

    expect(await runs(page)).toHaveLength(0);
    expect(await page.locator('.memory-tool-chip').count()).toBe(2);
    expect(await page.locator('.memory-tool-chip[data-status="awaiting_confirmation"]').count()).toBe(1);
  });

  test('thinking and assistant text split a group into separate groups', async ({ page }) => {
    await loadCoreScripts(page);
    const items = [
      step({ id: 'c1' }),
      step({ id: 'c2' }),
      { kind: 'message', role: 'operator', content: { text: 'thinking', function_calls: [{ name: 'x' }] }, created_at: T0 },
      step({ id: 'c3' }),
      step({ id: 'c4' }),
      { kind: 'message', role: 'agent', content: { html: '<p>answer</p>' }, created_at: T0 },
      step({ id: 'c5' }),
      step({ id: 'c6' }),
    ];
    await renderHistory(page, items as never);

    const groups = await runs(page);
    expect(groups).toHaveLength(3);
    expect(groups.every((g) => g.steps === 2)).toBe(true);
  });

  test('the reader opening a run outranks the settled default and survives a re-render', async ({
    page,
  }) => {
    await loadCoreScripts(page);
    const items = [step({ id: 'c1' }), step({ id: 'c2' })];
    await renderHistory(page, items);
    expect((await runs(page))[0].open).toBe(false);

    await page.locator('details.memory-tool-run summary').click();
    expect((await runs(page))[0].open).toBe(true);

    // The refresh that re-paints the transcript from the same items must not
    // re-close the run: the reader's choice outranks the default.
    await renderHistory(page, items);
    expect((await runs(page))[0].open).toBe(true);

    // ... and it is held for the page, never persisted: a fresh load starts from
    // the default again.
    await loadCoreScripts(page);
    await renderHistory(page, items);
    expect((await runs(page))[0].open).toBe(false);
  });
});

test.describe('tool runs — live turn', () => {
  test('a single tool call stays today\'s plain chip throughout', async ({ page }) => {
    await loadCoreScripts(page);
    await liveEngine(page);

    await toolEvent(page, {
      tool: 'search_memory',
      status: 'running',
      id: 'call-1',
      result: { query: 'retrieval notes' },
      toolCategory: SEARCH,
      toolSubject: 'retrieval notes',
    });
    expect(await runs(page)).toHaveLength(0);
    expect(await page.locator('.memory-tool-chip').count()).toBe(1);

    await toolEvent(page, {
      tool: 'search_memory',
      status: 'completed',
      id: 'call-1',
      result: { status: 'ok', data: { results: [] } },
      toolCategory: SEARCH,
    });
    await finishTurn(page);

    expect(await runs(page)).toHaveLength(0);
    expect(await page.locator('.memory-tool-chip').count()).toBe(1);
    expect(await page.locator('.chat.memory-aux .memory-tool-chip').count()).toBe(1);
  });

  test('three steps fold into one run that closes at settle, stating the count', async ({ page }) => {
    await loadCoreScripts(page);
    await liveEngine(page);

    for (const id of ['call-1', 'call-2', 'call-3']) {
      await toolEvent(page, {
        tool: 'search_memory',
        status: 'running',
        id,
        result: { query: id },
        toolCategory: SEARCH,
        toolSubject: id,
      });
    }
    let [run] = await runs(page);
    expect(run.steps).toBe(3);
    expect(run.open).toBe(false); // groups are collapsed by default

    for (const id of ['call-1', 'call-2', 'call-3']) {
      await toolEvent(page, {
        tool: 'search_memory',
        status: 'completed',
        id,
        result: { status: 'ok', data: { results: [] } },
        toolCategory: SEARCH,
      });
    }
    await finishTurn(page);

    [run] = await runs(page);
    expect(run.steps).toBe(3);
    expect(run.open).toBe(false);
    expect(run.text).toMatch(/^3 tool calls/);
  });

  test('a reader who opens a running group keeps it open when it settles', async ({ page }) => {
    await loadCoreScripts(page);
    await liveEngine(page);

    for (const id of ['call-1', 'call-2']) {
      await toolEvent(page, {
        tool: 'search_memory',
        status: 'running',
        id,
        result: { query: id },
        toolCategory: SEARCH,
      });
    }
    expect((await runs(page))[0].open).toBe(false);

    await page.locator('details.memory-tool-run summary').click();
    expect((await runs(page))[0].open).toBe(true);

    for (const id of ['call-1', 'call-2']) {
      await toolEvent(page, {
        tool: 'search_memory',
        status: 'completed',
        id,
        result: { status: 'ok', data: { results: [] } },
        toolCategory: SEARCH,
      });
    }
    await finishTurn(page);

    expect((await runs(page))[0].open).toBe(true);
  });

  test('a reader who closes a running group keeps it closed when a step settles', async ({ page }) => {
    await loadCoreScripts(page);
    await liveEngine(page);

    for (const id of ['call-1', 'call-2']) {
      await toolEvent(page, {
        tool: 'search_memory',
        status: 'running',
        id,
        result: { query: id },
        toolCategory: SEARCH,
      });
    }
    await page.locator('details.memory-tool-run summary').click(); // opened
    await page.locator('details.memory-tool-run summary').click(); // closed again
    expect((await runs(page))[0].open).toBe(false);

    for (const id of ['call-1', 'call-2']) {
      await toolEvent(page, {
        tool: 'search_memory',
        status: 'completed',
        id,
        result: { status: 'ok', data: { results: [] } },
        toolCategory: SEARCH,
      });
    }
    await finishTurn(page);

    expect((await runs(page))[0].open).toBe(false);
  });

  test("the reader's open choice survives the recorded re-render of the same turn", async ({
    page,
  }) => {
    await loadCoreScripts(page);
    // The page learns the turn's id only once the run is under way, so the live
    // run is keyed locally and re-keyed onto the run id at settle.
    await liveEngine(page, 'run-abc');

    for (const id of ['call-1', 'call-2']) {
      await toolEvent(page, {
        tool: 'memory_search',
        status: 'running',
        id,
        result: { query: id },
        toolCategory: MEMORY,
      });
    }
    await page.locator('details.memory-tool-run summary').click(); // the reader opens it
    for (const id of ['call-1', 'call-2']) {
      await toolEvent(page, {
        tool: 'memory_search',
        status: 'completed',
        id,
        result: { status: 'ok', data: { records: [] } },
        toolCategory: MEMORY,
      });
    }
    await finishTurn(page);
    expect((await runs(page))[0].open).toBe(true);

    // The turn's recorded transcript is the same turn under its run id.
    await renderHistory(page, [
      step({ id: 'c1', run_id: 'run-abc', tool_name: 'memory_search', tool_category: MEMORY }),
      step({ id: 'c2', run_id: 'run-abc', tool_name: 'memory_search', tool_category: MEMORY }),
    ]);
    const [recorded] = await runs(page);
    expect(recorded.steps).toBe(2);
    expect(recorded.open).toBe(true);
  });

  test('a closed choice made before the page learns the run id still survives the recorded re-render', async ({
    page,
  }) => {
    await loadCoreScripts(page);
    // The real /chat path: the run id arrives with the refresh payload, which can
    // land after the reader has already opened or closed the run. The choice is
    // recorded under the live key and re-keyed onto the run id at settle.
    await liveEngine(page, '');

    for (const id of ['call-1', 'call-2']) {
      await toolEvent(page, {
        tool: 'memory_search',
        status: 'running',
        id,
        result: { query: id },
        toolCategory: MEMORY,
      });
    }
    await page.locator('details.memory-tool-run summary').click(); // opened
    await page.locator('details.memory-tool-run summary').click(); // closed
    expect((await runs(page))[0].open).toBe(false);

    // The turn's id becomes known only now, before the settle.
    await page.evaluate(() => {
      (window as unknown as { __runId: string }).__runId = 'run-late';
    });
    for (const id of ['call-1', 'call-2']) {
      await toolEvent(page, {
        tool: 'memory_search',
        status: 'completed',
        id,
        result: { status: 'ok', data: { records: [] } },
        toolCategory: MEMORY,
      });
    }
    await finishTurn(page);

    await renderHistory(page, [
      step({ id: 'c1', run_id: 'run-late', tool_name: 'memory_search', tool_category: MEMORY }),
      step({ id: 'c2', run_id: 'run-late', tool_name: 'memory_search', tool_category: MEMORY }),
    ]);
    // Closed, not the group default: this only holds if the choice was re-keyed.
    expect((await runs(page))[0].open).toBe(false);
  });

  test('a live group keeps call-id correlation and expands to its child chips', async ({ page }) => {
    await loadCoreScripts(page);
    await liveEngine(page);

    await toolEvent(page, {
      tool: 'search_memory',
      status: 'running',
      id: 'call-1',
      result: { query: 'one' },
      toolCategory: SEARCH,
    });
    await toolEvent(page, {
      tool: 'memory_search',
      status: 'running',
      id: 'call-2',
      result: { query: 'two' },
      toolCategory: MEMORY,
    });

    const group = page.locator('details.memory-tool-run');
    await expect(group).toHaveAttribute('data-tool-group', '1');
    await expect(group).toHaveAttribute('data-tool-count', '2');
    // Promotion removes the temporary single-call rows: one group row remains.
    expect(await page.locator('#d > .chat.memory-aux').count()).toBe(1);

    await group.locator('summary').click();
    const children = group.locator('.memory-tool-run-body .memory-tool-chip');
    await expect(children).toHaveCount(2);
    await expect(group.locator('[data-call-id="call-1"]')).toHaveCount(1);
    await expect(group.locator('[data-call-id="call-2"]')).toHaveCount(1);

    // A terminal frame updates only the call it names, inside the group.
    await toolEvent(page, {
      tool: 'memory_search',
      status: 'failed',
      id: 'call-2',
      error: 'index unavailable',
      toolCategory: MEMORY,
    });
    await expect(group.locator('[data-call-id="call-2"]')).toHaveAttribute('data-status', 'error');
    await expect(group.locator('[data-call-id="call-1"]')).toHaveAttribute('data-status', 'running');
    await expect(group).toContainText('1 failed');
  });


  test('a dispatched mcp_tool frame does not break the live group', async ({ page }) => {
    await loadCoreScripts(page);
    const groups = await page.evaluate(() => {
      const messages = document.getElementById('d') as HTMLElement;
      messages.innerHTML = '';
      const badgeCtx = { messages, hideEmpty() {}, scrollToBottom() {} };
      const stream = (
        window as unknown as {
          MemoryChatStream: { createEngine: (ctx: unknown) => { handleToolEvent: (e: unknown) => void; breakToolGroup: () => void } };
        }
      ).MemoryChatStream.createEngine({
        messages,
        badgeCtx,
        hideEmpty() {},
        scrollToBottom() {},
        setStreaming() {},
        onStreamFinish() {},
        onStreamFail() {},
        currentAgentName: () => 'Memory',
      });
      const dispatch = (
        window as unknown as {
          MemoryChatHost: { makeHandleEvent: (h: unknown) => (raw: string) => void };
        }
      ).MemoryChatHost.makeHandleEvent({
        getStreaming: () => true,
        setBubbleHTML() {},
        updateBubbleText() {},
        scrollToBottom() {},
        handleToolEvent: (evt: unknown) => stream.handleToolEvent(evt),
        renderQuestion() {},
        renderApproval() {},
        failStream() {},
        onMeta() {},
        // Mirrors chat.js activityBoundary after the mcp_tool fix.
        onActivity: (type: string) => {
          if (type !== 'tool' && type !== 'mcp_tool' && type !== 'thinking') stream.breakToolGroup();
        },
      });
      for (const id of ['call-1', 'call-2']) {
        dispatch(JSON.stringify({
          type: 'mcp_tool',
          tool: 'search_memory',
          status: 'running',
          id,
          result: { query: id },
        }));
      }
      return messages.querySelectorAll('details.memory-tool-run[data-tool-group="1"]').length;
    });
    expect(groups).toBe(1);
  });

  test('a live awaiting-confirmation call leaves its group and renders standalone', async ({ page }) => {
    await loadCoreScripts(page);
    await liveEngine(page);

    await toolEvent(page, {
      tool: 'search_memory',
      status: 'running',
      id: 'call-1',
      result: { query: 'one' },
      toolCategory: SEARCH,
    });
    await toolEvent(page, {
      tool: 'memory_search',
      status: 'running',
      id: 'call-2',
      result: { query: 'two' },
      toolCategory: MEMORY,
    });
    expect(await runs(page)).toHaveLength(1);

    await toolEvent(page, {
      tool: 'memory_search',
      status: 'awaiting_confirmation',
      id: 'call-2',
      toolCategory: MEMORY,
    });

    // The paused call is standalone; the split leaves no group behind.
    expect(await runs(page)).toHaveLength(0);
    expect(await page.locator('.memory-tool-chip').count()).toBe(2);
    await expect(page.locator('[data-call-id="call-2"]')).toHaveAttribute(
      'data-status',
      'awaiting_confirmation',
    );
  });

  test('the reader opening a settled run keeps it open across the recorded re-render', async ({
    page,
  }) => {
    await loadCoreScripts(page);
    await liveEngine(page, 'run-xyz');

    for (const id of ['call-1', 'call-2']) {
      await toolEvent(page, {
        tool: 'memory_search',
        status: 'running',
        id,
        result: { query: id },
        toolCategory: MEMORY,
      });
    }
    for (const id of ['call-1', 'call-2']) {
      await toolEvent(page, {
        tool: 'memory_search',
        status: 'completed',
        id,
        result: { status: 'ok', data: { records: [] } },
        toolCategory: MEMORY,
      });
    }
    await finishTurn(page);
    expect((await runs(page))[0].open).toBe(false);

    await page.locator('details.memory-tool-run summary').click(); // the reader opens it
    await renderHistory(page, [
      step({ id: 'c1', run_id: 'run-xyz', tool_name: 'memory_search', tool_category: MEMORY }),
      step({ id: 'c2', run_id: 'run-xyz', tool_name: 'memory_search', tool_category: MEMORY }),
    ]);
    expect((await runs(page))[0].open).toBe(true);
  });
});

test.describe('tool run summary — accessibility', () => {
  // The app's root font is 15px, which makes the min-h-11 utility 41.25px; the
  // control's height is therefore a rule in the badge stylesheet, like the raw
  // payload disclosure's.
  test('the summary is a 44 CSS pixel control whose state is text', async ({ page }) => {
    await loadCoreScripts(page);
    await renderHistory(page, [
      step({ id: 'c1' }),
      step({ id: 'c2', tool_status: 'failed', tool_output: { status: 'error', message: 'nope' } }),
    ]);

    const summary = page.locator('details.memory-tool-run summary');
    expect(await summary.evaluate((el) => getComputedStyle(el).minHeight)).toBe('44px');
    // Closed runs say what they hold, including the failure, as words.
    const text = (await summary.textContent())?.replace(/\s+/g, ' ').trim();
    expect(text).toContain('2 tool calls');
    expect(text).toContain('Search memory ×2');
    expect(text).toContain('1 failed');
    // The chevron is decorative; the count and the failure are the state.
    expect(await page.locator('.memory-tool-run-chevron [aria-hidden="true"]').count()).toBe(1);
  });

  test('the chevron turns over when open, without motion when that is asked for', async ({ page }) => {
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await loadCoreScripts(page);
    await renderHistory(page, [step({ id: 'c1' }), step({ id: 'c2' })]);

    const chevron = page.locator('details.memory-tool-run summary .memory-tool-run-chevron');
    expect(await chevron.evaluate((el) => getComputedStyle(el).transitionDuration)).toBe('0s');

    await page.locator('details.memory-tool-run summary').click();
    // Open state is carried by geometry as well as by the steps being visible.
    expect(await chevron.evaluate((el) => getComputedStyle(el).transform)).not.toBe('none');
  });
});
