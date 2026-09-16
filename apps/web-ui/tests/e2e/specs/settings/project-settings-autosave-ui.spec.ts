import { test, expect, type Page, type Response } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

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
//   - editor-agent select     → POST /settings/editor
//
// The editor select only lists the project's agent definitions, so the second
// test creates a scratch `E2E Autosave Agent …` via the API, selects it, then
// restores the original selection and deletes the agent (in cleanup, pass/fail).

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
    await page.goto(SETTINGS);
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
      await page.goto(SETTINGS);
      const field = page.locator('#project-info');
      if ((await field.inputValue()) !== original) {
        await autosaveText(page, '#project-info', original, '/settings/project/project_info');
      }
      await page.reload();
      await expect(page.locator('#project-info')).toHaveValue(original);
    }
  }
});

test('project settings: editor-agent autosaves inline and persists after reload', async ({ page }) => {
  test.setTimeout(120_000);
  const name = `E2E Autosave Agent ${Date.now()}`;
  let agentId = '';
  let original: string | null = null;

  try {
    const created = await page.request.post('/api/agents', {
      data: { name, tools: [], skills: [], config: {} },
    });
    expect(created.ok(), `create scratch agent failed (HTTP ${created.status()})`).toBeTruthy();
    agentId = ((await created.json()) as { id: string }).id;

    await page.goto(SETTINGS);
    await expectAppPage(page, /Project Settings/);
    const select = page.locator('#editor-agent');
    await expect(select).toBeVisible();
    await expect(select.locator(`option[value="${agentId}"]`)).toHaveCount(1);
    original = await select.inputValue();
    await recordToasts(page);

    const [resp] = await Promise.all([
      waitForSave(page, '/settings/editor'),
      select.selectOption(agentId), // select change → HTMX POST /settings/editor
    ]);
    await expectSavedToast(page, resp);

    await page.reload();
    await expectAppPage(page, /Project Settings/);
    await expect(page.locator('#editor-agent')).toHaveValue(agentId);
  } finally {
    if (original !== null) {
      await page.goto(SETTINGS);
      const select = page.locator('#editor-agent');
      if ((await select.inputValue()) !== original) {
        await Promise.all([waitForSave(page, '/settings/editor'), select.selectOption(original)]);
      }
      await page.reload();
      await expect(page.locator('#editor-agent')).toHaveValue(original);
    }
    if (agentId) await page.request.delete(`/api/agents/${agentId}`).catch(() => {});
  }
});
