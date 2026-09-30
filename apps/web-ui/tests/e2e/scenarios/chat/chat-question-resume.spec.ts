import { test, expect } from '@playwright/test';
import {
  API_KEY,
  ASSISTANT_MD,
  SKIP_NO_KEY,
  cleanup,
  createScratchAgent,
  openChat,
  readConversationHistory,
} from '../../helpers/live-chat';

// ask_user question round-trip: an agent pauses on a pending question in the
// chat dock (#chat-dock [data-testid="dock-question"], chat_dock.templ), the
// operator answers it, and the parked run RESUMES and produces its continuation.
// The resume is not streamed: memory resumes the run in the background and the
// gateway reads the continuation back from history (chat.js answerQuestion →
// refreshActiveTranscript), so the assertion is a rendered continuation in the
// transcript, not a settled send. The dock render itself is asserted by
// chat-run-control.spec.ts; this scenario proves the ANSWER → RESUME round-trip.
//
// Live-LLM and env-gated on E2E_SCENARIO_LLM_API_KEY. Skips when the model never
// asks (no dock card) or when the run errors (provider/environment) — a model
// deviation, not a regression. HARD-FAILS when a card was answered but no
// continuation appears, which is the behavior under test.

const CHOSEN_VALUE = 'grape';
// A distinctive prefix the model only emits AFTER the run resumes, so its
// presence proves the continuation rather than the question card (whose option
// labels include the chosen value).
const MARKER = 'FINALCHOICE';

test.describe('Chat ask_user question resume', () => {
  test('answering a dock ask_user question resumes the run and persists the reply', async ({ page }) => {
    // Provider save + question wait + resumed-continuation wait can each be slow.
    test.setTimeout(600_000);
    test.skip(!API_KEY, SKIP_NO_KEY);

    const name = `E2E QuestionResume ${Date.now()}`;
    let projectId = '';
    let agentId = '';

    try {
      const systemPrompt =
        `Automated ask_user resume test. Call the "ask_user" tool immediately as your first ` +
        `action, with no preamble and no reasoning, exactly once: interaction_type "buttons", ` +
        `question "Which fruit should I pick?", options ` +
        `[{"label":"Pineapple","value":"pineapple"},{"label":"Grape","value":"grape"}]. ` +
        `Never answer in prose before asking, and never call any other tool. After you receive ` +
        `the answer, reply with exactly this single line and nothing else: ` +
        `"${MARKER}:<the value you were given>".`;
      // Policy must be "allow": with "ask" the ask_user TOOL CALL itself is
      // gated behind a tool approval, so the run parks on the approval before the
      // question is ever emitted. This scenario drives the question, not the
      // approval gate (chat-run-control.spec.ts covers the dock card kinds).
      const seeded = await createScratchAgent(page, name, systemPrompt, ['ask_user'], 'allow');
      projectId = seeded.projectId;
      agentId = seeded.agentId;

      await openChat(page, agentId);
      const assistantBefore = await page.locator(ASSISTANT_MD).count();
      await page.locator('#chat-input').fill('Ask me which fruit to pick.');
      await page.locator('#chat-send').click();

      // The pending question renders in the dock. The dock is re-rendered over
      // htmx while the run is parked, so select the option and submit in ONE
      // synchronous in-page poll — a Playwright click would auto-wait on a
      // replaced element until the test times out. The same poll resolves on a
      // transcript error span (env failure) so a 429 skips fast instead of
      // burning the full card timeout. Returning 'submitted' proves the dock
      // rendered the question AND accepted the answer.
      const cardWaitMs = 300_000;
      const outcome = await page
        .waitForFunction(
          (chosenValue: string) => {
            const dock = document.querySelector(
              '#chat-dock [data-testid="dock-question"]',
            ) as HTMLElement | null;
            if (dock) {
              const opt = dock.querySelector(
                `.dock-question-option[data-dock-option-value="${chosenValue}"]`,
              ) as HTMLButtonElement | null;
              const submit = dock.querySelector('.dock-question-submit') as HTMLButtonElement | null;
              if (opt && submit) {
                if (opt.getAttribute('aria-checked') !== 'true') opt.click();
                if (!submit.disabled) {
                  submit.click();
                  return 'submitted';
                }
                return null; // the option click enables submit on the next tick
              }
              return null;
            }
            if (document.querySelector('#chat-messages span.text-error')) return 'error';
            return null;
          },
          CHOSEN_VALUE,
          { timeout: cardWaitMs },
        )
        .then((h) => h.jsonValue() as Promise<'submitted' | 'error'>)
        .catch(() => 'timeout' as const);

      if (outcome !== 'submitted') {
        const errs = await page
          .locator('#chat-messages span.text-error')
          .allTextContents()
          .catch(() => [] as string[]);
        test.info().annotations.push({
          type: 'skipped-step',
          description:
            `no dock question card was answered within ${cardWaitMs / 1000}s (${outcome}) — ` +
            `the turn errored (provider/environment) or the model answered without asking; ` +
            `transcript error: ${errs[0]?.trim() ?? 'none'}`,
        });
        test.skip(true, 'the model did not ask via a pending-work dock question card');
        return;
      }

      // Wait for the resumed continuation's marker. If it never lands, decide
      // fail-vs-skip by whether a NEW assistant bubble appeared at all: no new
      // bubble = the run did not resume (HARD FAIL); a new bubble without the
      // marker = the model ignored the answer format (skip).
      let sawMarker = true;
      try {
        await expect
          .poll(async () => (await page.locator('#chat-messages').innerText()).toUpperCase(), {
            timeout: 150_000,
            intervals: [1000, 1000, 2000, 2000, 5000],
          })
          .toContain(MARKER);
      } catch {
        sawMarker = false;
      }

      const assistantAfter = await page.locator(ASSISTANT_MD).count();
      if (!sawMarker) {
        if (assistantAfter > assistantBefore) {
          test.info().annotations.push({
            type: 'skipped-step',
            description:
              'the run resumed (new assistant bubble) but the continuation did not use the ' +
              `pinned "${MARKER}:<value>" format — model deviation`,
          });
          test.skip(true, 'the model ignored the pinned continuation format after the answer');
          return;
        }
        throw new Error(
          'answering the dock ask_user question did not resume the run: no continuation ' +
            'bubble appeared within 150s',
        );
      }

      // Strong resume signal: the continuation echoes the chosen value.
      const transcript = await page.locator('#chat-messages').innerText();
      expect(
        transcript.toUpperCase(),
        `the resumed continuation must echo the chosen answer (tail: ${JSON.stringify(transcript.slice(-400))})`,
      ).toContain(CHOSEN_VALUE.toUpperCase());

      // The conversation persists the question turn + the resumed continuation.
      const cid = new URL(page.url()).searchParams.get('c') || '';
      expect(cid, 'the conversation must be addressable (?c=<id>)').not.toBe('');
      const items = await readConversationHistory(page, cid);
      const serialized = JSON.stringify(items).toUpperCase();
      expect(serialized, 'history must contain the resumed continuation marker').toContain(MARKER);
      expect(serialized, 'history must contain the chosen answer').toContain(CHOSEN_VALUE.toUpperCase());
    } finally {
      await cleanup(page, { agentIds: [agentId], projectId });
    }
  });
});
