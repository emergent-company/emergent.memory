import { test, expect, type Page, type Locator } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';

// MCP Servers management UI (gateway/mcp_servers.templ +
// mcp_servers_handlers.go): registers an external http-transport server through
// the create page, asserts its list row (type badge + tool count), edits its
// URL, then deletes it through the row's confirm dialog. The example server is
// never dialed by the gateway itself — sync/inspect reach out from the memory
// backend, so those steps need a real example server URL that the dev memory
// host can reach (see tests/e2e/.env.e2e.example):
//
//   E2E_MCP_EXAMPLE_URL           example MCP server the memory backend dials
//   E2E_MCP_EXAMPLE_HEADER_NAME   optional Authorization header name (and
//   E2E_MCP_EXAMPLE_HEADER_VALUE  value) registered with the server
//
// Without the URL the spec still exercises the full UI lifecycle (create →
// row → edit → delete) against a syntax-valid placeholder URL — memory only
// validates URL syntax/transport on create — and the sync/inspect/tool-toggle
// steps are recorded as skipped-step annotations instead of running (they can
// never pass against an unreachable URL, and must never fail on network state).
const MCP_BASE = '/settings/mcp-servers';
const EXAMPLE_URL = (process.env.E2E_MCP_EXAMPLE_URL || '').trim();
const EXAMPLE_HEADER_NAME = (process.env.E2E_MCP_EXAMPLE_HEADER_NAME || '').trim();
const EXAMPLE_HEADER_VALUE = (process.env.E2E_MCP_EXAMPLE_HEADER_VALUE || '').trim();
const EXAMPLE_URL_CONFIGURED = EXAMPLE_URL.length > 0;
// Placeholder when E2E_MCP_EXAMPLE_URL is unset: well-formed http URL that
// passes the create-time syntax validation; nothing ever dials it.
const PLACEHOLDER_URL = 'http://localhost:9999';

// Raised inside the network-dependent block to signal an environment-limited
// step (unreachable example server, no tools advertised, …). Caught in the
// test and recorded as a skipped-step annotation so the always-runnable UI
// lifecycle (edit/delete/cleanup) still completes.
class EnvStepSkip extends Error {}

function appendEditedMarker(url: string): string {
  const sep = url.includes('?') ? '&' : '?';
  return `${url}${sep}e2e=${Date.now()}`;
}

// Newest visible toast text inside the Alpine #toast-container queue ('' when
// none). Sync failures surface as an error toast carrying memory's reason.
async function lastAlertText(page: Page): Promise<string> {
  const alerts = page.locator('#toast-container [role="alert"]');
  const count = await alerts.count().catch(() => 0);
  for (let i = count - 1; i >= 0; i--) {
    const text = ((await alerts.nth(i).textContent().catch(() => '')) ?? '').trim();
    if (text) return text;
  }
  return '';
}

interface SyncOutcome {
  ok: boolean;
  toolCount: number;
  reason: string;
}

// Click the row's Sync action (a client-side POST to /api/mcp-servers/:id/sync)
// and watch the data-mcp-tools container for freshly cached tool rows. A
// failed sync (unreachable server) leaves the container untouched and raises an
// error toast — read it for the skip reason. Never asserts on network state.
async function runRowSync(
  page: Page,
  row: Locator,
  name: string,
  serverId: string,
): Promise<SyncOutcome> {
  await row.getByRole('button', { name: `Sync ${name}` }).click();
  const container = page.locator(`[data-mcp-tools="${serverId}"]`);
  const deadline = Date.now() + 60_000;
  while (Date.now() < deadline) {
    const toolCount = await container.locator('[data-mcp-tool-row]').count().catch(() => 0);
    if (toolCount > 0) return { ok: true, toolCount, reason: '' };
    const toast = await lastAlertText(page);
    if (toast && /sync/i.test(toast) && /fail|error|unreachable|could not|couldn't|refused|timeout|EOF/i.test(toast)) {
      return { ok: false, toolCount: 0, reason: toast };
    }
    await page.waitForTimeout(250);
  }
  return { ok: false, toolCount: 0, reason: 'sync did not complete within 60s' };
}

// Toggle the first enabled tool off and confirm the state change both in the
// DOM (unchecked) and persisted via GET /api/mcp-servers/:id/tools.
async function toggleFirstEnabledToolOff(page: Page, serverId: string): Promise<void> {
  const container = page.locator(`[data-mcp-tools="${serverId}"]`);
  const toggles = container.locator('[data-mcp-tool-toggle]');
  let target: Locator | null = null;
  const count = await toggles.count();
  for (let i = 0; i < count; i++) {
    if (await toggles.nth(i).isChecked()) {
      target = toggles.nth(i);
      break;
    }
  }
  if (!target) {
    throw new EnvStepSkip('all synced tools are already disabled — nothing to toggle off');
  }
  const toolId = (await target.getAttribute('data-mcp-tool')) ?? '';
  await expect(target).toBeChecked();
  await target.uncheck();
  await expect(target).not.toBeChecked({ timeout: 10_000 });
  await expect
    .poll(
      async () => {
        const resp = await page.request.get(`/api/mcp-servers/${serverId}/tools`);
        if (!resp.ok()) return null;
        const tools = (await resp.json()) as Array<{ id: string; enabled: boolean }>;
        return tools.find((t) => t.id === toolId)?.enabled;
      },
      { timeout: 15_000 },
    )
    .toBe(false);
}

// Run the row's Inspect action and require the rendered result to report the
// server reachable. Inspect never fails the test on network state: an
// unreachable server renders "Inspect failed — <reason>" into the inspect
// area, which becomes an EnvStepSkip instead.
async function inspectRowReachable(page: Page, row: Locator, name: string, serverId: string): Promise<void> {
  await row.getByRole('button', { name: `Inspect ${name}` }).click();
  const area = page.locator(`[data-mcp-inspect-area="${serverId}"]`);
  await expect(area).toBeVisible({ timeout: 60_000 });
  const text = ((await area.textContent()) ?? '').trim();
  if (!/^Reachable/.test(text)) {
    throw new EnvStepSkip(`inspect of ${EXAMPLE_URL} failed: ${text}`);
  }
}

// Expand the row (its actions/tools live in a collapsed <details> body) and
// return the scoped row locator.
async function openServerRow(page: Page, name: string): Promise<Locator> {
  const row = page.locator('[data-mcp-row]', { hasText: name });
  await expect(row).toBeVisible();
  await row.locator('summary').click();
  await expect(row.getByRole('button', { name: `Sync ${name}` })).toBeVisible();
  return row;
}

// Delete through the row's confirm dialog (native <dialog id="mcp-server-delete-…">
// opened by the row's Delete action). PRG-redirects to the list with ?deleted=1.
async function deleteServerViaUi(page: Page, name: string, serverId: string): Promise<void> {
  const row = page.locator('[data-mcp-row]', { hasText: name });
  await expect(row).toBeVisible();
  await row.locator('summary').click();
  const deleteBtn = row.getByRole('button', { name: `Delete ${name}` });
  await expect(deleteBtn).toBeVisible();
  await deleteBtn.click();
  const dialog = page.locator(`#mcp-server-delete-${serverId}`);
  await expect(dialog).toBeVisible();
  await dialog.getByRole('button', { name: 'Delete server' }).click();
  await page.waitForURL(/\/settings\/mcp-servers\?deleted=1/);
  await expect(page.locator(`[data-mcp-server="${serverId}"]`)).toHaveCount(0);
}

test('registers an MCP server via the settings UI end-to-end', async ({ page }) => {
  // Generous budget: the gated sync/inspect steps can take up to a minute each.
  test.setTimeout(300_000);

  const name = `E2E MCP ${Date.now()}`;
  const createUrl = EXAMPLE_URL || PLACEHOLDER_URL;
  const editedUrl = appendEditedMarker(createUrl);
  let serverId = '';

  try {
    // 1. NAVIGATE (UI): the list page → New server form.
    await page.goto(MCP_BASE);
    await expectAppPage(page, /MCP Servers/);
    const newLink = page.getByRole('link', { name: 'New server' });
    if (await newLink.isVisible().catch(() => false)) {
      await newLink.click();
    } else {
      // A project with zero servers renders the empty-state CTA instead.
      await page.getByRole('link', { name: 'Register a server' }).click();
    }
    await expectAppPage(page, /New MCP server/);

    // 2. CREATE (UI): http transport + name + URL (+ optional header row).
    await page.locator('#mcp-server-name').fill(name);
    await page.locator('input[name="type"][value="http"]').check();
    const urlInput = page.locator('#mcp-server-url');
    await expect(urlInput).toBeVisible();
    await urlInput.fill(createUrl);
    if (EXAMPLE_HEADER_NAME && EXAMPLE_HEADER_VALUE) {
      const kv = page.locator('[data-mcp-kv-prefix="headers"]');
      await kv.locator('input[name="headers.key"]').fill(EXAMPLE_HEADER_NAME);
      await kv.locator('input[name="headers.value"]').fill(EXAMPLE_HEADER_VALUE);
    }
    await page.getByRole('button', { name: 'Create server' }).click();
    await page.waitForURL(/\/settings\/mcp-servers\?created=1/);

    // 3. ROW (UI): the new server appears with its transport badge + tool count.
    await expectAppPage(page, /MCP Servers/);
    let row = page.locator('[data-mcp-row]', { hasText: name });
    await expect(row).toBeVisible();
    serverId = (await row.getAttribute('data-mcp-server')) ?? '';
    expect(serverId, 'list row must carry the server id').toBeTruthy();
    await expect(row.locator('summary')).toContainText(name);
    await expect(row.getByText('http', { exact: true })).toBeVisible();
    await expect(row.getByText('enabled', { exact: true })).toBeVisible();
    const countBadge = row.locator('[data-mcp-count]');
    await expect(countBadge).toBeVisible();
    await expect(countBadge).toHaveText(/^\d+ tools?$/);

    // 4. SYNC / TOGGLE / INSPECT — gated on a real example server the memory
    // backend can reach. Without it (or when the server is unreachable) the
    // step records a skipped-step annotation and the run continues; these
    // assertions must never fail on network state.
    if (!EXAMPLE_URL_CONFIGURED) {
      test.info().annotations.push({
        type: 'skipped-step',
        description:
          'E2E_MCP_EXAMPLE_URL is not set — registered the syntax-valid placeholder ' +
          `${PLACEHOLDER_URL} instead. Sync/inspect/tool-toggle need an example MCP server ` +
          'the dev memory backend can dial; set E2E_MCP_EXAMPLE_URL (and optionally ' +
          'E2E_MCP_EXAMPLE_HEADER_NAME/VALUE) in tests/e2e/.env.e2e to exercise them.',
      });
    } else {
      try {
        row = await openServerRow(page, name);

        // Sync from the row → cached tools render in the row.
        const outcome = await runRowSync(page, row, name, serverId);
        if (!outcome.ok) throw new EnvStepSkip(`sync to ${EXAMPLE_URL} failed: ${outcome.reason}`);
        expect(outcome.toolCount, 'example server should advertise at least one tool').toBeGreaterThan(0);
        await expect(row.locator('[data-mcp-count]')).toHaveText(
          `${outcome.toolCount} tool${outcome.toolCount === 1 ? '' : 's'}`,
        );

        // Toggle one tool off: DOM state change + persisted via the API.
        await toggleFirstEnabledToolOff(page, serverId);

        // Inspect the (reachable) server.
        await inspectRowReachable(page, row, name, serverId);
      } catch (e) {
        if (e instanceof EnvStepSkip) {
          test.info().annotations.push({ type: 'skipped-step', description: e.message });
        } else {
          throw e;
        }
      }
    }

    // 5. EDIT (UI): change the URL, save, and verify persistence on the
    // pre-filled edit page. Transport is fixed after registration, so the URL
    // is the editable connection detail. Memory re-validates only URL syntax
    // here, so this step runs in both env-gated modes.
    await page.goto(`${MCP_BASE}/${serverId}/edit`);
    await expectAppPage(page, /Edit server/);
    await expect(page.locator('#mcp-server-url')).toHaveValue(createUrl);
    await page.locator('#mcp-server-url').fill(editedUrl);
    await page.getByRole('button', { name: 'Save changes' }).click();
    await page.waitForURL(/\/settings\/mcp-servers\?updated=1/);
    await expectAppPage(page, /MCP Servers/);
    await expect(page.locator('[data-mcp-row]', { hasText: name })).toBeVisible();
    await page.goto(`${MCP_BASE}/${serverId}/edit`);
    await expectAppPage(page, /Edit server/);
    await expect(page.locator('#mcp-server-url')).toHaveValue(editedUrl);

    // 6. DELETE (UI): row action → confirm dialog → ?deleted=1 → row gone.
    await page.goto(MCP_BASE);
    await expectAppPage(page, /MCP Servers/);
    await deleteServerViaUi(page, name, serverId);
  } finally {
    // Belt-and-braces cleanup: never leave `E2E MCP *` servers behind, even
    // when the UI delete was interrupted (skip/assertion/air restart).
    if (serverId) {
      await page.request.delete(`/api/mcp-servers/${serverId}`).catch(() => {});
    }
  }
});

test('leaves no E2E MCP servers behind (self-cleanup guard)', async ({ page }) => {
  const resp = await page.request.get('/api/mcp-servers');
  expect(resp.ok(), `list mcp servers failed (HTTP ${resp.status()})`).toBeTruthy();
  const servers = (await resp.json()) as Array<{ id: string; name: string }>;
  const leaked = servers.filter((s) => s.name.startsWith('E2E MCP'));
  expect(leaked).toEqual([]);
});
