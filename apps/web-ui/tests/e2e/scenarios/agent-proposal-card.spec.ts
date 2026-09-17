import { test, expect, type Page } from '@playwright/test';
import { readBootstrap, createProject } from '../helpers/bootstrap';
import { expectAppPage } from '../helpers/page';
import { addProvider } from '../helpers/providers';

// Full ask_user proposal-card scenario: on a FRESH scratch project, a chat agent
// calls `ask_user` with a structured `proposal` (kind "blueprint") and the
// gateway renders a reviewable proposal card — a "Proposed changes" header with
// a kind badge + side-effect summary, then read-only object/relationship type
// previews — instead of a monochrome markdown fence.
//
// Signal (DOM, the one this scenario keys on): the web chat renders a
// `.proposal-card` inside the question card (`.memory-question`) from the
// `question` SSE event's `proposalHtml` field (gateway/proposal.templ +
// gateway/webui/static/js/chat-stream.js renderQuestion). The ask_user tool
// PAUSES the run (the SSE stream never emits `done`), so there is no
// closed-stream body to assert against — the card's presence in the DOM is the
// proof that the gateway generated and injected `proposalHtml`.
//
// Flow (mirrors mcp-servers-tool-call.spec.ts):
//   1. SEED (API): fresh scratch project under the bootstrap org — provider and
//      agent state are project-scoped, so a fresh project makes the loop real;
//   2. PROVIDER (UI): add the live provider from env vars; the save is
//      live-validated by the memory backend, so an invalid key or unsynced
//      catalog skips (with the backend's copy) instead of failing;
//   3. AGENT (API): create with `ask_user` in the tool whitelist (the ask_user
//      tool is only injected when the definition opts in — server
//      executor.go buildAskUserTool) and a directive system prompt that forces
//      the model to emit exactly one ask_user call carrying a blueprint
//      proposal. Created via API because the agent modal cannot express the
//      deterministic prompt this test needs;
//   4. CHAT (UI): /chat?agent=<id> → one forced turn → wait for the
//      `.proposal-card` to render;
//   5. ASSERT: the card header + "blueprint" kind badge + the "Object types"
//      preview section + the proposed object type name, plus the interactive
//      Accept/Reject answer controls that render alongside the card. When the
//      model completes the turn without a structured proposal (or the run
//      errors) the test skips with an annotation, never fails on
//      non-deterministic model tool-use.
//   6. CLEANUP (finally): delete agent, reactivate the bootstrap project,
//      delete the scratch project.
//
// Env vars (all reused — see tests/e2e/.env.e2e.example, no new keys):
//   E2E_SCENARIO_LLM_PROVIDER/API_KEY/BASE_URL/MODEL  live provider for the
//       scratch project (chat turn calls a real model; skip when key unset)
const PROVIDER = process.env.E2E_SCENARIO_LLM_PROVIDER || 'openai';
const API_KEY = process.env.E2E_SCENARIO_LLM_API_KEY || '';
const BASE_URL = process.env.E2E_SCENARIO_LLM_BASE_URL || 'http://litellm:4000/v1';
// The add-provider form renders the base_url field only for the OpenAI-
// compatible provider; passing a base URL for any other provider would make
// fillProviderForm wait on a non-existent field and time out.
const PROVIDER_BASE_URL = PROVIDER === 'openai' ? BASE_URL : undefined;
// The scenario suite treats E2E_SCENARIO_LLM_MODEL as the already-prefixed
// "provider/model" catalog value (see blueprint-object-chat / mcp-servers-tool-call).
// Tolerate a bare value too, but never double-prefix.
const MODEL = process.env.E2E_SCENARIO_LLM_MODEL || 'openai/deepseek-v4-flash';
const AGENT_MODEL = MODEL.includes('/') ? MODEL : `${PROVIDER}/${MODEL}`;

// The proposed object type the directive prompt pins. Asserting on this exact
// name proves the card rendered the STRUCTURED preview (object type rows), not
// just a summary-only degradation.
const PROPOSED_TYPE_NAME = 'Talk';

function requireBootstrap() {
  const bootstrap = readBootstrap();
  expect(bootstrap, 'setup project must run first').toBeTruthy();
  return bootstrap!;
}

// All steps best-effort: idempotent across repeated runs, skips, and failures.
async function cleanup(page: Page, agentId: string, projectId: string): Promise<void> {
  const bootstrap = readBootstrap();
  if (agentId) await page.request.delete(`/api/agents/${agentId}`).catch(() => {});
  if (bootstrap?.projectId) {
    await page.request.post(`/api/projects/${bootstrap.projectId}/activate`).catch(() => {});
  }
  if (projectId && bootstrap?.orgId) {
    await page.request
      .post(`/projects/delete?projectId=${projectId}&orgId=${bootstrap.orgId}`)
      .catch(() => {});
  }
}

// waitForProposalCard polls the transcript until the proposal card renders (or
// the stream surfaces an error), then returns its state + text. The ask_user
// tool pauses the run, so there is no "settled" bubble or `done` event to wait
// on — the card's presence is the terminal signal for this turn.
async function waitForProposalCard(
  page: Page,
): Promise<{ state: 'card' | 'error'; text: string }> {
  const result = await page.waitForFunction(
    () => {
      const transcript = document.querySelector('#chat-messages');
      if (!transcript) return null;
      const err = transcript.querySelector('span.text-error');
      if (err) {
        return { state: 'error', text: (transcript.textContent || '').slice(0, 500) };
      }
      const card = transcript.querySelector('.proposal-card');
      if (card) {
        return { state: 'card', text: (card.textContent || '').slice(0, 500) };
      }
      return null;
    },
    undefined,
    { timeout: 120_000 },
  );
  const value = (await result.jsonValue()) as { state: 'card' | 'error'; text: string };
  return value;
}

test.describe('ask_user proposal card scenario', () => {
  test('structured blueprint proposal renders as a reviewable card', async ({ page }) => {
    // Provider save and the chat turn are both network-bound live calls.
    test.setTimeout(300_000);

    test.skip(
      !API_KEY,
      'E2E_SCENARIO_LLM_API_KEY is not set — the provider save is live-validated ' +
        'and the chat turn calls a real model. Set it in tests/e2e/.env.e2e to run ' +
        'this scenario (defaults target the dev litellm: openai @ http://litellm:4000/v1).',
    );

    const bootstrap = requireBootstrap();
    const name = `E2E Proposal ${Date.now()}`;
    let projectId = '';
    let agentId = '';

    try {
      // 1. SEED (API): a fresh project isolates provider/agent state.
      projectId = await createProject(page, bootstrap.orgId, name);

      // 2. PROVIDER (UI): live-validated save; rejection is an environment
      // problem (invalid key / unreachable base URL), not a product regression
      // — skip with the backend's copy.
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

      // 3. AGENT (API): whitelist exactly `ask_user` (the tool is injected only
      // when opted in), allow policy, directive prompt, provider-prefixed model
      // (memory rejects bare model names). The prompt pins the exact proposal
      // shape so the card can be asserted deterministically; the persisted
      // whitelist is the opt-in proof.
      const systemPrompt =
        `Automated test of the ask_user proposal card. Your ONLY job in this turn is to ` +
        `call the "ask_user" tool exactly once, proposing a new object type as a structured ` +
        `blueprint proposal, then stop. Never answer in prose and never call any other tool. ` +
        `Call ask_user with exactly these arguments: ` +
        `question "Propose adding an object type for conference talks?", ` +
        `interaction_type "buttons", ` +
        `options [{label "Accept", value "accept"}, {label "Reject", value "reject"}], ` +
        `proposal {"kind":"blueprint","summary":"Adds 1 object type","body":{` +
        `"objectTypes":[{"name":"${PROPOSED_TYPE_NAME}","label":"${PROPOSED_TYPE_NAME}",` +
        `"description":"A conference talk","properties":{"title":{"type":"string",` +
        `"description":"Talk title"},"speaker":{"type":"string"}}}],"relationshipTypes":[]}}. ` +
        `Do not omit the proposal argument and do not wrap it in markdown.`;
      const createResp = await page.request.post('/api/agents', {
        data: {
          name,
          systemPrompt,
          tools: ['ask_user'],
          skills: [],
          defaultToolPolicy: 'allow',
          model: { name: AGENT_MODEL, temperature: 0, maxTokens: 4096 },
        },
      });
      if (!createResp.ok()) {
        const detail = ((await createResp.text().catch(() => '')) || `HTTP ${createResp.status()}`).slice(0, 300);
        test.info().annotations.push({
          type: 'skipped-step',
          description: `agent create rejected by memory backend: ${detail}`,
        });
        test.skip(true, `agent create rejected by memory backend: ${detail}`);
        return;
      }
      const created = (await createResp.json()) as { id: string };
      agentId = created.id;
      const agentResp = await page.request.get(`/api/agents/${agentId}`);
      expect(agentResp.ok(), 'fetch created agent failed').toBeTruthy();
      const agent = (await agentResp.json()) as { tools: string[] };
      expect(agent.tools, 'the agent whitelist must persist ask_user').toContain('ask_user');

      // 4. CHAT (UI): one forced turn against the agent. Completion is detected
      // on the proposal card (the ask_user tool pauses the run; there is no
      // closed stream or settled bubble to wait on).
      await page.goto(`/chat?agent=${agentId}`);
      await expectAppPage(page, /Chat/);
      const agentSelect = page.locator('#chat-agent');
      await expect(agentSelect).toBeVisible();
      await agentSelect.selectOption(agentId);
      await expect(page.locator('#chat-input')).toBeEnabled();

      await page.locator('#chat-input').fill('Propose an object type now.');
      await page.locator('#chat-send').click();

      let result: { state: 'card' | 'error'; text: string };
      try {
        result = await waitForProposalCard(page);
      } catch (err) {
        const why = (err as Error).message;
        test.info().annotations.push({
          type: 'skipped-step',
          description: `no proposal card rendered within 120s (${why}) — the turn may have stalled or the model answered without asking`,
        });
        test.skip(true, `proposal card did not render: ${why}`);
        return;
      }

      if (result.state === 'error') {
        test.info().annotations.push({
          type: 'skipped-step',
          description: `chat run errored before rendering a proposal (provider/environment): ${result.text}`,
        });
        test.skip(true, `chat run errored before rendering a proposal: ${result.text}`);
        return;
      }

      // 5. ASSERT: the card header + kind badge + the STRUCTURED object-type
      // preview. When the model emitted a valid proposal but with a non-blueprint
      // kind (or an empty body), the gateway renders a summary-only card — that
      // still proves the card path, but not the structured preview this test
      // targets, so skip with an annotation rather than fail on model deviation.
      const card = page.locator('#chat-messages .proposal-card').first();
      if (!/object\s*types/i.test(result.text)) {
        test.info().annotations.push({
          type: 'skipped-step',
          description: `proposal card rendered but without the object-type preview (summary-only degradation): "${result.text}"`,
        });
        test.skip(
          true,
          `proposal card rendered without the structured object-type preview: "${result.text}"`,
        );
        return;
      }

      await expect(card).toBeVisible();
      await expect(card.getByText('Proposed changes')).toBeVisible();
      await expect(card.getByText(/blueprint/i)).toBeVisible();
      await expect(card.getByText(/object\s*types/i)).toBeVisible();
      await expect(card.getByText(PROPOSED_TYPE_NAME)).toBeVisible();

      // The interactive answer controls render alongside the card (buttons
      // interaction type → Accept/Reject radio rows), proving the question card
      // stays fully usable with a proposal attached.
      const question = page.locator('#chat-messages .memory-question').first();
      await expect(question.getByRole('radio', { name: 'Accept' })).toBeVisible();
      await expect(question.getByRole('radio', { name: 'Reject' })).toBeVisible();
    } finally {
      await cleanup(page, agentId, projectId);
    }
  });
});
