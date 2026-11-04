import { test, expect, type Page, type Response } from '@playwright/test';
import { expectAppPage, goto } from '../../helpers/page';

// Project Settings (gateway/project_settings.templ + settings_handlers.go) is
// the gateway's heaviest HTMX surface: most fields are inline autosaves
// (`hx-post` + `hx-trigger="change delay:400ms"` + `hx-swap="none"`) whose only
// success signal is an `HX-Trigger` response header carrying a `memory-toast`
// event ({kind, message}) that app.js turns into a toast. This spec drives two
// representative autosave routes end-to-end and asserts the resulting
// PERSISTED state (the saved value renders after a full reload) — not just the
// transient toast.
//
// Exercised fields, both safe to change and symmetrically restorable:
//   - project-info textarea   → POST /settings/project/project_info
//   - editor-agent select     → POST /settings/editor (capability-gated)
//   - merge-agent select      → POST /settings/merge  (capability-gated)
//
// Both agent selects list every agent definition but only accept agents that
// grant the flow's tool set, so each test creates one capable and one toolless
// scratch agent, selects the capable one, restores the original selection and
// deletes both (in cleanup, pass/fail).

const SETTINGS = '/settings';

/** Resolve when the autosave POST to `path` completes. */
function waitForSave(page: Page, path: string): Promise<Response> {
  return page.waitForResponse(
    (r) => r.request().method() === 'POST' && new URL(r.url()).pathname === path,
    { timeout: 20_000 },
  );
}

/**
 * Fill + blur an input/textarea so HTMX's `change` trigger fires, then await
 * the resulting autosave POST. Registering the response wait before the edit
 * means it resolves whether `fill` or the blur dispatches `change` first.
 */
async function autosaveText(
  page: Page,
  selector: string,
  value: string,
  path: string,
): Promise<Response> {
  const field = page.locator(selector);
  const [resp] = await Promise.all([
    waitForSave(page, path),
    (async () => {
      await field.fill(value);
      await field.press('Tab'); // blur → browser fires `change`
    })(),
  ]);
  return resp;
}

interface ToastDetail {
  kind?: string;
  message?: string;
}

/**
 * Record the `memory-toast` events htmx fires from the HX-Trigger header, so
 * the "toast shown" assertion does not race the toast's 4.2s auto-dismissal.
 */
async function recordToasts(page: Page): Promise<void> {
  await page.evaluate(() => {
    const w = window as unknown as { __e2eToasts?: ToastDetail[] };
    w.__e2eToasts = [];
    document.body.addEventListener('memory-toast', (e) => {
      w.__e2eToasts!.push((e as CustomEvent<ToastDetail>).detail);
    });
  });
}

/** Assert a successful inline save: HTTP 200 + HX-Trigger toast + rendered toast. */
async function expectSavedToast(page: Page, resp: Response): Promise<void> {
  expect(resp.status()).toBe(200);
  expect(resp.headers()['hx-trigger'] ?? '').toContain('memory-toast');

  const toasts = await page.evaluate(
    () => (window as unknown as { __e2eToasts?: ToastDetail[] }).__e2eToasts ?? [],
  );
  expect(toasts).toContainEqual({ kind: 'success', message: 'Saved' });

  await expect(page.locator('#toast-container [role="alert"]').last()).toContainText('Saved');
}

test('project settings: project-info autosaves inline and persists after reload', async ({ page }) => {
  test.setTimeout(120_000);
  const marker = `E2E autosave ${Date.now()}`;
  let original: string | null = null;

  try {
    await goto(page, SETTINGS);
    await expectAppPage(page, /Project Settings/);
    const field = page.locator('#project-info');
    await expect(field).toBeVisible();
    original = await field.inputValue();
    await recordToasts(page);

    const resp = await autosaveText(page, '#project-info', marker, '/settings/project/project_info');
    await expectSavedToast(page, resp);

    // Resulting state, not the transient toast: a full reload renders the saved value.
    await page.reload();
    await expectAppPage(page, /Project Settings/);
    await expect(page.locator('#project-info')).toHaveValue(marker);
  } finally {
    // Restore the original value (only when we captured it before mutating).
    if (original !== null) {
      await goto(page, SETTINGS);
      const field = page.locator('#project-info');
      if ((await field.inputValue()) !== original) {
        await autosaveText(page, '#project-info', original, '/settings/project/project_info');
      }
      await page.reload();
      await expect(page.locator('#project-info')).toHaveValue(original);
    }
  }
});

test('project settings: editor agent autosaves and only editor-capable agents are selectable', async ({ page }) => {
  test.setTimeout(120_000);
  const stamp = Date.now();
  let capableId = '';
  let toollessId = '';
  let original: string | null = null;

  try {
    // The object-editing chat needs entity-create (or -update) and
    // relationship-create (gateway/agent_tools.go).
    const capable = await page.request.post('/api/agents', {
      data: {
        name: `E2E Editor Agent ${stamp}`,
        tools: ['entity-update', 'relationship-create'],
        skills: [],
        config: {},
      },
    });
    expect(capable.ok(), `create editor-capable agent failed (HTTP ${capable.status()})`).toBeTruthy();
    capableId = ((await capable.json()) as { id: string }).id;

    const toolless = await page.request.post('/api/agents', {
      data: { name: `E2E Toolless Editor ${stamp}`, tools: [], skills: [], config: {} },
    });
    expect(toolless.ok(), `create toolless agent failed (HTTP ${toolless.status()})`).toBeTruthy();
    toollessId = ((await toolless.json()) as { id: string }).id;

    await goto(page, SETTINGS);
    await expectAppPage(page, /Project Settings/);
    const select = page.locator('#editor-agent');
    await expect(select).toBeVisible();

    // An editor-capable agent is selectable; a toolless one is present but
    // disabled with its missing requirements spelled out.
    await expect(select.locator(`option[value="${capableId}"]`)).toHaveCount(1);
    await expect(select.locator(`option[value="${capableId}"]`)).not.toHaveAttribute('disabled', '');
    const toollessOption = select.locator(`option[value="${toollessId}"]`);
    await expect(toollessOption).toHaveCount(1);
    await expect(toollessOption).toHaveAttribute('disabled', '');
    await expect(toollessOption).toContainText('missing:');

    original = await select.inputValue();
    await recordToasts(page);

    const [resp] = await Promise.all([
      waitForSave(page, '/settings/editor'),
      select.selectOption(capableId), // select change → HTMX POST /settings/editor
    ]);
    await expectSavedToast(page, resp);

    await page.reload();
    await expectAppPage(page, /Project Settings/);
    await expect(page.locator('#editor-agent')).toHaveValue(capableId);
  } finally {
    if (original !== null) {
      await goto(page, SETTINGS);
      const select = page.locator('#editor-agent');
      const originalDisabled = await select
        .locator(`option[value="${original}"]`)
        .evaluate((el) => (el as HTMLOptionElement).disabled)
        .catch(() => true);
      // A stored value that is no longer editor-capable cannot be re-selected;
      // clear the setting instead of leaving it pointing at a scratch agent we
      // are about to delete.
      const restoreTo = originalDisabled ? '' : original;
      if ((await select.inputValue()) !== restoreTo) {
        await Promise.all([waitForSave(page, '/settings/editor'), select.selectOption(restoreTo)]);
      }
      await page.reload();
      await expect(page.locator('#editor-agent')).toHaveValue(restoreTo);
    }
    if (capableId) await page.request.delete(`/api/agents/${capableId}`).catch(() => {});
    if (toollessId) await page.request.delete(`/api/agents/${toollessId}`).catch(() => {});
  }
});

test('project settings: merge agent autosaves and only merge-capable agents are selectable', async ({ page }) => {
  test.setTimeout(120_000);
  const stamp = Date.now();
  let capableId = '';
  let toollessId = '';
  let original: string | null = null;

  try {
    // The merge flow needs entity-create (or -update), entity-delete,
    // entity-edges-get and relationship-create (gateway/agent_tools.go).
    const capable = await page.request.post('/api/agents', {
      data: {
        name: `E2E Merge Agent ${stamp}`,
        tools: ['entity-create', 'entity-delete', 'entity-edges-get', 'relationship-create'],
        skills: [],
        config: {},
      },
    });
    expect(capable.ok(), `create merge-capable agent failed (HTTP ${capable.status()})`).toBeTruthy();
    capableId = ((await capable.json()) as { id: string }).id;

    const toolless = await page.request.post('/api/agents', {
      data: { name: `E2E Toolless Agent ${stamp}`, tools: [], skills: [], config: {} },
    });
    expect(toolless.ok(), `create toolless agent failed (HTTP ${toolless.status()})`).toBeTruthy();
    toollessId = ((await toolless.json()) as { id: string }).id;

    await goto(page, SETTINGS);
    await expectAppPage(page, /Project Settings/);
    const select = page.locator('#merge-agent');
    await expect(select).toBeVisible();

    // A merge-capable agent is selectable; a toolless agent is present but
    // disabled with the missing requirements spelled out.
    await expect(select.locator(`option[value="${capableId}"]`)).toHaveCount(1);
    await expect(select.locator(`option[value="${capableId}"]`)).not.toHaveAttribute('disabled', '');
    const toollessOption = select.locator(`option[value="${toollessId}"]`);
    await expect(toollessOption).toHaveCount(1);
    await expect(toollessOption).toHaveAttribute('disabled', '');
    await expect(toollessOption).toContainText('missing:');

    original = await select.inputValue();
    await recordToasts(page);

    const [resp] = await Promise.all([
      waitForSave(page, '/settings/merge'),
      select.selectOption(capableId), // select change → HTMX POST /settings/merge
    ]);
    await expectSavedToast(page, resp);

    await page.reload();
    await expectAppPage(page, /Project Settings/);
    await expect(page.locator('#merge-agent')).toHaveValue(capableId);
  } finally {
    if (original !== null) {
      await goto(page, SETTINGS);
      const select = page.locator('#merge-agent');
      const originalDisabled = await select
        .locator(`option[value="${original}"]`)
        .evaluate((el) => (el as HTMLOptionElement).disabled)
        .catch(() => true);
      // A stored value that is no longer merge-capable cannot be re-selected;
      // clear the setting instead of leaving it pointing at a scratch agent we
      // are about to delete.
      const restoreTo = originalDisabled ? '' : original;
      if ((await select.inputValue()) !== restoreTo) {
        await Promise.all([waitForSave(page, '/settings/merge'), select.selectOption(restoreTo)]);
      }
      await page.reload();
      await expect(page.locator('#merge-agent')).toHaveValue(restoreTo);
    }
    if (capableId) await page.request.delete(`/api/agents/${capableId}`).catch(() => {});
    if (toollessId) await page.request.delete(`/api/agents/${toollessId}`).catch(() => {});
  }
});
