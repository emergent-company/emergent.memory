import { test, expect, type Page } from '@playwright/test';
import { goto } from '../../helpers/page';
import { expectTokenAuthenticated } from '../../helpers/tokens';

// MCP Sharing — duplicate (copy-and-start-editing) flow:
// gateway/mcp_shares_handlers.go uiMCPShareDuplicatePage + mcp_shares.templ.
//
// "Duplicate share" on an existing row opens the CREATE form pre-filled with
// that share's name ("<name> (copy)"), description and tool selection, so a new
// share starts from a known grant instead of a blank form. Submitting is an
// ordinary create: it mints a NEW key and leaves the source share — and its key
// — untouched.
//
// The source share is made through the JSON API so the tool selection is
// deterministic; everything is revoked in `finally` so the shared bootstrap
// tenant is never left with live shares.

const SHARES = '/settings/mcp-servers/shares';

type CreatedShare = { instance: { id: string }; token: string };

/** Read the one-time key out of the reveal modal. */
async function revealToken(page: Page): Promise<string> {
  const secret = page.locator('#mcp-share-key');
  await expect(secret).toBeVisible();
  const token = ((await secret.inputValue()) ?? '').trim();
  expect(token, 'the reveal must carry a non-empty raw key').not.toBe('');
  return token;
}

test('MCP share duplicate: prefilled create form, new key, source untouched', async ({ page }) => {
  const stamp = Date.now();
  const sourceName = `E2E Dup Source ${stamp}`;
  // Derived up front so the `finally` can clean the copy by name even if the
  // test fails between creating it and reading its row id.
  const copyName = `${sourceName} (copy)`;
  const description = 'source grant for the duplicate flow';
  const ids: string[] = [];

  try {
    // --- catalog: pick two tools so the prefill is measurable ---
    const catalogResp = await page.request.get('/api/mcp-shares/tools');
    expect(catalogResp.ok()).toBeTruthy();
    const catalog = (await catalogResp.json()) as
      | { tools?: Array<{ name: string }> }
      | Array<{ name: string }>;
    const catalogTools = (Array.isArray(catalog) ? catalog : (catalog.tools ?? []))
      .map((t) => t.name)
      .filter(Boolean);
    if (catalogTools.length === 0) {
      test.skip(true, 'memory tool catalog is empty — cannot create a share to duplicate');
    }
    const tools = catalogTools.slice(0, 2);

    // --- source share via the API ---
    const createResp = await page.request.post('/api/mcp-shares', {
      data: { name: sourceName, description, tools },
    });
    expect(createResp.ok(), `create source share: ${createResp.status()}`).toBeTruthy();
    const created = (await createResp.json()) as CreatedShare;
    const sourceId = created.instance.id;
    const sourceToken = created.token;
    ids.push(sourceId);

    // --- duplicate from the row's ⋯ menu ---
    await goto(page, SHARES);
    const row = page.locator(`[data-mcp-share-row="${sourceId}"]`);
    await expect(row).toBeVisible();
    await page.locator(`[data-testid="mcp-share-actions-${sourceId}"]`).click();
    await page.locator(`[data-testid="mcp-share-duplicate-link-${sourceId}"]`).click();

    await page.waitForURL(new RegExp(`/shares/${sourceId}/duplicate$`));
    // The page announces itself as a duplicate, not a blank create.
    await expect(page).toHaveTitle(/Duplicate MCP share/);

    // --- the draft is the source share ---
    await expect(page.locator('#mcp-share-name')).toHaveValue(copyName);
    await expect(page.locator('#mcp-share-description')).toHaveValue(description);
    const checked = page.locator('[data-mcp-tool-option] input[name="tools"]:checked');
    await expect(checked).toHaveCount(tools.length);
    for (const tool of tools) {
      await expect(
        page.locator(`[data-mcp-tool-option] input[name="tools"][value="${tool}"]`),
      ).toBeChecked();
    }
    // Invite recipients are never carried over: a copy means a new key.
    await expect(page.locator('#mcp-share-emails')).toHaveValue('');

    // --- create the copy: a fresh key, its own row ---
    await page.getByRole('button', { name: 'Create share' }).click();
    const copyToken = await revealToken(page);
    expect(copyToken).not.toBe(sourceToken);

    await page.getByRole('button', { name: "Done — I've saved the key" }).click();
    const copyRow = page.locator('[data-mcp-share-row]').filter({ hasText: copyName }).first();
    await expect(copyRow).toBeVisible();
    const copyId = (await copyRow.getAttribute('data-mcp-share-row')) ?? '';
    expect(copyId, 'the copy row must carry its id').not.toBe('');
    ids.push(copyId);

    // --- the source share is untouched (same name, tools, and key) ---
    const listResp = await page.request.get('/api/mcp-shares');
    expect(listResp.ok()).toBeTruthy();
    const shares = (await listResp.json()) as Array<{
      id: string;
      name: string;
      tools: string[];
      toolCount: number;
    }>;
    const source = shares.find((s) => s.id === sourceId);
    expect(source, 'the source share must still exist').toBeTruthy();
    expect(source?.name).toBe(sourceName);
    expect(source?.toolCount).toBe(tools.length);
    // Its original key still authenticates: a duplicate mints a new key, it
    // never rotates the source.
    await expectTokenAuthenticated(page.request, sourceToken);
  } finally {
    // The copy's id is only known once its row renders; sweep by name as well so
    // a failure between "Create share" and that lookup cannot leak it.
    try {
      const resp = await page.request.get('/api/mcp-shares');
      if (resp.ok()) {
        for (const s of (await resp.json()) as Array<{ id: string; name: string }>) {
          if (s.name === copyName) ids.push(s.id);
        }
      }
    } catch {
      /* best effort: the ids below still remove the shares we know about */
    }
    for (const id of new Set(ids)) {
      await page.request.delete(`/api/mcp-shares/${id}`).catch(() => {});
    }
  }
});
