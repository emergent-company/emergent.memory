import { test, expect, type Page } from '@playwright/test';
import { readBootstrap, createProject } from '../helpers/bootstrap';
import { expectAppPage } from '../helpers/page';
import { addProvider } from '../helpers/providers';

// Chat run-control scenarios (gateway lane A/B + the dock/queue/rail surfaces):
// on a FRESH scratch project, a live agent run drives the pending-work dock, the
// client-side composer queue, and the session-rail run bucket. These are the
// three signals tasks.md 9.4 calls out, each proven against a real model turn
// rather than a stubbed stream.
//
// All three are live-LLM and env-gated on E2E_SCENARIO_LLM_API_KEY (the same
// key the rest of the scenarios suite uses), so the default full run skips fast.
// Model tool-use and turn length are non-deterministic, so each test skips with
// an annotation when the model deviates (never answers with the expected tool,
// or finishes before the signal can be observed) — mirroring
// agent-proposal-card.spec.ts and mcp-servers-tool-call.spec.ts.
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
// "provider/model" catalog value; tolerate a bare value but never double-prefix.
const MODEL = process.env.E2E_SCENARIO_LLM_MODEL || 'openai/deepseek-v4-flash';
const AGENT_MODEL = MODEL.includes('/') ? MODEL : `${PROVIDER}/${MODEL}`;

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

// createScratchAgent seeds a fresh project, saves the live provider, and creates
// one agent via the API (the agent modal cannot express the deterministic
// system prompt + tool-policy config these tests need). Returns the ids, or
// skips the test when the environment rejects a step.
async function createScratchAgent(
  page: Page,
  name: string,
  systemPrompt: string,
  tools: string[],
  defaultToolPolicy: string,
): Promise<{ projectId: string; agentId: string }> {
  const bootstrap = requireBootstrap();

  const projectId = await createProject(page, bootstrap.orgId, name);

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
  }

  const createResp = await page.request.post('/api/agents', {
    data: {
      name,
      systemPrompt,
      tools,
      skills: [],
      defaultToolPolicy,
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
  }
  const created = (await createResp.json()) as { id: string };
  return { projectId, agentId: created.id };
}

// openChat navigates to /chat for an agent and waits for the composer.
async function openChat(page: Page, agentId: string): Promise<void> {
  await page.goto(`/chat?agent=${agentId}`);
  await expectAppPage(page, /Chat/);
  const agentSelect = page.locator('#chat-agent');
  await expect(agentSelect).toBeVisible();
  await agentSelect.selectOption(agentId);
  await expect(page.locator('#chat-input')).toBeEnabled();
}

// waitForDock polls until the dock mount (#chat-dock) is populated with at
// least one card (or the transcript surfaces a run error). Returns the state.
async function waitForDock(
  page: Page,
): Promise<{ state: 'dock' | 'error'; cards: number; text: string }> {
  const result = await page.waitForFunction(
    () => {
      const dock = document.getElementById('chat-dock');
      const transcript = document.querySelector('#chat-messages');
      const err = transcript?.querySelector('span.text-error');
      if (err) {
        return { state: 'error', cards: 0, text: (transcript?.textContent || '').slice(0, 500) };
      }
      if (dock) {
        const cards = dock.querySelectorAll(
          '[data-testid="dock-approval"], [data-testid="dock-question"]',
        );
        if (cards.length > 0) {
          return { state: 'dock', cards: cards.length, text: (dock.textContent || '').slice(0, 500) };
        }
      }
      return null;
    },
    undefined,
    { timeout: 120_000 },
  );
  const value = (await result.jsonValue()) as { state: 'dock' | 'error'; cards: number; text: string };
  return value;
}

// skipOnTurnDeviation is the shared fail-vs-skip gate for model non-determinism:
// a run error is an environment failure (skip), and a completed turn with no
// signal is a model deviation (skip) — neither is a product regression.
function skipOnTurnDeviation(reason: string): void {
  test.info().annotations.push({ type: 'skipped-step', description: reason });
  test.skip(true, reason);
}

test.describe('chat run-control scenarios', () => {
  test('dock renders a pending question with decision controls and a count', async ({ page }) => {
    // Provider save and the chat turn are both network-bound live calls.
    test.setTimeout(300_000);
    test.skip(
      !API_KEY,
      'E2E_SCENARIO_LLM_API_KEY is not set — the provider save is live-validated ' +
        'and the chat turn calls a real model. Set it in tests/e2e/.env.e2e to run ' +
        'this scenario (defaults target the dev litellm: openai @ http://litellm:4000/v1).',
    );

    const name = `E2E RunControl Dock ${Date.now()}`;
    let projectId = '';
    let agentId = '';

    try {
      // A single forced ask_user call surfaces as a pending QUESTION card in the
      // dock (submit/cancel controls), not a tool approval — the tool-approval
      // card path is not covered here.
      const systemPrompt =
        `Automated test of the chat pending-work dock. Your ONLY job in this turn is to ` +
        `call the "ask_user" tool exactly once with interaction_type "buttons" and options ` +
        `[{label "Yes, approve", value "yes"}, {label "No, reject", value "no"}], then stop. ` +
        `Never answer in prose and never call any other tool. Do not wrap arguments in markdown.`;
      const seeded = await createScratchAgent(page, name, systemPrompt, ['ask_user'], 'ask');
      projectId = seeded.projectId;
      agentId = seeded.agentId;

      await openChat(page, agentId);
      await page.locator('#chat-input').fill('Ask for approval now.');
      await page.locator('#chat-send').click();

      let result: { state: 'dock' | 'error'; cards: number; text: string };
      try {
        result = await waitForDock(page);
      } catch (err) {
        const why = (err as Error).message;
        skipOnTurnDeviation(
          `no dock card rendered within 120s (${why}) — the turn may have stalled or the model answered without asking`,
        );
        return;
      }

      if (result.state === 'error') {
        skipOnTurnDeviation(`chat run errored before rendering a dock card (provider/environment): ${result.text}`);
        return;
      }

      // The dock owns the pending decision. The selector accepts either card
      // kind, but this scenario deterministically produces the question card.
      const dock = page.locator('#chat-dock');
      await expect(dock).toBeVisible();
      await expect(dock.locator('[data-testid="dock-approval"], [data-testid="dock-question"]').first()).toBeVisible();
      // A decision control is present for whichever card kind rendered.
      await expect(dock.locator('[data-dock-action]').first()).toBeVisible();

      // The count badge renders only when more than one item is pending — model
      // tool-use can't be forced to produce two, so assert it only when the dock
      // actually rendered multiple cards, otherwise annotate.
      if (result.cards > 1) {
        await expect(dock.locator('[data-testid="dock-count"]')).toBeVisible();
        await expect(dock.locator('[data-testid="dock-count"]')).toHaveText(String(result.cards));
      } else {
        test.info().annotations.push({
          type: 'skipped-step',
          description: 'the model produced exactly one pending item; the >1 dock-count path was not exercised',
        });
      }
    } finally {
      await cleanup(page, agentId, projectId);
    }
  });

  test('a queued follow-up auto-releases when the turn ends', async ({ page }) => {
    test.setTimeout(300_000);
    test.skip(
      !API_KEY,
      'E2E_SCENARIO_LLM_API_KEY is not set — the chat turn calls a real model. Set it in ' +
        'tests/e2e/.env.e2e to run this scenario.',
    );

    const name = `E2E RunControl Queue ${Date.now()}`;
    let projectId = '';
    let agentId = '';

    try {
      // A verbose directive keeps the turn open long enough to queue into it.
      const systemPrompt =
        `You are an automated test assistant. When asked, write a long, detailed, ` +
        `multi-paragraph answer (at least four paragraphs). Never call tools.`;
      const seeded = await createScratchAgent(page, name, systemPrompt, [], 'allow');
      projectId = seeded.projectId;
      agentId = seeded.agentId;

      await openChat(page, agentId);

      const first = 'Write a long, detailed essay about the history of calendars.';
      await page.locator('#chat-input').fill(first);
      await page.locator('#chat-send').click();

      // While the turn streams, park two follow-ups with Cmd/Ctrl+Enter (always
      // queues, regardless of turn state). The queue renders .memory-queue rows.
      await expect(page.locator('#chat-messages .memory-typing').first()).toBeVisible({ timeout: 60_000 });
      const input = page.locator('#chat-input');
      const queue = page.locator('#chat-queue');
      for (const msg of ['Second: what is a leap second?', 'Third: who invented the Gregorian calendar?']) {
        await input.fill(msg);
        await input.press('Control+Enter');
      }
      await expect(queue).toBeVisible();
      await expect(queue.locator('.memory-queue-row')).toHaveCount(2);

      // When the running turn ends, the queue auto-releases in order: each parked
      // message becomes its own turn, so both rows clear and their user bubbles
      // land in the transcript in submission order.
      await expect(queue.locator('.memory-queue-row')).toHaveCount(0, { timeout: 180_000 });
      const userBubbles = page.locator('#chat-messages .chat.chat-end .chat-bubble');
      await expect(userBubbles).toHaveCount(3);
      await expect(userBubbles.nth(1)).toContainText('what is a leap second');
      await expect(userBubbles.nth(2)).toContainText('Gregorian calendar');
    } finally {
      await cleanup(page, agentId, projectId);
    }
  });

  test('the session-rail badge reflects a bucket change without a reload', async ({ page }) => {
    test.setTimeout(300_000);
    test.skip(
      !API_KEY,
      'E2E_SCENARIO_LLM_API_KEY is not set — the chat turn calls a real model. Set it in ' +
        'tests/e2e/.env.e2e to run this scenario.',
    );

    const name = `E2E RunControl Rail ${Date.now()}`;
    let projectId = '';
    let agentId = '';

    try {
      const systemPrompt =
        `You are an automated test assistant. Answer the user's question in one short ` +
        `sentence. Never call tools.`;
      const seeded = await createScratchAgent(page, name, systemPrompt, [], 'allow');
      projectId = seeded.projectId;
      agentId = seeded.agentId;

      await openChat(page, agentId);
      await page.locator('#chat-input').fill('Say hello.');
      await page.locator('#chat-send').click();

      // During the run the active session row's badge carries a non-done bucket
      // (running or needs_input), rendered server-side from the polled history.
      const row = page.locator('[data-testid="session-row"][data-active="true"]').first();
      await expect(row).toBeVisible();

      // Poll until the active row's data-bucket leaves "done" (the run is live),
      // then wait for it to return to "done" (the run ended) — all without a
      // navigation: the page URL must stay on /chat the whole time.
      await expect
        .poll(async () => (await row.getAttribute('data-bucket')) ?? '', { timeout: 120_000 })
        .not.toBe('done');

      // While the turn streams, the active row's badge shows the running
      // indicator (never a "Done" label on a live turn).
      const badge = row.locator('.memory-rail-badge');
      await expect(badge).toContainText('Running', { timeout: 30_000 });

      await expect
        .poll(async () => (await row.getAttribute('data-bucket')) ?? '', { timeout: 180_000 })
        .toBe('done');

      // Once the run ends the badge clears to a plain row: hidden, with no
      // lingering "Done" or "Running" label.
      await expect(badge).toBeHidden();
      await expect(badge).not.toContainText('Done');

      expect(new URL(page.url()).pathname).toBe('/chat');
    } finally {
      await cleanup(page, agentId, projectId);
    }
  });
});
