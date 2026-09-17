import { test, expect, type Page } from '@playwright/test';
import { expectAppPage } from '../../helpers/page';
import { createAgentViaModal } from '../../helpers/agents';
import { memoryAuthHeaders } from '../../helpers/objects';
import { STORAGE_STATE } from '../../constants/storage';

// Agent-scoped MCP endpoint UI (gateway/agent_mcp_endpoint.templ +
// agent_mcp_endpoint_handlers.go): the "MCP endpoint" section on the agent's
// own Settings page. It creates/revokes the endpoint, mints labeled keys with a
// one-time secret reveal, rotates and revokes individual keys, and lists the
// external client sessions those keys produced.
//
// This lives in the serial `mutations` project (the `-ui.spec.ts` testMatch) —
// it creates credentials and revokes them again, and it must not race the
// parallel read surface. The whole file shares ONE dedicated `E2E MCP Agent`
// created in `beforeAll` and removed in `afterAll`, so the bootstrap tenant's
// shared agents are never touched. Cleanup revokes every key it created, then
// the endpoint, then the agent, so repeated runs are idempotent.
//
// The sessions assertion only depends on a brand-new endpoint having no
// sessions — it never needs a live agent run to produce one.

const SETTINGS = (id: string) => `/agents/${encodeURIComponent(id)}/settings/mcp`;
const SHARES_NEW = '/settings/mcp-servers/shares/new';
const SHARES = '/settings/mcp-servers/shares';

// The memory backend the gateway proxies to (see helpers/objects.ts). The
// agent MCP endpoint API is newer than the rest of the surface, so the spec
// probes for it once and skips the endpoint-dependent tests with a stated
// reason when the deployed backend predates it — the always-runnable render
// assertions below still execute.
const MEMORY_API_URL = process.env.E2E_MEMORY_API_URL || 'https://api.dev.emergent-company.ai';

/** One key row as returned by the gateway JSON list (no secret by construction). */
interface AgentMCPKeyRow {
  id: string;
  label: string;
  status: string;
}

let agentId = '';
let agentName = '';
let endpointId = '';
let backendReady = true;
let backendGateReason = '';

/**
 * Probe whether the upstream memory backend exposes the agent MCP endpoint API.
 * A POST to the route with a placeholder agent id answers with a domain error
 * when the route exists, and with Echo's generic `not_found` when it does not —
 * a side-effect-free way to distinguish "route missing" from "agent missing".
 * Any failure to classify assumes support, so a real feature gap fails loudly
 * rather than being masked.
 */
async function probeAgentMCPBackend(page: Page): Promise<{ ok: boolean; reason: string }> {
  let version = 'unknown';
  try {
    const health = await page.request.get(`${MEMORY_API_URL}/health`);
    if (health.ok()) version = ((await health.json()) as { version?: string }).version ?? version;
  } catch {
    // best-effort only
  }
  try {
    const headers = await memoryAuthHeaders(page);
    const resp = await page.request.post(
      `${MEMORY_API_URL}/api/projects/${headers['X-Project-ID']}/agents/00000000-0000-0000-0000-000000000000/mcp-endpoint`,
      { headers, failOnStatusCode: false },
    );
    if (resp.status() === 404) {
      const body = (await resp.json().catch(() => ({}))) as { error?: { code?: string } };
      if (body?.error?.code === 'not_found') {
        return {
          ok: false,
          reason:
            `the memory backend behind the gateway (${MEMORY_API_URL}, version ${version}) does not ` +
            'expose the agent MCP endpoint API yet: POST ' +
            '/api/projects/:projectId/agents/:agentId/mcp-endpoint answers 404 not_found, so the ' +
            'endpoint/key/session lifecycle cannot run. Deploy a backend that contains the ' +
            'agent-scoped endpoint routes, then re-run this spec.',
        };
      }
    }
    return { ok: true, reason: '' };
  } catch {
    return { ok: true, reason: '' };
  }
}

/** Skip an endpoint-dependent test when the upstream backend lacks the API. */
function requireAgentMCPBackend(): void {
  if (backendReady) return;
  // Record the reason as an annotation too, so it is visible in the HTML/JSON
  // reports and not only on the console.
  test.info().annotations.push({ type: 'skipped-backend', description: backendGateReason });
  test.skip(true, backendGateReason);
}

/** The endpoint's keys via the gateway JSON surface (metadata only). */
async function endpointKeys(page: Page): Promise<AgentMCPKeyRow[]> {
  if (!endpointId) return [];
  const resp = await page.request.get(`/api/agent-mcp-endpoints/${endpointId}/keys`);
  if (!resp.ok()) return [];
  const body = (await resp.json()) as { keys?: AgentMCPKeyRow[] };
  return body.keys ?? [];
}

/** Locate one key row by its label. */
function keyRow(page: Page, label: string) {
  return page.getByTestId('agent-mcp-key-row').filter({ hasText: label });
}

/**
 * Create a labeled key through the form and return the one-time secret shown in
 * the reveal modal. Leaves the modal open; success renders the settings page at
 * the POST URL with `#agent-mcp-reveal-modal` already open (no redirect — the
 * raw secret exists only in that response).
 */
async function createKeyViaUi(page: Page, label: string): Promise<string> {
  await page.goto(SETTINGS(agentId));
  await page.locator('#agent-mcp-key-label').fill(label);
  await page.getByTestId('agent-mcp-create-key').getByRole('button', { name: 'Create key' }).click();

  const secret = page.getByTestId('agent-mcp-key-secret');
  await expect(secret).toBeVisible();
  const token = ((await secret.textContent()) ?? '').trim();
  expect(token, 'the reveal must carry a non-empty raw secret').not.toBe('');
  return token;
}

/** Close the one-time reveal modal so the underlying section is usable. */
async function closeRevealModal(page: Page): Promise<void> {
  await page.getByRole('button', { name: "Done — I've saved the key" }).click();
  await expect(page.getByTestId('agent-mcp-key-secret')).toBeHidden();
}

/** Click a row action, confirm in the opened native dialog, and wait for the PRG URL. */
async function confirmRowAction(page: Page, actionName: string | RegExp, confirmName: string, urlPattern: RegExp): Promise<void> {
  await page.getByRole('button', { name: actionName }).click();
  const dialog = page.locator('dialog[open]');
  await expect(dialog).toBeVisible();
  await dialog.getByRole('button', { name: confirmName, exact: true }).click();
  await page.waitForURL(urlPattern);
}

test.describe.serial('agent MCP endpoint keys and sessions', () => {
  test.beforeAll(async ({ browser }) => {
    const context = await browser.newContext({ storageState: STORAGE_STATE });
    const page = await context.newPage();
    try {
      agentName = `E2E MCP Agent ${Date.now()}`;
      agentId = await createAgentViaModal(page, agentName);
      const probe = await probeAgentMCPBackend(page);
      backendReady = probe.ok;
      backendGateReason = probe.reason;
    } finally {
      await context.close();
    }
  });

  test.afterAll(async ({ browser }) => {
    if (!agentId) return;
    const context = await browser.newContext({ storageState: STORAGE_STATE });
    const page = await context.newPage();
    try {
      // Revoke every key the run left behind (idempotent), then the endpoint,
      // then the agent. All best-effort so a failing assertion cannot strand
      // credentials in the bootstrap tenant.
      if (endpointId) {
        for (const k of await endpointKeys(page)) {
          await page.request.delete(`/api/agent-mcp-keys/${k.id}`).catch(() => {});
        }
        await page.request.delete(`/api/agent-mcp-endpoints/${endpointId}`).catch(() => {});
      }
      await page.request.delete(`/api/agents/${agentId}`).catch(() => {});
    } finally {
      await context.close();
    }
  });

  test('exposes the MCP section on the agent Settings page', async ({ page }) => {
    await page.goto(SETTINGS(agentId));
    await expectAppPage(page, new RegExp(agentName));

    await expect(page.getByTestId('agent-mcp-section')).toBeVisible();
    // A freshly created agent has no endpoint: the section renders its
    // create-the-endpoint first step.
    await expect(page.getByTestId('agent-mcp-create-endpoint')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Create MCP endpoint' })).toBeVisible();
    await expect(page.getByTestId('agent-mcp-endpoint')).toHaveCount(0);
  });

  test('creates the endpoint and shows a readable, copyable client URL', async ({ page }) => {
    requireAgentMCPBackend();
    await page.goto(SETTINGS(agentId));
    await page.getByTestId('agent-mcp-create-endpoint').getByRole('button', { name: 'Create MCP endpoint' }).click();
    await page.waitForURL(/mcpCreated=1/);

    await expect(page.getByTestId('agent-mcp-endpoint')).toBeVisible();
    await expect(page.getByTestId('agent-mcp-endpoint-status')).toHaveText(/active/i);

    // The endpoint URL is what an external MCP client connects to; it must be
    // readable and match what the backend reports.
    const resp = await page.request.get(`/api/agents/${agentId}/mcp-endpoint`);
    expect(resp.ok(), `GET endpoint failed (HTTP ${resp.status()})`).toBeTruthy();
    const endpoint = (await resp.json()) as { id: string; mcpUrl: string };
    endpointId = endpoint.id;
    expect(endpointId, 'the created endpoint must have an id').toBeTruthy();
    expect(endpoint.mcpUrl, 'the backend must report a client URL').toBeTruthy();

    const url = page.locator('#agent-mcp-endpoint-url');
    await expect(url).toBeVisible();
    await expect(url).toHaveText(endpoint.mcpUrl);

    // Copy affordance: the button targets the URL element and really copies it.
    const copyBtn = page.getByRole('button', { name: 'Copy endpoint URL' });
    await expect(copyBtn).toBeVisible();
    await expect(copyBtn).toHaveAttribute('data-copy-target', '#agent-mcp-endpoint-url');
    await page.context().grantPermissions(['clipboard-read', 'clipboard-write']);
    await copyBtn.click();
    await expect
      .poll(async () => page.evaluate(() => navigator.clipboard.readText()), { timeout: 5_000 })
      .toBe(endpoint.mcpUrl);
  });

  test('renders the sessions panel with its empty state for an endpoint with no sessions', async ({ page }) => {
    requireAgentMCPBackend();
    expect(endpointId, 'the previous test must have created the endpoint').toBeTruthy();
    await page.goto(SETTINGS(agentId));

    await expect(page.getByTestId('agent-mcp-sessions')).toBeVisible();
    // A brand-new endpoint has no external client sessions. This must not
    // depend on a live agent run ever producing one.
    await expect(page.getByTestId('agent-mcp-sessions-empty')).toBeVisible();
    await expect(page.getByRole('group', { name: 'Filter sessions by status' })).toBeVisible();
  });

  test('creates a labeled key, shows the one-time secret, and lists an active row', async ({ page }) => {
    requireAgentMCPBackend();
    const label = `E2E Key ${Date.now()}`;
    const token = await createKeyViaUi(page, label);
    expect(token.length).toBeGreaterThan(0);

    // Still on the reveal response: closing it returns to the section, where the
    // new key is already listed.
    await closeRevealModal(page);
    const row = keyRow(page, label);
    await expect(row).toBeVisible();
    await expect(row.getByText('active', { exact: true })).toBeVisible();
    await expect(row.getByRole('button', { name: `Rotate key ${label}` })).toBeVisible();
    await expect(row.getByRole('button', { name: `Revoke key ${label}` })).toBeVisible();

    // The key is real: the JSON list carries it (metadata only, never the secret).
    const keys = await endpointKeys(page);
    const listed = keys.find((k) => k.label === label);
    expect(listed, `key "${label}" must appear in the endpoint key list`).toBeTruthy();
    expect(JSON.stringify(keys)).not.toContain(token);
  });

  test('never reveals a key secret again after navigating away and reloading', async ({ page }) => {
    requireAgentMCPBackend();
    const label = `E2E Key ${Date.now()}`;
    const token = await createKeyViaUi(page, label);

    // Leave the one-time reveal response and come back through a plain GET...
    await page.goto(SETTINGS(agentId));
    await expect(page.getByTestId('agent-mcp-key-secret')).toHaveCount(0);
    await expect(page.locator('body')).not.toContainText(token);

    // ...and reload that GET page. The raw secret must be gone for good.
    await page.reload();
    await expect(page.getByTestId('agent-mcp-key-secret')).toHaveCount(0);
    await expect(page.locator('body')).not.toContainText(token);

    // The key itself survives; only its secret is unrecoverable.
    await expect(keyRow(page, label)).toBeVisible();
    await expect(page.getByTestId('agent-mcp-key-secret')).toHaveCount(0);
  });

  test('rejects a duplicate key label with a readable inline error and creates no second key', async ({ page }) => {
    requireAgentMCPBackend();
    const label = `E2E Key ${Date.now()}`;
    await createKeyViaUi(page, label);
    await closeRevealModal(page);

    // Resubmit the same label: the backend answers 409 and the page re-renders
    // with an inline action error instead of raw JSON.
    await page.locator('#agent-mcp-key-label').fill(label);
    await page.getByTestId('agent-mcp-create-key').getByRole('button', { name: 'Create key' }).click();

    const err = page.getByTestId('agent-mcp-action-error');
    await expect(err).toBeVisible();
    await expect(err).toContainText(/could not create the key/i);
    await expect(err).toContainText(/already exists/i);
    await expect(page.getByTestId('agent-mcp-key-secret')).toHaveCount(0);

    // Exactly one key with that label exists — the duplicate was rejected.
    await page.goto(SETTINGS(agentId));
    await expect(keyRow(page, label)).toHaveCount(1);
    const keys = await endpointKeys(page);
    expect(keys.filter((k) => k.label === label)).toHaveLength(1);
  });

  test('rotates a key, shows the new secret exactly once, and preserves the key identity', async ({ page }) => {
    requireAgentMCPBackend();
    const label = `E2E Key ${Date.now()}`;
    const firstToken = await createKeyViaUi(page, label);
    await closeRevealModal(page);

    const before = (await endpointKeys(page)).find((k) => k.label === label);
    expect(before, 'the created key must be listed before rotation').toBeTruthy();

    await confirmRowAction(page, `Rotate key ${label}`, 'Rotate key', /mcp-endpoint\/keys\/.+\/rotate/);
    // Rotation renders the new one-time secret directly in the response.
    const secret = page.getByTestId('agent-mcp-key-secret');
    await expect(secret).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Key rotated' })).toBeVisible();
    const rotatedToken = ((await secret.textContent()) ?? '').trim();
    expect(rotatedToken, 'rotation must issue a new secret').not.toBe('');
    expect(rotatedToken).not.toBe(firstToken);

    // Same key identity, one secret reveal only.
    const after = (await endpointKeys(page)).find((k) => k.label === label);
    expect(after, 'the key must still exist after rotation').toBeTruthy();
    expect(after?.id).toBe(before?.id);

    await page.goto(SETTINGS(agentId));
    await expect(page.getByTestId('agent-mcp-key-secret')).toHaveCount(0);
    await expect(page.locator('body')).not.toContainText(rotatedToken);
    await page.reload();
    await expect(page.getByTestId('agent-mcp-key-secret')).toHaveCount(0);
    await expect(keyRow(page, label)).toBeVisible();
  });

  test('revokes one key while another key stays active', async ({ page }) => {
    requireAgentMCPBackend();
    const doomed = `E2E Key doom ${Date.now()}`;
    const survivor = `E2E Key keep ${Date.now()}`;
    await createKeyViaUi(page, doomed);
    await closeRevealModal(page);
    await createKeyViaUi(page, survivor);
    await closeRevealModal(page);

    await confirmRowAction(page, `Revoke key ${doomed}`, 'Revoke key', /keyRevoked=1/);

    // The revoked key is marked revoked and read-only...
    const revokedRow = keyRow(page, doomed);
    await expect(revokedRow).toBeVisible();
    await expect(revokedRow.getByText('revoked', { exact: true })).toBeVisible();
    await expect(revokedRow.getByRole('button', { name: `Rotate key ${doomed}` })).toHaveCount(0);
    await expect(revokedRow.getByRole('button', { name: `Revoke key ${doomed}` })).toHaveCount(0);

    // ...while the other key is untouched and still actionable.
    const survivorRow = keyRow(page, survivor);
    await expect(survivorRow).toBeVisible();
    await expect(survivorRow.getByText('active', { exact: true })).toBeVisible();
    await expect(survivorRow.getByRole('button', { name: `Rotate key ${survivor}` })).toBeVisible();
    await expect(survivorRow.getByRole('button', { name: `Revoke key ${survivor}` })).toBeVisible();

    // The endpoint is still up: only one of the two keys was revoked.
    await expect(page.getByTestId('agent-mcp-endpoint')).toBeVisible();
  });

  test('project shares page offers no agent picker (superseded feature guard)', async ({ page }) => {
    // The project-share surface is for tool-scoped shares, not agent selection:
    // agent sharing lives on the agent's own Settings page. A regression that
    // reintroduced an agent picker here would break the agent-owned endpoint
    // model, so assert its absence structurally.
    await page.goto(SHARES_NEW);
    await expectAppPage(page, /New MCP share/);
    await expect(page.locator('#mcp-share-name')).toBeVisible();
    await expect(page.locator('[data-mcp-share-form-fields]')).toBeVisible();

    const form = page.locator('[data-mcp-share-form-fields]');
    await expect(form.locator('select')).toHaveCount(0);
    await expect(form.locator('[name*="agent" i]')).toHaveCount(0);

    // The only agent affordance is a link out to the agent surface.
    await page.goto(SHARES);
    await expect(page.getByRole('link', { name: 'Manage an agent endpoint' })).toHaveAttribute('href', '/agents');
  });

  test('revoking the endpoint returns the section to its not-enabled state', async ({ page }) => {
    requireAgentMCPBackend();
    expect(endpointId, 'an endpoint must exist to revoke').toBeTruthy();
    await page.goto(SETTINGS(agentId));
    await expect(page.getByTestId('agent-mcp-endpoint')).toBeVisible();

    await confirmRowAction(
      page,
      "Revoke this agent's MCP endpoint",
      'Revoke endpoint',
      /mcpRevoked=1/,
    );

    await expect(page.getByTestId('agent-mcp-endpoint')).toHaveCount(0);
    await expect(page.getByTestId('agent-mcp-create-endpoint')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Create MCP endpoint' })).toBeVisible();
    // Revocation invalidates every key with it, so the keys panel is gone too.
    await expect(page.getByTestId('agent-mcp-keys')).toHaveCount(0);
  });
});
