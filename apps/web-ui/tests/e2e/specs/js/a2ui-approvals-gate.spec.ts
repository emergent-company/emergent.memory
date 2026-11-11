import { test, expect, type Locator, type Page } from '@playwright/test';
import path from 'node:path';
import { APPROVAL_DECISIONS } from '../../helpers/approval-controls';

// Hermetic DOM-level spec for the A2UI read-only gate (#1892).
//
// `renderA2UISurface` is the second renderer of approval/question controls: the
// public share page reaches it through the `ui` SSE frame. When the bound link
// disables end-user approvals, `share-agent.js` supplies an `approvalsEnabled`
// hook on the render context, and every decision-bearing card must render
// read-only — the same contract `chat-stream.js` applies to the `approval` and
// `question` frames. The gateway's share specs drive the frame path only, so
// this hermetic gate is what pins the A2UI path (`chat-components.js`).
//
// No gateway, no memory API, no Zitadel, no `.env.e2e` — runs in CI.

const CHAT_COMPONENTS_JS = path.resolve(
  __dirname,
  '../../../../gateway/webui/static/js/chat-components.js',
);

const SURFACE_ID = 'js-dom-approvals-surface';

// Every A2UI card is `chat chat-start memory-rise > card > card-body`
// (chat-components.js `a2uiShell`).
const CARD_BODIES = '#chat-messages .chat.chat-start.memory-rise .card-body';

const DISABLED_NOTE = 'Approvals are disabled for this chat.';

interface RenderCtx {
  // When set, the render context carries an `approvalsEnabled` hook returning
  // this value — mirroring `share-agent.js` (disabled) versus the authenticated
  // hosts (hook absent).
  approvalsEnabled?: boolean;
}

function card(page: Page, text: string): Locator {
  return page.locator(CARD_BODIES).filter({ hasText: text });
}

// The gateway passes the A2UI envelope array through verbatim; one
// `updateComponents` message is the shape `renderA2UISurface` consumes.
function uiMessages(components: unknown[]): unknown[] {
  return [{ updateComponents: { surfaceId: SURFACE_ID, components } }];
}

// The three card types that carry decision controls.
const DECISION_COMPONENTS = [
  { id: 'proposal-1', component: 'proposal', kind: 'deploy', summary: 'PROPOSAL_SENTINEL', body: { plan: 'ship it' } },
  { id: 'approval-1', component: 'approval', tool: 'APPROVAL_TOOL_SENTINEL', input: { path: '/tmp/x' } },
  {
    id: 'question-1',
    component: 'question',
    prompt: 'QUESTION_SENTINEL',
    options: [
      { label: 'Yes', value: 'yes' },
      { label: 'No', value: 'no' },
    ],
  },
];

async function bootstrap(page: Page): Promise<string[]> {
  const errors: string[] = [];
  page.on('pageerror', (err) => errors.push(err.message));

  await page.setContent(
    '<!doctype html><html><body><div id="chat-messages"></div></body></html>',
  );
  await page.addScriptTag({ path: CHAT_COMPONENTS_JS });
  return errors;
}

async function render(page: Page, components: unknown[], ctx: RenderCtx): Promise<void> {
  await page.evaluate(
    ({ surfaceId, messages, enabled }) => {
      const container = document.getElementById('chat-messages');
      const api = (
        window as unknown as {
          MemoryChatComponents: {
            renderA2UISurface: (
              surfaceId: string,
              msgs: unknown[],
              ctx: {
                messages: HTMLElement;
                approvalsEnabled?: () => boolean;
              },
            ) => void;
          };
        }
      ).MemoryChatComponents;
      const effective: {
        messages: HTMLElement;
        approvalsEnabled?: () => boolean;
      } = { messages: container as HTMLElement };
      if (enabled !== null) effective.approvalsEnabled = () => enabled;
      api.renderA2UISurface(surfaceId, messages, effective);
    },
    { surfaceId: SURFACE_ID, messages: uiMessages(components), enabled: ctx.approvalsEnabled ?? null },
  );
}

test.describe('A2UI read-only gate (chat-components.js)', () => {
  test('decision controls render read-only when the host disables approvals', async ({ page }) => {
    const errors = await bootstrap(page);
    await render(page, DECISION_COMPONENTS, { approvalsEnabled: false });

    await expect(page.locator(CARD_BODIES)).toHaveCount(3);

    // No decision control survives anywhere in the surface.
    for (const { testid } of APPROVAL_DECISIONS) {
      await expect(page.locator(`[data-testid="${testid}"]`)).toHaveCount(0);
    }
    await expect(page.locator('[data-testid="chat-question-option"]')).toHaveCount(0);
    await expect(page.locator('[data-testid="chat-question-submit"]')).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Accept' })).toHaveCount(0);

    // The bodies stay visible: the tool call, the proposal summary, the prompt.
    await expect(card(page, 'APPROVAL_TOOL_SENTINEL')).toHaveCount(1);
    await expect(card(page, 'PROPOSAL_SENTINEL')).toHaveCount(1);
    await expect(card(page, 'QUESTION_SENTINEL')).toHaveCount(1);
    await expect(page.getByText(DISABLED_NOTE)).toHaveCount(3);

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });

  test('the full controls render when the host does not disable approvals', async ({ page }) => {
    const errors = await bootstrap(page);
    // The authenticated chat host omits the hook entirely; that must keep the
    // canvas interactive (guards against over-gating).
    await render(page, DECISION_COMPONENTS, {});

    await expect(page.locator(CARD_BODIES)).toHaveCount(3);
    // The decision set is asserted from its single owner
    // (helpers/approval-controls.ts): this is the spec that runs the shipped
    // renderer, so a renderer change should break here and nowhere else.
    for (const { testid, label } of APPROVAL_DECISIONS) {
      const control = page.locator(`[data-testid="${testid}"]`);
      await expect(control).toHaveCount(1);
      await expect(control).toHaveText(label);
    }
    await expect(
      card(page, 'APPROVAL_TOOL_SENTINEL').getByRole('button'),
    ).toHaveCount(APPROVAL_DECISIONS.length);
    await expect(page.locator('[data-testid="chat-question-option"]')).toHaveCount(2);
    await expect(page.getByRole('button', { name: 'Accept' })).toHaveCount(1);
    await expect(page.getByText(DISABLED_NOTE)).toHaveCount(0);

    expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([]);
  });
});
