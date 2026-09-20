import { test, expect } from '@playwright/test';
import { createAgentViaModal } from '../../helpers/agents';

// Per-agent sandbox (workspace) config page: gateway/agent.templ
// `agentSandboxForm` → POST /agents/:id/sandbox/update (agent.go
// `uiAgentSandboxUpdate`). The form persists the enable switch, base image,
// repo source (type/url/branch), tool allowlist, resource limits, setup
// commands and env vars onto the agent's `AgentSandboxConfig`, then PRG-
// redirects to `/agents/:id/sandbox?updated=1`.
//
// The spec drives the full save → reload round-trip and asserts the resulting
// field values (not a transient toast), matching the suite's established
// "assert resulting state" pattern. It creates one scratch agent and deletes
// it via API in `finally`, so the shared bootstrap tenant is never polluted.
//
// Env vars are rendered sorted (agent.go `sandboxEnvVars`), so a single
// KEY=VALUE is used to keep the round-trip assertion deterministic. The
// provider select is left at its default ("Auto") — the available providers
// depend on the live backend, so asserting a specific provider would be
// brittle.

const SANDBOX = (id: string) => `/agents/${encodeURIComponent(id)}/sandbox`;

test('sandbox config persists across a full reload', async ({ page }) => {
  const name = `E2E Sandbox ${Date.now()}`;
  const id = await createAgentViaModal(page, name);

  const baseImage = 'memory-workspace:test';
  const repoUrl = 'https://github.com/example/e2e-repo';
  const repoBranch = 'main';

  try {
    await page.goto(SANDBOX(id));

    // The form is present and starts disabled with no repo source.
    await expect(page.locator('#agent-sandbox-enabled')).toBeVisible();

    // Enable the sandbox and fill every persisted field.
    await page.locator('#agent-sandbox-enabled').check();
    await page.locator('#agent-sandbox-base-image').fill(baseImage);

    // Repo source: fixed repository + URL + branch.
    await page.locator('#agent-sandbox-repo-type').selectOption('fixed');
    await page.locator('#agent-sandbox-repo-url').fill(repoUrl);
    await page.locator('#agent-sandbox-repo-branch').fill(repoBranch);

    // Restrict the tool allowlist to a single tool (empty means "all allowed").
    await page.locator('input[name="sandboxTool"][value="bash"]').check();

    // Resource limits + setup/env.
    await page.locator('#agent-sandbox-cpu').fill('2');
    await page.locator('#agent-sandbox-memory').fill('4G');
    await page.locator('#agent-sandbox-disk').fill('10G');
    await page.locator('#agent-sandbox-setup').fill('echo hello\npwd');
    await page.locator('#agent-sandbox-env').fill('FOO=bar');

    await page.getByRole('button', { name: 'Save changes' }).click();

    // PRG redirect on success carries ?updated=1.
    await page.waitForURL(/\/sandbox\?updated=1/);

    // Every submitted value survives a full reload (server round-trip, not
    // client state).
    await page.reload();
    await expect(page.locator('#agent-sandbox-enabled')).toBeChecked();
    await expect(page.locator('#agent-sandbox-base-image')).toHaveValue(baseImage);
    await expect(page.locator('#agent-sandbox-repo-type')).toHaveValue('fixed');
    await expect(page.locator('#agent-sandbox-repo-url')).toHaveValue(repoUrl);
    await expect(page.locator('#agent-sandbox-repo-branch')).toHaveValue(repoBranch);
    await expect(page.locator('input[name="sandboxTool"][value="bash"]')).toBeChecked();
    await expect(page.locator('#agent-sandbox-cpu')).toHaveValue('2');
    await expect(page.locator('#agent-sandbox-memory')).toHaveValue('4G');
    await expect(page.locator('#agent-sandbox-disk')).toHaveValue('10G');
    await expect(page.locator('#agent-sandbox-setup')).toHaveValue('echo hello\npwd');
    await expect(page.locator('#agent-sandbox-env')).toHaveValue('FOO=bar');
  } finally {
    await page.request.delete(`/api/agents/${id}`).catch(() => {});
  }
});

test('fixed repo source without a URL is rejected', async ({ page }) => {
  const name = `E2E Sandbox Err ${Date.now()}`;
  const id = await createAgentViaModal(page, name);

  try {
    await page.goto(SANDBOX(id));

    // "fixed" requires a repository URL (agent.go uiAgentSandboxUpdate); an
    // empty URL redirects back with a flash error rather than persisting.
    await page.locator('#agent-sandbox-repo-type').selectOption('fixed');
    await page.getByRole('button', { name: 'Save changes' }).click();

    // The errored save redirects back with ?err= (ui.go redirectWithError) —
    // no ?updated=1 — and the source stays unset after a reload.
    await page.waitForURL(/\/sandbox\?err=/);
    await expect(page).not.toHaveURL(/updated=1/);

    await page.reload();
    await expect(page.locator('#agent-sandbox-repo-type')).not.toHaveValue('fixed');
  } finally {
    await page.request.delete(`/api/agents/${id}`).catch(() => {});
  }
});
