import { test, expect } from '@playwright/test';
import { sendChatMessage } from '../../helpers/chat';
import {
  API_KEY,
  SKIP_NO_KEY,
  cleanup,
  createScratchAgent,
  openChat,
  readConversationHistory,
} from '../../helpers/live-chat';

// Multi-turn context scenario: a fresh scratch project carries a fact planted in
// turn 1 into turn 2 of the SAME conversation. This is the multi-round contract
// the other chat specs do not assert — they exercise navigation, run control and
// single forced tool calls, but never that the prior turn's history reaches the
// model on a later turn.
//
// Live-LLM and env-gated on E2E_SCENARIO_LLM_API_KEY (the same key the rest of
// the scenarios suite uses), so the default full run skips fast. The recall
// assertion HARD-FAILS: if turn 2 cannot see turn 1's history, that is the
// regression under test (a model flake on a temperature-0 recall of a token it
// was just told is not plausible enough to justify a skip).

const CODEWORD = 'BANANA42';

test.describe('Chat multi-turn context', () => {
  test('carries an earlier turn into a later turn of the same conversation', async ({ page }) => {
    test.setTimeout(300_000);
    test.skip(!API_KEY, SKIP_NO_KEY);

    const name = `E2E MultiTurn ${Date.now()}`;
    let projectId = '';
    let agentId = '';

    try {
      const systemPrompt =
        `You are an automated test assistant. Answer in one short sentence and never ` +
        `call tools. Always follow the user's requested output format exactly.`;
      const seeded = await createScratchAgent(page, name, systemPrompt, [], 'allow');
      projectId = seeded.projectId;
      agentId = seeded.agentId;

      await openChat(page, agentId);

      // Turn 1: plant a fact. Its own reply is irrelevant; the follow-up is the
      // assertion.
      const first = await sendChatMessage(
        page,
        `Remember this codeword exactly as written: ${CODEWORD}. Reply with just OK.`,
      );
      expect(first.trim().length, 'turn 1 must produce a non-empty reply').toBeGreaterThan(0);

      // The first message enters a conversation (URL ?c=<id>) we can read back.
      const cid = new URL(page.url()).searchParams.get('c') || '';
      expect(cid, 'the first turn must enter a conversation (?c=<id>)').not.toBe('');

      // Turn 2: ask for the fact. A correct answer proves turn 1's history
      // reached the model on a later turn of the same conversation.
      const second = await sendChatMessage(
        page,
        'What was the codeword I asked you to remember? Reply with only the codeword.',
      );
      expect(
        second.toUpperCase(),
        `turn 2 must recall the codeword planted in turn 1 (got: ${JSON.stringify(second)})`,
      ).toContain(CODEWORD);

      // Both turns are persisted in the conversation history.
      const items = await readConversationHistory(page, cid);
      expect(items.length, 'the conversation history must carry the turns').toBeGreaterThanOrEqual(1);
      expect(
        JSON.stringify(items),
        'the conversation history must contain the planted codeword',
      ).toContain(CODEWORD);
    } finally {
      await cleanup(page, { agentIds: [agentId], projectId });
    }
  });
});
