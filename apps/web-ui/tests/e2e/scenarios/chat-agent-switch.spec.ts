import { test, expect, type Page } from '@playwright/test';
import { readBootstrap, createProject } from '../helpers/bootstrap';
import { expectAppPage } from '../helpers/page';
import { addProvider } from '../helpers/providers';
import { createAgentViaModal } from '../helpers/agents';
import { sendChatMessage } from '../helpers/chat';

// Chat agent-switch navigation scenario: on a FRESH scratch project, start a
// conversation with agent A, switch to agent B via "New chat", then navigate
// back to A's conversation from the session rail — proving the chat area's
// navigation (agent picker + session-rail resume) round-trips correctly.
//
// Live-LLM and env-gated on E2E_SCENARIO_LLM_API_KEY (same key as the rest of
// the scenarios suite), so the default full run skips fast. The two turns only
// need a short reply; the assertion is about navigation state (which agent is
// selected, which transcript is loaded), not reply content.
//
// Env vars (all reused — see tests/e2e/.env.e2e.example, no new keys):
//   E2E_SCENARIO_LLM_PROVIDER/API_KEY/BASE_URL/MODEL  live provider for the
//       scratch project (each chat turn calls a real model; skip when key unset)
const PROVIDER = process.env.E2E_SCENARIO_LLM_PROVIDER || 'openai';
const API_KEY = process.env.E2E_SCENARIO_LLM_API_KEY || '';
const BASE_URL = process.env.E2E_SCENARIO_LLM_BASE_URL || 'http://litellm:4000/v1';
const MODEL = process.env.E2E_SCENARIO_LLM_MODEL || 'openai/deepseek-v4-flash';
// base_url is rendered only for the OpenAI-compatible provider, so pass it only
// then (mirrors the other live scenarios). AGENT_MODEL prefixes an unprefixed
// MODEL env with the configured provider, so a non-OpenAI run can supply a
// bare model name instead of a hard-coded `openai/` catalog value.
const PROVIDER_BASE_URL = PROVIDER === 'openai' ? BASE_URL : undefined;
const AGENT_MODEL = MODEL.includes('/') ? MODEL : `${PROVIDER}/${MODEL}`;

function requireBootstrap() {
  const bootstrap = readBootstrap();
  expect(bootstrap, 'setup project must run first').toBeTruthy();
  return bootstrap!;
}

// All steps best-effort: idempotent across repeated runs, skips, and failures.
async function cleanup(page: Page, agentIds: string[], projectId: string): Promise<void> {
  const bootstrap = readBootstrap();
  for (const id of agentIds) await page.request.delete(`/api/agents/${id}`).catch(() => {});
  if (bootstrap?.projectId) {
    await page.request.post(`/api/projects/${bootstrap.projectId}/activate`).catch(() => {});
  }
  if (projectId && bootstrap?.orgId) {
    await page.request
      .post(`/projects/delete?projectId=${projectId}&orgId=${bootstrap.orgId}`)
      .catch(() => {});
  }
}

test.describe('Chat agent-switch navigation scenario', () => {
  test('switch agents and resume the first conversation from the session rail', async ({ page }) => {
    // Provider save plus two live chat turns — give the journey headroom.
    test.setTimeout(300_000);

    test.skip(
      !API_KEY,
      'E2E_SCENARIO_LLM_API_KEY is not set — the provider save is live-validated ' +
        'and each chat turn calls a real model. Set it in tests/e2e/.env.e2e to run ' +
        'this scenario (defaults target the dev litellm: openai @ http://litellm:4000/v1).',
    );

    const bootstrap = requireBootstrap();
    // Project and agents share one unique name, so failures are easy to spot in
    // the org's lists even if cleanup is interrupted.
    const name = `E2E Chat Nav ${Date.now()}`;
    const agentIds: string[] = [];
    let projectId = '';

    try {
      // 1. SEED (API): a fresh project isolates the provider + agents and keeps
      // the session rail empty of anything but this test's two conversations.
      // createProject also activates the project for the session.
      projectId = await createProject(page, bootstrap.orgId, name);

      // 2. PROVIDER (UI): the memory backend live-validates the save (catalog
      // must know the provider; the key must be usable against the base URL). A
      // rejection is an environment problem, not a product regression — skip
      // with the backend's copy so a dev-memory hiccup never reddens the suite.
      const saved = await addProvider(page, PROVIDER, API_KEY, PROVIDER_BASE_URL);
      if (saved !== 'saved') {
        let detail = "couldn't save provider";
        const modal = page.locator('#provider-save-error-modal');
        if (await modal.isVisible().catch(() => false)) {
          const reason = await modal.locator('p').last().textContent().catch(() => null);
          if (reason?.trim()) detail = reason.trim();
        }
        test.skip(
          true,
          `provider save rejected by memory backend (catalog unsynced or invalid key): ${detail}`,
        );
        return;
      }

      // 3. AGENTS (UI): two agents in the scratch project. Both carry the same
      // explicit model; each will own one conversation in the rail.
      const agentA = await createAgentViaModal(page, `${name} A`, AGENT_MODEL, '*');
      agentIds.push(agentA);
      const agentB = await createAgentViaModal(page, `${name} B`, AGENT_MODEL, '*');
      agentIds.push(agentB);

      // 4. CHAT WITH A (UI): pick A in the welcome hero and complete one turn.
      // The fresh workspace has no conversation, so the empty state (and its
      // #chat-agent picker) is visible.
      await page.goto('/chat');
      await expectAppPage(page, /Chat/);
      await page.locator('#chat-agent').selectOption(agentA);
      await sendChatMessage(page, 'Hello from A.');

      // The first message must "enter" the freshly-created conversation: the URL
      // flips to ?c=<conversationId> immediately — not only on a later resume.
      await expect(page).toHaveURL(/\/chat\?c=/);

      // A's conversation lands in the session rail and is the active row.
      const rowA = page.locator(`[data-testid="session-row"][data-agent="${agentA}"]`);
      await expect(rowA).toBeVisible();
      await expect(rowA).toHaveAttribute('data-active', 'true');

      // 5. SWITCH TO B (UI): "New chat" resets the workspace back to the empty
      // state (whose #chat-agent picker is the only agent switch), then pick B
      // and complete a second turn. A's conversation stays in the rail.
      await page.getByTestId('new-chat').click();
      await expect(page.locator('#chat-agent')).toBeVisible();
      await page.locator('#chat-agent').selectOption(agentB);
      await sendChatMessage(page, 'Hello from B.');

      // Both conversations now coexist in the rail, each tagged with its agent.
      const rowB = page.locator(`[data-testid="session-row"][data-agent="${agentB}"]`);
      await expect(rowB).toBeVisible();
      await expect(rowA).toBeVisible();

      // 6. COME BACK TO A (UI): resume A's conversation from the session rail.
      // resumeConversation switches the picker back to A and loads A's transcript.
      await rowA.click();

      await expect(page.locator('#chat-agent')).toHaveValue(agentA);
      await expect(page).toHaveURL(/\/chat\?c=/);
      await expect(rowA).toHaveAttribute('data-active', 'true');
      // A's transcript (not B's) is loaded in the workspace.
      await expect(page.locator('#chat-messages')).toContainText('Hello from A.');
      await expect(page.locator('#chat-messages')).not.toContainText('Hello from B.');
    } finally {
      await cleanup(page, agentIds, projectId);
    }
  });
});
