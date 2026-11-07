import { test, expect } from '@playwright/test';

// Disabled-Connect branch (gateway/mcp_servers.templ `renderInspect`): when an
// MCP server requires OAuth authorization but its provider publishes no
// discoverable OAuth metadata, inspect renders the token-header remedy plus a
// disabled Connect button — and no enabled `/oauth/start` link. This spec runs
// against the hermetic dev-mode mock harness (mcp-inspect.config.ts), where
// mock-memory.mjs seeds one outbound http server whose inspect answers
// `auth_required` + `unavailable`.

test('inspect renders the disabled Connect branch when OAuth metadata is unavailable', async ({ page }) => {
  await page.goto('/settings/mcp-servers');

  const row = page.locator('[data-mcp-row]', { hasText: 'unavailable-oauth-server' });
  await expect(row).toBeVisible();

  // The Inspect action lives inside the collapsed <details> body.
  await row.locator('summary').click();
  const inspectBtn = row.getByRole('button', { name: 'Inspect unavailable-oauth-server' });
  await expect(inspectBtn).toBeVisible();
  await inspectBtn.click();

  const area = page.locator('[data-mcp-inspect-area="mcp-unavailable"]');
  await expect(area).toBeVisible();

  // The disabled Connect control renders and is actually disabled.
  const disabledBtn = area.getByTestId('mcp-oauth-connect-disabled');
  await expect(disabledBtn).toBeVisible();
  await expect(disabledBtn).toBeDisabled();

  // No enabled Connect link is offered (no /oauth/start navigation).
  await expect(area.getByTestId('mcp-oauth-connect')).toHaveCount(0);

  // The token-header remedy is surfaced.
  await expect(area).toContainText('token header');
});

test('expanding a row fetches and renders the deferred cached tool rows', async ({ page }) => {
  // The list page ships a pending placeholder; mcpServersScript fetches the
  // fragment on the first expand (GET /settings/mcp-servers/:id/tools), and the
  // gateway reads the server detail from mock-memory.mjs's new
  // GET /api/admin/mcp-servers/:id route. This is the browser proof that the
  // deferred path renders rows instead of landing on the fragment's error note.
  await page.goto('/settings/mcp-servers');

  const row = page.locator('[data-mcp-row]', { hasText: 'unavailable-oauth-server' });
  await expect(row).toBeVisible();

  const host = row.locator('[data-mcp-tools-host="mcp-unavailable"]');
  // Collapsed <details> hides the body, so assert attachment rather than
  // visibility: the placeholder exists but is not rendered until expand.
  await expect(host.locator('[data-mcp-tools-pending]')).toHaveCount(1);

  await row.locator('summary').click();

  // The placeholder is replaced by the fetched container…
  await expect(host.locator('[data-mcp-tools-pending]')).toHaveCount(0);
  // …with one row per cached tool from the mock detail response.
  const toolRows = host.locator('[data-mcp-tool-row]');
  await expect(toolRows).toHaveCount(2);
  await expect(toolRows.filter({ hasText: 'alpha_tool' })).toBeVisible();
  await expect(toolRows.filter({ hasText: 'beta_tool' })).toBeVisible();
});
