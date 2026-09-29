import { test, expect, type Locator, type Page } from '@playwright/test';
import path from 'node:path';

// Hermetic DOM-level wiring spec for the shipped A2UI card renderer.
//
// It loads `chat-components.js` (the exact file the browser receives) into a
// bare page and drives `renderA2UISurface` + its buttons. This is the automated
// guard for the #1216 regression: `a2uiButton` referenced a `surfaceId`
// identifier that was never a function parameter, so every card-button click
// threw `ReferenceError: surfaceId is not defined` and never dispatched the
// `a2ui:action` event. A syntax check cannot see that (it is a runtime
// ReferenceError), and the Go render/lint tests cannot see it either.
//
// No gateway, no memory API, no Zitadel, no `.env.e2e` — runs in CI.

const CHAT_COMPONENTS_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/chat-components.js',
);

const SURFACE_ID = 'js-dom-surface';

// Every A2UI card is `chat chat-start memory-rise > card > card-body`
// (chat-components.js `a2uiShell`).
const CARD_BODIES = '#chat-messages .chat.chat-start.memory-rise .card-body';

interface DispatchedAction {
  surfaceId: string;
  action: { componentId: string; response: unknown };
}

function card(page: Page, text: string): Locator {
  return page.locator(CARD_BODIES).filter({ hasText: text });
}

// The gateway passes the A2UI envelope array through verbatim; one
// `updateComponents` message is the shape `renderA2UISurface` consumes.
function uiMessages(components: unknown[]): unknown[] {
  return [{ updateComponents: { surfaceId: SURFACE_ID, components } }];
}

// Load the real JS into a bare document and capture `a2ui:action` events plus
// any uncaught page error. Returns the live error array so a test can assert it
// stayed empty (a ReferenceError in a click handler lands here, not in the
// dispatched-event list).
async function bootstrap(page: Page): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));

  await page.setContent(
    '<!doctype html><html><body><div id="chat-messages"></div></body></html>',
  );
  await page.addScriptTag({ path: CHAT_COMPONENTS_JS });

  await page.evaluate(() => {
    const w = window as unknown as { __actions: unknown[] };
    w.__actions = [];
    document.addEventListener('a2ui:action', (e) => {
      w.__actions.push((e as CustomEvent).detail);
    });
  });
  return errors;
}

async function render(page: Page, components: unknown[]): Promise<void> {
  await page.evaluate(
    ({ surfaceId, messages }) => {
      const container = document.getElementById('chat-messages');
      const componentsApi = (
        window as unknown as {
          MemoryChatComponents: {
            renderA2UISurface: (
              surfaceId: string,
              msgs: unknown[],
              ctx: { messages: HTMLElement },
            ) => void;
          };
        }
      ).MemoryChatComponents;
      componentsApi.renderA2UISurface(surfaceId, messages, {
        messages: container as HTMLElement,
      });
    },
    { surfaceId: SURFACE_ID, messages: uiMessages(components) },
  );
}

async function dispatched(page: Page): Promise<DispatchedAction[]> {
  return (await page.evaluate(
    () => (window as unknown as { __actions: unknown[] }).__actions,
  )) as DispatchedAction[];
}

test.describe('A2UI action wiring (chat-components.js)', () => {
  test('card buttons dispatch a2ui:action carrying the surfaceId and action', async ({ page }) => {
    const errors = await bootstrap(page);

    await render(page, [
      {
        id: 'proposal-1',
        component: 'proposal',
        kind: 'deploy',
        summary: 'PROPOSAL_SENTINEL',
        body: { plan: 'ship it' },
      },
      {
        id: 'approval-1',
        component: 'approval',
        tool: 'APPROVAL_TOOL_SENTINEL',
        input: { path: '/tmp/x' },
      },
      {
        id: 'question-1',
        component: 'question',
        prompt: 'QUESTION_SENTINEL',
        options: [
          { label: 'Yes', value: 'yes' },
          { label: 'No', value: 'no' },
        ],
      },
    ]);

    await expect(page.locator(CARD_BODIES)).toHaveCount(3);

    // proposal action row → a2uiActions → a2uiButton
    const proposal = card(page, 'PROPOSAL_SENTINEL');
    await expect(proposal).toHaveCount(1);
    await proposal.getByRole('button', { name: 'Accept' }).click();
    await expect
      .poll(async () => (await dispatched(page)).length)
      .toBe(1);
    expect((await dispatched(page))[0]).toEqual({
      surfaceId: SURFACE_ID,
      action: { componentId: 'proposal-1', response: 'accept' },
    });

    // approval action row → a2uiActions → a2uiButton
    const approval = card(page, 'APPROVAL_TOOL_SENTINEL');
    await expect(approval).toHaveCount(1);
    await approval.getByRole('button', { name: 'Approve' }).click();
    await expect
      .poll(async () => (await dispatched(page)).length)
      .toBe(2);
    expect((await dispatched(page))[1]).toEqual({
      surfaceId: SURFACE_ID,
      action: { componentId: 'approval-1', response: 'approve' },
    });

    // question options use a separate a2uiButton call path (a2uiQuestion) —
    // click one and assert it also carries the surfaceId.
    const question = card(page, 'QUESTION_SENTINEL');
    await expect(question).toHaveCount(1);
    await question.getByRole('button', { name: 'Yes' }).click();
    await expect
      .poll(async () => (await dispatched(page)).length)
      .toBe(3);
    expect((await dispatched(page))[2]).toEqual({
      surfaceId: SURFACE_ID,
      action: { componentId: 'question-1', response: 'yes' },
    });

    // A ReferenceError inside a click handler never reaches the dispatch
    // assertions above silently — it surfaces here as a page error.
    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('an unknown component degrades to a summary card without throwing', async ({ page }) => {
    const errors = await bootstrap(page);

    await render(page, [
      { id: 'mystery-1', component: 'future-card', someProp: 'FALLBACK_SENTINEL' },
    ]);

    const fallback = card(page, 'FALLBACK_SENTINEL');
    await expect(fallback).toHaveCount(1);
    await expect(fallback.getByText('future-card')).toBeVisible();

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });
});
