import { test, expect, type Page, type Response } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

// Remember-agent field (gateway/project_settings.templ `rememberDedupPanel` →
// POST /settings/remember/agent, `hx-trigger="change"` + `hx-swap="none"`).
// The select's option value is the agent NAME (unlike `#editor-agent`, which
// uses the agent id), and the empty option is the "domain-remember-agent
// (default)" fallback — selecting it clears the override. The task (5.7) is the
// remember-field route; the `dedup` sibling is deliberately not exercised:
// its empty input means "leave unchanged", so a set value cannot be
// symmetrically restored (same reason 5.1 rejected `dedup_threshold`).

const SETTINGS = '/settings';

function waitForSave(page: Page, path: string): Promise<Response> {
  return page.waitForResponse(
    (r) => r.request().method() === 'POST' && new URL(r.url()).pathname === path,
    { timeout: 20_000 },
  );
}

interface ToastDetail {
  kind?: string;
  message?: string;
}

async function recordToasts(page: Page): Promise<void> {
  await page.evaluate(() => {
    const w = window as unknown as { __e2eToasts?: ToastDetail[] };
    w.__e2eToasts = [];
    document.body.addEventListener('memory-toast', (e) => {
      w.__e2eToasts!.push((e as CustomEvent<ToastDetail>).detail);
    });
  });
}

async function expectSavedToast(page: Page, resp: Response): Promise<void> {
  expect(resp.status()).toBe(200);
  expect(resp.headers()['hx-trigger'] ?? '').toContain('memory-toast');

  const toasts = await page.evaluate(
    () => (window as unknown as { __e2eToasts?: ToastDetail[] }).__e2eToasts ?? [],
  );
  expect(toasts).toContainEqual({ kind: 'success', message: 'Saved' });

  await expect(page.locator('#toast-container [role="alert"]').last()).toContainText('Saved');
}

test('remember-agent select autosaves inline and persists after reload', async ({ page }) => {
  test.setTimeout(120_000);
  const name = `E2E Remember Agent ${Date.now()}`;
  let agentId = '';
  let original: string | null = null;

  try {
    // A scratch project agent provides a selectable option (value = its name).
    const created = await page.request.post('/api/agents', {
      data: { name, tools: [], skills: [], config: {} },
    });
    expect(created.ok(), `create scratch agent failed (HTTP ${created.status()})`).toBeTruthy();
    agentId = ((await created.json()) as { id: string }).id;

    await page.goto(SETTINGS);
    await expectAppPage(page, /Project Settings/);
    const select = page.locator('#remember-agent');
    await expect(select).toBeVisible();
    await expect(select.locator(`option[value="${name}"]`)).toHaveCount(1);
    original = await select.inputValue();
    await recordToasts(page);

    const [resp] = await Promise.all([
      waitForSave(page, '/settings/remember/agent'),
      select.selectOption(name), // change → HTMX POST /settings/remember/agent
    ]);
    await expectSavedToast(page, resp);

    await page.reload();
    await expectAppPage(page, /Project Settings/);
    await expect(page.locator('#remember-agent')).toHaveValue(name);
  } finally {
    if (original !== null) {
      await page.goto(SETTINGS);
      const select = page.locator('#remember-agent');
      if ((await select.inputValue()) !== original) {
        await Promise.all([
          waitForSave(page, '/settings/remember/agent'),
          select.selectOption(original),
        ]);
      }
      await page.reload();
      await expect(page.locator('#remember-agent')).toHaveValue(original);
    }
    if (agentId) await page.request.delete(`/api/agents/${agentId}`).catch(() => {});
  }
});
