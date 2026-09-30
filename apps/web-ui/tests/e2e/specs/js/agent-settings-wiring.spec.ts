import { test, expect, type Page } from '@playwright/test';
import path from 'node:path';

// Hermetic DOM-level wiring spec for the agent settings client
// (`gateway/webui/static/js/agent-settings.js`, the exact file the browser
// receives). It loads the shipped JS into a bare page and drives the two
// behaviours added for the settings feedback:
//
//   #1276 — debounced auto-save: text edits post once after the debounce
//           window (not per keystroke), unchanged values never post, and a
//           failed save surfaces an error + retry without dropping the edit.
//   #1274 — tool-approval inheritance: changing the default or a group policy
//           relabels every policy select's `Inherit (<value>)` option; the group
//           policy select lives in the disclosure BODY (not inside the
//           interactive <summary>), so its click is never cancelled and the
//           group only toggles from its own summary trigger.
//
// No gateway, no memory API, no Zitadel, no `.env.e2e` — runs in CI
// (`npx playwright test --config=js-dom.config.ts`).

const AGENT_SETTINGS_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/agent-settings.js',
);

interface FetchCall {
  url: string;
  body: string;
}

interface FetchResponse {
  status: number;
  body: unknown;
}

// Install a recording fetch stub before the script under test loads. The
// response is read from window.__fetchResponse at call time, so a test can
// switch it (e.g. to a 422) without re-registering anything.
async function installFetch(page: Page) {
  await page.evaluate(() => {
    const w = window as unknown as { __fetchCalls: FetchCall[]; __fetchResponse: FetchResponse };
    w.__fetchCalls = [];
    w.__fetchResponse = { status: 200, body: { ok: true } };
    (window as unknown as { fetch: unknown }).fetch = function (url: string, opts: { body?: string }) {
      const body = (opts && opts.body) || '';
      w.__fetchCalls.push({ url: url, body: body });
      const res = w.__fetchResponse;
      return Promise.resolve({
        ok: res.status >= 200 && res.status < 300,
        status: res.status,
        json: function () {
          return Promise.resolve(res.body);
        },
      });
    };
  });
}

async function setFetchResponse(page: Page, status: number, body: unknown) {
  await page.evaluate(
    (payload: FetchResponse) => {
      (window as unknown as { __fetchResponse: FetchResponse }).__fetchResponse = payload;
    },
    { status: status, body: body },
  );
}

async function loadScript(page: Page) {
  await page.addScriptTag({ path: AGENT_SETTINGS_JS });
}

async function fetches(page: Page): Promise<FetchCall[]> {
  return (await page.evaluate(
    () => (window as unknown as { __fetchCalls: FetchCall[] }).__fetchCalls,
  )) as FetchCall[];
}

const AUTOSAVE_FORM = `
<form method="post" action="/agents/a1/settings/general"
      data-agent-autosave="general"
      data-autosave-url="/agents/a1/settings/general/autosave">
  <input id="agent-settings-name" name="name" type="text" value="Diane">
  <textarea id="agent-settings-prompt" name="systemPrompt">be terse</textarea>
  <div data-autosave-status data-state="idle" data-testid="agent-settings-autosave-status">
    <span class="iconify lucide--cloud-check" data-autosave-icon></span>
    <span data-autosave-status-label>All changes saved</span>
    <button type="button" data-autosave-retry hidden>Retry</button>
  </div>
</form>`;

const TOOLS_FORM = `
<form method="post" action="/agents/a1/settings/tools">
  <select name="defaultToolPolicy" data-tool-default-policy>
    <option value="allow" selected>Allow</option>
    <option value="ask">Ask</option>
    <option value="deny">Deny</option>
  </select>
  <details data-testid="tool-group" data-tool-group="search" open>
    <summary id="group-summary">
      <span>Search</span>
    </summary>
    <div>
      <label for="group-policy-search">Approval policy</label>
      <select id="group-policy-search" name="groupPolicy.search" data-testid="tool-group-policy-search">
        <option value="inherit" data-inherit-option selected>Inherit (Allow)</option>
        <option value="allow">Allow</option>
        <option value="ask">Ask</option>
        <option value="deny">Deny</option>
      </select>
    </div>
    <div>
      <select name="toolPolicy.web_search">
        <option value="" data-inherit-option selected>Inherit (Allow)</option>
        <option value="ask">Ask</option>
        <option value="deny">Deny</option>
      </select>
      <select name="toolPolicy.web_fetch">
        <option value="" data-inherit-option selected>Inherit (Allow)</option>
        <option value="ask">Ask</option>
        <option value="deny">Deny</option>
      </select>
    </div>
  </details>
  <details data-testid="tool-group-source">
    <summary>
      <span>web-tools</span>
    </summary>
    <div>
      <select name="toolPolicy.relay_read">
        <option value="" data-inherit-option selected>Inherit (Allow)</option>
        <option value="ask">Ask</option>
        <option value="deny">Deny</option>
      </select>
    </div>
  </details>
</form>`;

function inheritText(select: import('@playwright/test').Locator): Promise<string> {
  // textContent, not innerText: the source group's <details> is closed, so its
  // select is not rendered and innerText would read empty.
  return select.locator('option[data-inherit-option]').textContent() as Promise<string>;
}

test.describe('agent settings auto-save (agent-settings.js)', () => {
  test('debounces text edits, skips unchanged values, and retries after an error', async ({ page }) => {
    await page.setContent(`<!doctype html><html><body>${AUTOSAVE_FORM}</body></html>`);
    await installFetch(page);
    await loadScript(page);

    const status = page.getByTestId('agent-settings-autosave-status');
    const name = page.locator('#agent-settings-name');

    // Typing schedules a save; nothing fires inside the debounce window.
    await name.fill('Diane Renamed');
    await page.waitForTimeout(300);
    expect(await fetches(page)).toHaveLength(0);

    // After the window, exactly one save carrying the edit.
    await expect.poll(async () => (await fetches(page)).length, { timeout: 3000 }).toBe(1);
    const first = (await fetches(page))[0];
    expect(first.url).toBe('/agents/a1/settings/general/autosave');
    expect(first.body).toContain('name=Diane+Renamed');
    await expect(status).toHaveAttribute('data-state', 'saved');
    await expect(status.locator('[data-autosave-status-label]')).toHaveText('All changes saved');

    // Re-entering the same value is not a change: no request.
    await name.fill('Diane Renamed');
    await page.waitForTimeout(900);
    expect(await fetches(page)).toHaveLength(1);

    // A server validation failure is surfaced with a retry, and nothing is lost.
    await setFetchResponse(page, 422, { ok: false, error: 'name is required' });
    await name.fill('');
    await expect.poll(async () => (await fetches(page)).length, { timeout: 3000 }).toBe(2);
    await expect(status).toHaveAttribute('data-state', 'error');
    await expect(status.locator('[data-autosave-status-label]')).toHaveText('name is required');
    await expect(page.locator('[data-autosave-retry]')).toBeVisible();
    await expect(name).toHaveValue('');
  });
});

test.describe('tool-approval inheritance (agent-settings.js)', () => {
  test('changing a group policy relabels its tools; changing the default relabels the rest', async ({ page }) => {
    await page.setContent(`<!doctype html><html><body>${TOOLS_FORM}</body></html>`);
    await loadScript(page);

    const defaultSel = page.locator('select[name="defaultToolPolicy"]');
    const groupSel = page.locator('select[name="groupPolicy.search"]');
    const toolInGroup = page.locator('select[name="toolPolicy.web_search"]');
    const toolOutside = page.locator('select[name="toolPolicy.relay_read"]');

    // Initial render: group select falls back to the default, group tools have
    // no group policy yet, the source tool inherits the default.
    expect(await inheritText(groupSel)).toBe('Inherit (Allow)');
    expect(await inheritText(toolInGroup)).toBe('Inherit (Allow)');
    expect(await inheritText(toolOutside)).toBe('Inherit (Allow)');

    // Group policy → Deny: the group's tools now inherit Deny, the group select
    // still inherits the default, the source tool is untouched.
    await groupSel.selectOption('deny');
    expect(await inheritText(toolInGroup)).toBe('Inherit (Deny)');
    expect(await inheritText(groupSel)).toBe('Inherit (Allow)');
    expect(await inheritText(toolOutside)).toBe('Inherit (Allow)');

    // Default → Ask: the group select relabels to Ask, the group's tools keep
    // the group policy (Deny), the source tool inherits the new default.
    await defaultSel.selectOption('ask');
    expect(await inheritText(groupSel)).toBe('Inherit (Ask)');
    expect(await inheritText(toolInGroup)).toBe('Inherit (Deny)');
    expect(await inheritText(toolOutside)).toBe('Inherit (Ask)');
  });

  test('the group toggles from its trigger and the policy select keeps its normal click', async ({ page }) => {
    await page.setContent(`<!doctype html><html><body>${TOOLS_FORM}</body></html>`);
    await loadScript(page);

    const group = page.locator('details[data-tool-group="search"]');
    const summary = page.locator('#group-summary');
    const select = page.locator('select[name="groupPolicy.search"]');

    // New structure: the policy select sits in the disclosure body, never a
    // descendant of the interactive <summary>, so its click cannot run the
    // summary activation. Nothing cancels the click default either — the native
    // dropdown is free to open. (The old fix cancelled the click in the capture
    // phase; that hack is gone.)
    const shape = await page.evaluate(() => {
      const sel = document.querySelector('select[name="groupPolicy.search"]') as HTMLElement;
      let prevented = false;
      const read = (ev: Event) => {
        prevented = ev.defaultPrevented;
      };
      sel.addEventListener('click', read);
      sel.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
      sel.removeEventListener('click', read);
      return { inSummary: sel.closest('summary') !== null, clickPrevented: prevented };
    });
    expect(shape.inSummary).toBe(false);
    expect(shape.clickPrevented).toBe(false);

    // Clicking the select does not fold the group (no summary activation ran).
    await select.dispatchEvent('click');
    await expect(group).toHaveAttribute('open', '');

    // The summary trigger alone is what toggles the group.
    await summary.click();
    await expect(group).not.toHaveAttribute('open', '');
    await summary.click();
    await expect(group).toHaveAttribute('open', '');
  });
});
