import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Hermetic wiring spec for spawned sub-chats in the session rail. The
// shipped chat.js is loaded verbatim into a bare page; the rail markup mirrors
// what chat.templ emits for a group (parent toggle + nested rows).

const CHAT_TRANSPORT_JS = path.resolve(__dirname, '../../../../gateway/webui/static/js/chat-transport.js');
const CHAT_COMPONENTS_JS = path.resolve(__dirname, '../../../../gateway/webui/static/js/chat-components.js');
const CHAT_HOST_JS = path.resolve(__dirname, '../../../../gateway/webui/static/js/chat-host.js');
const CHAT_STREAM_JS = path.resolve(__dirname, '../../../../gateway/webui/static/js/chat-stream.js');
const CHAT_JS = path.resolve(__dirname, '../../../../gateway/webui/static/js/chat.js');

function parentRow(expanded: boolean): string {
  return `<li class="list-row" data-action="resume-session" data-id="parent" data-agent="a1" data-origin="manual" data-testid="session-row" data-subchat-count="2" data-subchat-group="parent"${expanded ? ' data-subchats-expanded="true"' : ''}>
  <div class="list-col-grow"><div class="font-medium text-sm">Parent</div></div>
  <button type="button" data-action="toggle-subchats" data-subchat-group="parent" data-testid="session-subchats-toggle" aria-expanded="${expanded ? 'true' : 'false'}" aria-label="${expanded ? 'Hide' : 'Show'} 2 sub-chats"><span class="tabular-nums">2</span><svg class="subchat-chevron"></svg></button>
  <button type="button" data-action="delete-session" data-id="parent" data-title="Parent" data-testid="session-delete">Delete</button>
</li>`;
}

function childRow(id: string, title: string, hidden: boolean, active = false): string {
  return `<li class="list-row session-subchat${hidden ? ' hidden' : ''}" data-action="resume-session" data-id="${id}" data-agent="internal" data-origin="manual" data-testid="session-row" data-parent-id="parent"${active ? ' data-active="true"' : ''}>
  <div class="list-col-grow"><div class="font-medium text-sm">${title}</div></div>
</li>`;
}

function rail(expanded = false, activeChild = ''): string {
  return (
    parentRow(expanded) +
    childRow('child-a', 'Child A', !expanded) +
    childRow('child-b', 'Child B', !expanded, activeChild === 'child-b')
  );
}

function skeleton(railHTML: string): string {
  return `<!doctype html>
<html><head><meta charset="utf-8" /><style>.hidden { display: none !important; }</style></head>
<body>
<div id="chat-root" data-conv="">
  <aside id="chat-rail">
    <div id="chat-rail-resize" role="separator" aria-orientation="vertical" aria-label="Resize session list"></div>
    <div><button type="button" data-action="toggle-session-select" data-testid="toggle-session-select" aria-pressed="false">Select</button></div>
    <select id="chat-agent-filter" aria-label="Filter sessions by agent">
      <option value="">All agents</option>
      <option value="a1">Tester</option>
      <option value="internal">Internal</option>
      <option value="other">Other</option>
    </select>
    <select id="chat-origin-filter" aria-label="Filter sessions by type"><option value="">All types</option><option value="manual">Manual</option></select>
    <label><input id="chat-include-archived" type="checkbox" aria-label="Include archived" /> Include archived</label>
    <div id="chat-rail-list" data-testid="session-list">${railHTML}</div>
    <div id="chat-bulk-bar" class="hidden" role="group" aria-label="Bulk session actions">
      <input id="chat-select-all" data-action="select-all-sessions" type="checkbox" aria-label="Select all sessions" />
      <span id="chat-selection-count">0 selected</span>
      <button type="button" id="chat-bulk-archive" data-action="bulk-archive-sessions" disabled>Archive</button>
    </div>
    <div id="chat-rail-empty" class="hidden">No sessions yet.</div>
    <div id="chat-rail-empty-filtered" class="hidden">No sessions match these filters.</div>
  </aside>
  <div>
    <div><div id="chat-run-status" class="memory-run-status hidden" role="status" aria-live="polite"></div></div>
    <div id="chat-log">
      <div id="chat-empty"><select id="chat-agent" aria-label="Agent"><option value="a1">Tester</option></select></div>
      <div id="chat-todos" class="hidden"></div>
      <div id="chat-messages"></div>
    </div>
    <div>
      <div id="chat-dock" aria-live="polite" class="hidden"></div>
      <div id="chat-queue" class="memory-queue hidden" aria-live="polite"></div>
      <form id="chat-form">
        <textarea id="chat-input" rows="1" aria-label="Message"></textarea>
        <button id="chat-send" type="submit" aria-label="Send">Send</button>
        <span id="chat-stop" class="hidden shrink-0"><button type="button" aria-label="Stop generating">Stop</button></span>
      </form>
    </div>
  </div>
  <dialog id="chat-delete-confirm-modal" class="modal" hx-boost="false">
    <div class="modal-box max-w-sm">
      <h3 class="text-lg font-semibold">Delete session?</h3>
      <p class="text-muted mt-1 text-sm">This permanently deletes <span id="chat-delete-confirm-name">this session</span>, its messages, and any sub-chats spawned from it. It cannot be undone.</p>
      <div class="modal-action">
        <button type="button" data-action="close-delete-session">Cancel</button>
        <button type="button" id="chat-delete-confirm-go">Delete</button>
      </div>
    </div>
  </dialog>
</div>
</body></html>`;
}

async function bootstrap(page: Page, railHTML: string): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));

  await page.setContent(skeleton(railHTML));
  await page.evaluate(() => {
    const w = window as unknown as { MemoryApp: unknown; fetch: unknown };
    w.MemoryApp = { toast: () => {} };
    w.fetch = () => Promise.resolve(new Response('[]', { status: 200 }));
  });

  await page.addScriptTag({ path: CHAT_TRANSPORT_JS });
  await page.addScriptTag({ path: CHAT_COMPONENTS_JS });
  await page.addScriptTag({ path: CHAT_HOST_JS });
  await page.addScriptTag({ path: CHAT_STREAM_JS });
  await page.addScriptTag({ path: CHAT_JS });
  return errors;
}

function rowHidden(page: Page, id: string): Promise<boolean> {
  return page.evaluate((rowID) => {
    const el = document.querySelector(`li[data-action="resume-session"][data-id="${rowID}"]`);
    return !!el && el.classList.contains('hidden');
  }, id);
}

function toggleExpanded(page: Page): Promise<string | null> {
  return page.evaluate(
    () => document.querySelector('[data-action="toggle-subchats"]')?.getAttribute('aria-expanded') ?? null,
  );
}

async function click(page: Page, selector: string): Promise<void> {
  await page.evaluate((sel) => {
    const el = document.querySelector(sel);
    if (!el) throw new Error(`missing element: ${sel}`);
    (el as HTMLElement).click();
  }, selector);
}

async function setFilter(page: Page, selector: string, value: string): Promise<void> {
  await page.evaluate(
    ({ sel, val }) => {
      const el = document.querySelector(sel) as HTMLSelectElement;
      el.value = val;
      el.dispatchEvent(new Event('change', { bubbles: true }));
    },
    { sel: selector, val: value },
  );
}

test.describe('spawned sub-chats', () => {
  test('starts collapsed and toggles the nested rows', async ({ page }) => {
    const errors = await bootstrap(page, rail(false));

    expect(await rowHidden(page, 'child-a')).toBe(true);
    expect(await rowHidden(page, 'child-b')).toBe(true);
    expect(await toggleExpanded(page)).toBe('false');

    await click(page, '[data-action="toggle-subchats"]');
    expect(await rowHidden(page, 'child-a')).toBe(false);
    expect(await rowHidden(page, 'child-b')).toBe(false);
    expect(await toggleExpanded(page)).toBe('true');

    await click(page, '[data-action="toggle-subchats"]');
    expect(await rowHidden(page, 'child-a')).toBe(true);
    expect(await toggleExpanded(page)).toBe('false');
    expect(errors, errors.join(' | ')).toEqual([]);
  });

  test('an active sub-chat opens its branch on load', async ({ page }) => {
    const errors = await bootstrap(page, rail(true, 'child-b'));

    expect(await toggleExpanded(page)).toBe('true');
    expect(await rowHidden(page, 'child-b')).toBe(false);
    expect(await rowHidden(page, 'child-a')).toBe(false);
    expect(errors, errors.join(' | ')).toEqual([]);
  });

  test('an open group stays open through a filter pass', async ({ page }) => {
    const errors = await bootstrap(page, rail(true));

    expect(await rowHidden(page, 'child-a')).toBe(false);
    await setFilter(page, '#chat-origin-filter', 'manual');
    expect(await rowHidden(page, 'child-a')).toBe(false);
    expect(await rowHidden(page, 'child-b')).toBe(false);
    expect(await toggleExpanded(page)).toBe('true');
    expect(errors, errors.join(' | ')).toEqual([]);
  });

  test('a child-only filter match keeps the collapsed parent reachable', async ({ page }) => {
    const errors = await bootstrap(page, rail(false));

    await setFilter(page, '#chat-agent-filter', 'internal');
    // Parent does not match the agent filter but its child does: the header
    // stays visible (collapsed) instead of hiding the only way to open it.
    expect(await rowHidden(page, 'parent')).toBe(false);
    expect(await rowHidden(page, 'child-a')).toBe(true);

    await click(page, '[data-action="toggle-subchats"]');
    expect(await rowHidden(page, 'child-a')).toBe(false);
    expect(await rowHidden(page, 'child-b')).toBe(false);

    // A filter that matches nothing hides the whole group and shows the
    // filtered empty state.
    await setFilter(page, '#chat-agent-filter', 'other');
    expect(await rowHidden(page, 'parent')).toBe(true);
    expect(
      await page.evaluate(() => !document.getElementById('chat-rail-empty-filtered')?.classList.contains('hidden')),
    ).toBe(true);
    expect(errors, errors.join(' | ')).toEqual([]);
  });

  test('the delete confirmation states the sub-chat blast radius', async ({ page }) => {
    const errors = await bootstrap(page, rail(false));

    await click(page, '[data-action="delete-session"][data-id="parent"]');
    expect(
      await page.evaluate(() => document.getElementById('chat-delete-confirm-name')?.textContent),
    ).toBe('Parent');
    expect(
      await page.evaluate(() => document.getElementById('chat-delete-confirm-modal')?.textContent),
    ).toContain('and any sub-chats spawned from it');
    expect(errors, errors.join(' | ')).toEqual([]);
  });
});
