import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Chat transcript day dividers + session-start formatting.
//
// The shared renderer (chat-components.js) groups a transcript by the reader's
// LOCAL calendar day — one quiet "Today" / "Yesterday" / "Wed, 12 Nov" divider
// before the first row of each day — and the /chat page seeds the session-start
// day so its "Session started" line is not repeated as a divider. This spec
// drives the shipped scripts in a real Chromium engine; no gateway, no auth.
//
// Run: npx playwright test --config=js-dom.config.ts

const CHAT_TRANSPORT_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/chat-transport.js',
);
const CHAT_COMPONENTS_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/chat-components.js',
);
const CHAT_HOST_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/chat-host.js',
);
const CHAT_STREAM_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/chat-stream.js',
);

const BARE_SKELETON = `<!doctype html><html><head><meta charset="utf-8" /></head><body></body></html>`;

// Midday UTC timestamps three local days apart (safe for the usual timezone
// offsets), so the test never depends on the runner's zone.
const DAY_A = '2026-09-30T12:00:00.000Z';
const DAY_A_LATER = '2026-09-30T15:00:00.000Z';
const DAY_B = '2026-10-01T12:00:00.000Z';
const DAY_C = '2026-10-02T12:00:00.000Z';

type HistoryItem = Record<string, unknown>;

const ITEMS: HistoryItem[] = [
  { kind: 'message', role: 'user', content: { text: 'day A first' }, created_at: DAY_A },
  { kind: 'message', role: 'agent', content: { html: '<p>day A reply</p>' }, created_at: DAY_A_LATER },
  { kind: 'tool_call', tool_name: 'entity-query', tool_status: 'ok', created_at: DAY_B },
  { kind: 'message', role: 'user', content: { text: 'day B' }, created_at: DAY_B },
  { kind: 'message', role: 'agent', content: { html: '<p>day B reply</p>' }, created_at: DAY_B },
  { kind: 'message', role: 'user', content: { text: 'day C' }, created_at: DAY_C },
];

async function loadScripts(page: Page): Promise<void> {
  await page.setContent(BARE_SKELETON);
  await page.addScriptTag({ path: CHAT_TRANSPORT_JS });
  await page.addScriptTag({ path: CHAT_COMPONENTS_JS });
  await page.addScriptTag({ path: CHAT_HOST_JS });
  await page.addScriptTag({ path: CHAT_STREAM_JS });
}

// renderItems paints the timeline into a fresh container and returns the day
// dividers in document order, plus the rendered row classes for order checks.
async function renderItems(
  page: Page,
  items: HistoryItem[],
  flags: Record<string, unknown>,
): Promise<{ days: string[]; labels: string[]; order: string[] }> {
  return page.evaluate(
    ({ items, flags }) => {
      const c = document.createElement('div');
      document.body.appendChild(c);
      const append = (cls: string, text: string) => {
        const d = document.createElement('div');
        d.className = cls;
        d.textContent = text;
        c.appendChild(d);
      };
      const ctx = {
        container: c,
        badgeCtx: { messages: c, hideEmpty() {}, scrollToBottom() {} },
        getAgentName: () => 'Memory',
        begin() { c.innerHTML = ''; },
        addUserMessage(text: string) { append('u', text); },
        addAssistantMessage(html: string) {
          const d = document.createElement('div');
          d.className = 'a';
          d.innerHTML = html;
          c.appendChild(d);
          return d;
        },
        toolChip(tool: string) { append('tool', tool); },
        renderHistoryQuestion() {},
        renderThinkingBlock() { return null; },
        isThinkingItem() { return false; },
      };
      (window as any).MemoryChatComponents.renderTimeline(items, ctx, flags);
      const dividers = Array.from(c.querySelectorAll('.memory-day-divider'));
      return {
        days: dividers.map((d) => d.getAttribute('data-day') || ''),
        labels: dividers.map((d) => (d.textContent || '').trim()),
        order: Array.from(c.children).map((el) => el.className.split(' ')[0]),
      };
    },
    { items, flags },
  );
}

// localDayKey computes the browser-local day key from an ISO instant, matching
// the renderer's own derivation so the assertion is timezone-independent.
async function localDayKey(page: Page, iso: string): Promise<string> {
  return page.evaluate((iso) => {
    const d = new Date(iso);
    const pad = (n: number) => (n < 10 ? '0' + n : String(n));
    return d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate());
  }, iso);
}

test.describe('chat transcript day dividers (chat-components.js)', () => {
  test('one divider per local day, in chronological order', async ({ page }) => {
    await loadScripts(page);
    const out = await renderItems(page, ITEMS, { silent: false });

    expect(out.days).toEqual([
      await localDayKey(page, DAY_A),
      await localDayKey(page, DAY_B),
      await localDayKey(page, DAY_C),
    ]);
    // Every divider is a separator with a visible, non-empty label.
    expect(out.labels.every((l) => l.length > 0)).toBe(true);

    // Each divider precedes a rendered row on that day (never trails the
    // transcript, never sits mid-day).
    expect(out.order[0]).toBe('memory-day-divider');
    expect(out.order).toContain('memory-day-divider');
    expect(out.order.filter((c) => c === 'memory-day-divider').length).toBe(3);
  });

  test('same-day rows share one divider', async ({ page }) => {
    await loadScripts(page);
    const out = await renderItems(page, [ITEMS[0], ITEMS[1]], { silent: false });
    expect(out.days).toEqual([await localDayKey(page, DAY_A)]);
  });

  test('startedAt suppresses the start-day divider (session-start line already dates it)', async ({ page }) => {
    await loadScripts(page);
    const out = await renderItems(page, ITEMS, { silent: false, startedAt: DAY_A });
    expect(out.days).toEqual([
      await localDayKey(page, DAY_B),
      await localDayKey(page, DAY_C),
    ]);
  });

  test('an item that paints nothing does not announce its day', async ({ page }) => {
    await loadScripts(page);
    const items: HistoryItem[] = [
      ITEMS[0],
      // Tool-result rows are hidden (the chip above already shows the call).
      { kind: 'message', role: 'tool', content: { text: 'result' }, created_at: DAY_B },
      { kind: 'message', role: 'user', content: { text: 'day C' }, created_at: DAY_C },
    ];
    const out = await renderItems(page, items, { silent: false });
    expect(out.days).toEqual([
      await localDayKey(page, DAY_A),
      await localDayKey(page, DAY_C),
    ]);
  });

  test('formatSessionStart renders a local date + time', async ({ page }) => {
    await loadScripts(page);
    const label = await page.evaluate(() => {
      const C = (window as any).MemoryChatComponents;
      return {
        real: C.formatSessionStart('2026-11-16T09:05:00.000Z'),
        empty: C.formatSessionStart(''),
        bad: C.formatSessionStart('not-a-date'),
      };
    });
    expect(label.real).toContain('2026');
    expect(label.real).toContain('·');
    expect(label.empty).toBe('');
    expect(label.bad).toBe('');
  });

  test('maybeDayDivider dedupes per container and can be reset', async ({ page }) => {
    await loadScripts(page);
    const res = await page.evaluate(
      ({ a, b }) => {
        const C = (window as any).MemoryChatComponents;
        const c = document.createElement('div');
        document.body.appendChild(c);
        const first = C.maybeDayDivider(c, a);
        const same = C.maybeDayDivider(c, a);
        const next = C.maybeDayDivider(c, b);
        C.resetDayDivider(c);
        const afterReset = C.maybeDayDivider(c, a);
        return {
          first: !!first,
          same: !!same,
          next: !!next,
          afterReset: !!afterReset,
          count: c.querySelectorAll('.memory-day-divider').length,
        };
      },
      { a: DAY_A, b: DAY_B },
    );
    expect(res).toEqual({ first: true, same: false, next: true, afterReset: true, count: 3 });
  });
});
