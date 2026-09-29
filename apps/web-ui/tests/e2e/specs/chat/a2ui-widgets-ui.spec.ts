import { test, expect, type Locator, type Page } from '@playwright/test';
import { readBootstrap } from '../../helpers/bootstrap';
import { expectAppPage } from '../../helpers/page';

// A2UI (v0.9.1) structured-UI card renderer, exercised end-to-end in the
// browser.
//
// An agent streams declarative cards to the client as a single `ui` SSE event
// (server `pkg/sse` UIEvent → gateway rewriteChatStream passthrough → chat.js
// renderUI → chat-components.js renderA2UISurface). In production the nine
// catalog cards are emitted by a model, so a real turn cannot reliably exercise
// every card — the same reason agent-proposal-card.spec.ts skips on model
// deviation. This spec instead intercepts POST /api/chat and fulfils it with a
// hand-written SSE stream carrying one `ui` event, then asserts the client
// renders every catalog card, the unknown-component fallback, and the action
// wiring. Deterministic — no LLM, no provider, no env gate.
//
// Not covered here: the gateway's verbatim `ui` passthrough
// (gateway/sse_markdown_test.go) and catalog validation + fenced-block
// extraction (apps/server/pkg/a2ui/a2ui_test.go).

const SURFACE_ID = 'a2ui-e2e-surface';
const CATALOG_ID = 'memory-basic';

// One sentinel per card, unique across the whole surface, so a scoped locator
// can never match a sibling card.
const SENTINEL = {
  proposal: 'A2UI_PROPOSAL_SENTINEL',
  approval: 'A2UI_APPROVAL_SENTINEL',
  question: 'A2UI_QUESTION_SENTINEL',
  code: 'A2UI_CODE_SENTINEL',
  entityType: 'A2UI_ENTITY_TYPE_SENTINEL',
  entityProp: 'A2UI_ENTITY_PROP_SENTINEL',
  entityRel: 'A2UI_ENTITY_REL_SENTINEL',
  form: 'Structured input requested',
  todo: 'A2UI_TODO_SENTINEL',
  todoDone: 'A2UI_TODO_DONE_SENTINEL',
  result: 'A2UI_RESULT_SENTINEL',
  sources: 'A2UI_SOURCES_SENTINEL',
  fallback: 'A2UI_FALLBACK_PROP_SENTINEL',
} as const;

// The full catalog (apps/server/pkg/a2ui BasicCatalog) plus one unknown
// component. Two updateComponents messages prove the renderer accumulates
// components across messages; createSurface/updateDataModel are tolerated
// (ignored) envelopes.
const COMPONENTS_FIRST = [
  { id: 'proposal-1', component: 'proposal', kind: 'deploy', summary: SENTINEL.proposal, body: { plan: 'ship it' } },
  { id: 'approval-1', component: 'approval', tool: SENTINEL.approval, input: { path: '/tmp/x' } },
  {
    id: 'question-1',
    component: 'question',
    prompt: SENTINEL.question,
    options: [
      { label: 'Yes', value: 'yes' },
      { label: 'No', value: 'no' },
    ],
  },
  { id: 'code-1', component: 'code', lang: 'go', code: SENTINEL.code },
];

const COMPONENTS_SECOND = [
  {
    id: 'entity-1',
    component: 'entity',
    type: SENTINEL.entityType,
    properties: { name: SENTINEL.entityProp },
    relationships: [{ relation: 'knows', target: SENTINEL.entityRel }],
  },
  { id: 'object-form-1', component: 'object-form', fields: [{ name: 'title', type: 'string' }] },
  {
    id: 'todo-1',
    component: 'todo',
    items: [
      { label: SENTINEL.todo, done: false },
      { label: SENTINEL.todoDone, done: true },
    ],
  },
  { id: 'result-1', component: 'result', rows: [{ label: 'rows', value: SENTINEL.result }] },
  {
    id: 'sources-1',
    component: 'sources',
    items: [{ id: SENTINEL.sources, type: 'document', label: SENTINEL.sources }],
  },
  { id: 'mystery-1', component: 'future-card', someProp: SENTINEL.fallback },
];

const UI_MESSAGES = [
  { createSurface: { surfaceId: SURFACE_ID, catalogId: CATALOG_ID } },
  { updateDataModel: { surfaceId: SURFACE_ID, path: '/ignored', value: 1 } },
  { updateComponents: { surfaceId: SURFACE_ID, components: COMPONENTS_FIRST } },
  { updateComponents: { surfaceId: SURFACE_ID, components: COMPONENTS_SECOND } },
];

// The post-rewrite shape the browser consumes: one `ui` SSE frame carrying the
// UIEvent (pkg/sse/events.go). No `meta` frame — a `meta` conversationId makes
// finishTurn re-render an (empty, mocked) conversation transcript, which wipes
// the appended cards before the assertions run.
const SSE_BODY = `data: ${JSON.stringify({ type: 'ui', surfaceId: SURFACE_ID, messages: UI_MESSAGES })}\n\n`;

// Every A2UI card is `chat chat-start memory-rise > card > card-body`
// (chat-components.js a2uiShell). The assistant bubble in the same stream uses
// `.chat-bubble`, not `.card-body`, so this stays a2ui-only for this turn.
const CARD_BODIES = '#chat-messages .chat.chat-start.memory-rise .card-body';

function card(page: Page, text: string): Locator {
  return page.locator(CARD_BODIES).filter({ hasText: text });
}

async function createAgent(page: Page, name: string): Promise<string> {
  const bootstrap = readBootstrap();
  expect(bootstrap, 'setup project must run first').toBeTruthy();
  const resp = await page.request.post('/api/agents', {
    data: { name, tools: [], skills: [], config: {} },
  });
  expect(resp.ok(), `create agent failed (HTTP ${resp.status()}): ${await resp.text()}`).toBeTruthy();
  return ((await resp.json()) as { id: string }).id;
}

// renderSurfaces seeds an agent, opens /chat, and answers the next turn with an
// intercepted SSE stream carrying the canned A2UI surface. Returns the agent
// id for cleanup.
async function renderSurfaces(page: Page, agentName: string): Promise<string> {
  const agentId = await createAgent(page, agentName);

  // Capture the action CustomEvent the card buttons dispatch.
  await page.addInitScript(() => {
    const w = window as unknown as { __a2uiActions: unknown[] };
    w.__a2uiActions = [];
    document.addEventListener('a2ui:action', (e) => {
      w.__a2uiActions.push((e as CustomEvent).detail);
    });
  });

  // The agent's reply is the canned `ui` frame; the LLM never runs.
  await page.route(/\/api\/chat$/, (route) =>
    route.fulfill({
      status: 200,
      headers: { 'content-type': 'text/event-stream', 'cache-control': 'no-cache' },
      body: SSE_BODY,
    }),
  );

  await page.goto(`/chat?agent=${agentId}`);
  await expectAppPage(page, /Chat/);
  const agentSelect = page.locator('#chat-agent');
  await expect(agentSelect).toBeVisible();
  await agentSelect.selectOption(agentId);
  await expect(page.locator('#chat-input')).toBeEnabled();

  await page.locator('#chat-input').fill('Render the A2UI surface.');
  await page.locator('#chat-send').click();
  return agentId;
}

test.describe('A2UI surface cards', () => {
  test('renders every catalog card and the unknown-component fallback', async ({ page }) => {
    const agentId = await renderSurfaces(page, `E2E A2UI Catalog ${Date.now()}`);
    try {
      // 9 catalog components + 1 unknown → exactly 10 cards.
      await expect(page.locator(CARD_BODIES)).toHaveCount(10, { timeout: 15_000 });
      // proposal
      const proposal = card(page, SENTINEL.proposal);
      await expect(proposal).toHaveCount(1);
      await expect(proposal.getByText('deploy')).toBeVisible();
      await expect(proposal.getByRole('button', { name: 'Accept' })).toBeVisible();
      await expect(proposal.getByRole('button', { name: 'Reject' })).toBeVisible();

      // approval
      const approval = card(page, SENTINEL.approval);
      await expect(approval).toHaveCount(1);
      await expect(approval.getByRole('button', { name: 'Approve' })).toBeVisible();
      await expect(approval.getByRole('button', { name: 'Deny' })).toBeVisible();

      // question
      const question = card(page, SENTINEL.question);
      await expect(question).toHaveCount(1);
      await expect(question.getByRole('button', { name: 'Yes' })).toBeVisible();
      await expect(question.getByRole('button', { name: 'No' })).toBeVisible();

      // code — lang label + monospaced payload
      const code = card(page, SENTINEL.code);
      await expect(code).toHaveCount(1);
      await expect(code.locator('pre code')).toHaveText(SENTINEL.code);
      await expect(code.getByText('go', { exact: true })).toBeVisible();

      // entity — type badge + properties + relationships
      const entity = card(page, SENTINEL.entityType);
      await expect(entity).toHaveCount(1);
      await expect(entity.getByText('Properties')).toBeVisible();
      await expect(entity.getByText(SENTINEL.entityProp)).toBeVisible();
      await expect(entity.getByText('Relationships')).toBeVisible();
      await expect(entity.getByText(SENTINEL.entityRel)).toBeVisible();

      // object-form — header + not-implemented placeholder (read-only renderer)
      const form = card(page, SENTINEL.form);
      await expect(form).toHaveCount(1);
      await expect(form.getByText(SENTINEL.form)).toBeVisible();

      // todo — two read-only checkboxes reflecting done state
      const todo = card(page, SENTINEL.todo);
      await expect(todo).toHaveCount(1);
      await expect(todo.locator('li')).toHaveCount(2);
      await expect(todo.locator('li').nth(0).locator('input[type=checkbox]')).not.toBeChecked();
      await expect(todo.locator('li').nth(1).locator('input[type=checkbox]')).toBeChecked();
      await expect(todo.locator('input[type=checkbox]').first()).toBeDisabled();

      // result
      await expect(card(page, SENTINEL.result)).toHaveCount(1);

      // sources — header + one rendered source row
      const sources = card(page, SENTINEL.sources);
      await expect(sources).toHaveCount(1);
      await expect(sources.getByText('Sources', { exact: true })).toBeVisible();
      await expect(sources.getByTestId('chat-source-name')).toHaveText(SENTINEL.sources);

      // unknown component → summary fallback (header + JSON), never thrown
      const fallback = card(page, SENTINEL.fallback);
      await expect(fallback).toHaveCount(1);
      await expect(fallback.getByText('future-card')).toBeVisible();

      // No run error surfaced anywhere in the transcript.
      await expect(page.locator('#chat-messages span.text-error')).toHaveCount(0);
    } finally {
      await page.request.delete(`/api/agents/${agentId}`).catch(() => {});
    }
  });

  test('card action buttons emit an a2ui:action event with surfaceId + action', async ({ page }) => {
    const agentId = await renderSurfaces(page, `E2E A2UI Actions ${Date.now()}`);
    try {
      const proposal = card(page, SENTINEL.proposal);
      await expect(proposal).toHaveCount(1, { timeout: 15_000 });
      await proposal.getByRole('button', { name: 'Accept' }).click();

      await expect
        .poll(() =>
          page.evaluate(
            () => (window as unknown as { __a2uiActions: unknown[] }).__a2uiActions.length,
          ),
        )
        .toBe(1);

      const actions = await page.evaluate(
        () => (window as unknown as { __a2uiActions: unknown[] }).__a2uiActions,
      );
      expect(actions[0]).toEqual({
        surfaceId: SURFACE_ID,
        action: { componentId: 'proposal-1', response: 'accept' },
      });

      // Question options use a separate a2uiButton call path (a2uiQuestion) —
      // click one and assert it also carries the surfaceId.
      const question = card(page, SENTINEL.question);
      await expect(question).toHaveCount(1);
      await question.getByRole('button', { name: 'Yes' }).click();

      await expect
        .poll(() =>
          page.evaluate(
            () => (window as unknown as { __a2uiActions: unknown[] }).__a2uiActions.length,
          ),
        )
        .toBe(2);

      const afterQuestion = await page.evaluate(
        () => (window as unknown as { __a2uiActions: unknown[] }).__a2uiActions,
      );
      expect(afterQuestion[1]).toEqual({
        surfaceId: SURFACE_ID,
        action: { componentId: 'question-1', response: 'yes' },
      });
    } finally {
      await page.request.delete(`/api/agents/${agentId}`).catch(() => {});
    }
  });
});
