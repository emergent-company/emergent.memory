import { Page, expect } from '@playwright/test';

/**
 * Create an agent through the real UI (Agents page → "New agent" modal) and
 * return the new agent's id. Fills the name and, when `model` is given,
 * selects it in the #agent-model catalog dropdown (options are grouped by
 * provider; the first, empty option is "Auto — default model"). The `model`
 * argument must be the prefixed `provider/model` string (e.g.
 * `openai/deepseek-v4-flash`) — that is the value the modal's options carry
 * and the string submitted/stored by the agent create flow. When `tools` is
 * given (comma-separated names, or "*" for the full whitelist), fills the
 * modal's #agent-tools field — an agent with an EMPTY tools list is treated
 * by memory as having no tool access at all (only coordination tools are
 * injected), so chat-about-object turns need real tools. Submits and reads
 * the id back from the reloaded agents card's data-href.
 */
export async function createAgentViaModal(page: Page, name: string, model?: string, tools?: string): Promise<string> {
  await page.goto('/agents');

  await page.getByRole('button', { name: 'New agent' }).first().click();
  await expect(page.locator('#agent-name')).toBeVisible();
  await page.locator('#agent-name').fill(name);

  if (tools !== undefined) {
    await page.locator('#agent-tools').fill(tools);
  }

  if (model !== undefined) {
    const modelSelect = page.locator('#agent-model');
    await modelSelect.waitFor({ state: 'visible' });
    const optionValues = await modelSelect.locator('option').evaluateAll((opts) =>
      opts.map((o) => (o as HTMLOptionElement).value),
    );
    if (!optionValues.includes(model)) {
      throw new Error(
        `createAgentViaModal: model "${model}" is not in the #agent-model catalog ` +
          `[available: ${optionValues.join(', ')}] — the provider catalog is not synced`,
      );
    }
    await modelSelect.selectOption(model);
  }

  await page.getByRole('button', { name: 'Create agent' }).click();

  // The agents list is responsive: desktop (md+) renders agentsTable rows as a
  // real <a href="/agents/<id>"> link; below md agentCardGrid cards carry
  // data-href but are md:hidden on desktop. e2e runs Desktop Chrome (md+), so
  // the visible target is the table-row link — read the id from its href.
  // (The mobile card's only <a> is the "Chat with <name>" affordance,
  // /chat?agent=<id>, which never matches the /agents/ prefix.)
  // Scope to #main-content: the spotlight/command palette renders its own
  // a[href^="/agents/<id>"] rows earlier in DOM order, so an unscoped .first()
  // can pick a hidden palette row instead of the visible list row.
  const rowLink = page.locator('#main-content a[href^="/agents/"]').filter({ hasText: name }).first();
  await expect(rowLink).toBeVisible();
  const href = await rowLink.getAttribute('href');
  if (!href) {
    throw new Error(`createAgentViaModal: the agents-list row for "${name}" has no href`);
  }
  const id = new URL(href, 'http://localhost').pathname.split('/').pop();
  if (!id) {
    throw new Error(`createAgentViaModal: could not parse an agent id from href "${href}"`);
  }
  return id;
}
