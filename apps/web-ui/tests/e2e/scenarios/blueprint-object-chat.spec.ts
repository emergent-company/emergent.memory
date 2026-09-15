import { test, expect, type Page } from '@playwright/test';
import { readBootstrap, createProject } from '../helpers/bootstrap';
import { expectAppPage } from '../helpers/page';
import { addProvider, setDefaultEmbeddingModel } from '../helpers/providers';
import { createAgentViaModal } from '../helpers/agents';
import { installBlueprint } from '../helpers/blueprints';
import { createObjectViaForm } from '../helpers/objects';
import { startObjectChat, sendChatMessage } from '../helpers/chat';

// Full-scenario e2e journey: provider → agent → blueprint → object → chat.
//
// One test composes the shared UI-step helpers into a single realistic flow on
// a FRESH scratch project under the bootstrap org:
//   1. SEED (API): create the scratch project (the bootstrap project already
//      has personal-memory applied, so a fresh project is what makes the
//      blueprint + type steps real — all of provider/blueprint/object/agent
//      state is project-scoped);
//   2. PROVIDER (UI): add a live provider in /settings/providers/new. The save
//      is live-validated by the memory backend, so a real key is required;
//      when dev memory rejects it (unsynced catalog or invalid key) the test
//      skips with the backend's copy — it does NOT fail, mirroring the skip
//      precedent in specs/agents/agent-model-warning-ui.spec.ts;
//   3. EMBEDDING MODEL (UI): pin the project's default embedding model on
//      /settings/providers — agent memory-lookup turns embed their query, which
//      needs a project embedding model (the provider save only configures the
//      generative side);
//   4. AGENT (UI): create an agent with an explicit model (prefixed
//      provider/model catalog value, e.g. openai/deepseek-v4-flash — the same
//      string the agent modal submits and the memory backend stores);
//   5. BLUEPRINT (UI): install the bundled `personal-memory` pack;
//   6. VERIFY: the blueprint's compiled types now exist in THIS project
//      (/objects/new offers the Person type);
//   7. OBJECT (UI): create a Person object with a unique key + tag;
//   8. CHAT (UI): start a chat about the object (conversation pre-seeded with
//      the object as context) and send a real prompt — the first user message
//      triggers a live LLM turn through the agent's explicit model and this
//      project's provider;
//   9. PERSISTENCE: the conversation shows up in the session rail.
//
// Why its own project: this scenario hits a live LLM, so it must never run in
// the parallel `chromium` read surface, must not race the serial `mutations`
// group, and mutates a scratch project it owns. It depends on `setup` only and
// env-gates itself: without E2E_SCENARIO_LLM_API_KEY the whole test skips fast,
// so the default full-suite run stays green (no key = no live call).
//
// Env vars (see tests/e2e/.env.e2e.example):
//   E2E_SCENARIO_LLM_PROVIDER   provider slug for the settings save
//                               (default 'openai' — the dev litellm gateway)
//   E2E_SCENARIO_LLM_API_KEY    real provider key; no default — the scenario
//                               skips when unset
//   E2E_SCENARIO_LLM_BASE_URL   OpenAI-compatible base URL for the provider
//                               (default http://litellm:4000/v1, the dev litellm)
//   E2E_SCENARIO_LLM_MODEL      prefixed provider/model catalog name picked in
//                               the agent modal's #agent-model dropdown
//                               (default 'openai/deepseek-v4-flash', served by
//                               the dev litellm)
//   E2E_SCENARIO_LLM_EMBEDDING_MODEL
//                               prefixed provider/model pinned as the project's
//                               default embedding model on /settings/providers
//                               (default 'openai/gemini-embedding-001' — agent
//                               memory-lookup turns embed the query, which needs
//                               a project embedding model)
//
// Cleanup mirrors specs/agents/agent-model-warning-ui.spec.ts: delete the agent
// while the scratch project is still the session's active project, reactivate
// the bootstrap project, then delete the scratch project (POST /projects/delete,
// org-scoped). All steps best-effort so repeated runs — and mid-journey skips —
// stay idempotent.
const PROVIDER = process.env.E2E_SCENARIO_LLM_PROVIDER || 'openai';
const API_KEY = process.env.E2E_SCENARIO_LLM_API_KEY || '';
const BASE_URL = process.env.E2E_SCENARIO_LLM_BASE_URL || 'http://litellm:4000/v1';
const MODEL = process.env.E2E_SCENARIO_LLM_MODEL || 'openai/deepseek-v4-flash';
const EMBEDDING_MODEL = process.env.E2E_SCENARIO_LLM_EMBEDDING_MODEL || 'openai/gemini-embedding-001';

function requireBootstrap() {
  const bootstrap = readBootstrap();
  expect(bootstrap, 'setup project must run first').toBeTruthy();
  return bootstrap!;
}

// All steps best-effort (catches are empty): idempotent across repeated runs,
// skips, and failures anywhere in the journey.
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

test.describe('Blueprint → object → chat scenario', () => {

test('full journey: provider → agent → blueprint → object → chat', async ({ page }) => {
  // The live chat turn alone can take a minute; the whole journey needs headroom.
  test.setTimeout(240_000);

  test.skip(
    !API_KEY,
    'E2E_SCENARIO_LLM_API_KEY is not set — the provider save is live-validated ' +
      'and the chat turn calls a real model. Set it in tests/e2e/.env.e2e to run ' +
      'this scenario (defaults target the dev litellm: openai @ http://litellm:4000/v1).',
  );

  const bootstrap = requireBootstrap();
  // Project and agent share one unique name, so failures are easy to spot in
  // the org's lists even if cleanup is interrupted.
  const name = `E2E Scenario ${Date.now()}`;
  const key = `e2e-scenario-${Date.now()}`;
  const tag = `e2e-scenario-tag-${Date.now()}`;
  let agentId = '';
  let projectId = '';

  try {
    // 1. SEED (API): a fresh project isolates provider/blueprint/object/agent
    // state (all project-scoped) and makes the blueprint step real, since the
    // bootstrap project already has personal-memory applied. createProject also
    // activates it for the session.
    projectId = await createProject(page, bootstrap.orgId, name);

    // 2. PROVIDER (UI): the memory backend live-validates the save (catalog
    // must know the provider; the key must be usable against the base URL). A
    // rejection is an environment problem, not a product regression — skip
    // with the backend's copy so a dev-memory hiccup never reddens the suite.
    const saved = await addProvider(page, PROVIDER, API_KEY, BASE_URL);
    if (saved !== 'saved') {
      // The failure modal carries the backend's reason as its body paragraph.
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

    // 2b. EMBEDDING MODEL (UI): agent memory-lookup turns embed the query, so
    // the project needs a default embedding model. Pin it via the Default
    // models panel on /settings/providers (writes the project model-config the
    // embedding resolver reads first). Only meaningful when the provider save
    // succeeded.
    await setDefaultEmbeddingModel(page, EMBEDDING_MODEL);

    // 3. AGENT (UI): explicit model in the modal's #agent-model dropdown, and
    // the FULL tool whitelist ("*") in #agent-tools. Memory treats an agent
    // with an EMPTY tools list as having no tool access at all (only
    // coordination tools are injected), so without tools the agent could not
    // look the object up — the turn ran but produced no reply. If MODEL isn't
    // in the catalog the helper throws — that is a misconfigured
    // E2E_SCENARIO_LLM_MODEL, so fail loudly rather than skip.
    agentId = await createAgentViaModal(page, name, MODEL, '*');

    // 4. BLUEPRINT (UI): install the bundled personal-memory pack into THIS
    // project (the fresh scratch project has none of its types yet).
    await installBlueprint(page, 'personal-memory');

    // 5. VERIFY: compiled types come from applied blueprints and are
    // project-scoped — the Person type must now be offered in this project's
    // object-creation form. (selectOption asserts the option exists and is
    // enabled; a bare toBeVisible on an <option> never passes because options
    // in a closed select are not "visible" to Playwright.)
    await page.goto('/objects/new');
    await page.locator('select[name="type"]').selectOption('Person');
    await expect(page.locator('#object-key')).toBeVisible();

    // 6. OBJECT (UI): create a Person object and confirm its detail page
    // rendered (the detail page titles itself with the object's key).
    const objectId = await createObjectViaForm(page, 'Person', key, [tag]);
    await page.goto(`/objects/${objectId}`);
    await expectAppPage(page, new RegExp(key));

    // 7. CHAT ABOUT OBJECT (UI): object detail → "Chat about object" → the
    // conversation is pre-seeded with the object as context. The first user
    // message triggers the real LLM turn (the agent has an explicit model and
    // the provider is configured in this project).
    await startObjectChat(page, objectId);
    const reply = await sendChatMessage(
      page,
      'What is this object about? Answer in one sentence.',
    );
    expect(reply.trim().length).toBeGreaterThan(0);

    // 8. Session persisted: the conversation now appears in the session rail.
    // Rail rows are <li> elements carrying data-action="resume-session"
    // (rendered by sessionRailItem in gateway/chat.templ), so assert on the
    // attribute directly.
    await expect(
      page.locator('#chat-rail [data-action="resume-session"]').first(),
    ).toBeVisible();
  } finally {
    await cleanup(page, agentId, projectId);
  }
});

});
