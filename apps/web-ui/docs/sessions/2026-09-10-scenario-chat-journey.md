# 2026-09-10 — Full-journey e2e scenario + agent chat surfacing fixes

## Goal

Build a separate, more sophisticated Playwright scenario that composes the existing
mutation steps into one full journey: configure an OpenAI-compatible (LiteLLM) provider,
deploy a test blueprint, create an object, create an agent, then start a chat about the
object with the agent. Reuse the existing specs' UI flows as reusable steps, and group
these scenarios separately from the read/mutation suites. Verify it live against the dev
gateway.

## Outcome

**Done** — new `scenarios` test group + `blueprint-object-chat` full-journey spec + shared
UI-step helpers shipped, and the journey **passes live** (setup + journey, 28.4s). Getting
there surfaced a chain of real product bugs across the gateway and the memory backend; all
are fixed, merged, and deployed except one gateway PR still open (`#23`).

Bugs found and fixed while making the journey pass:

- **emergent.memory #399** (merged + deployed) — provider catalog upsert SQL referenced the
  aliased insert target by schema-qualified name → `SQLSTATE 42P01` on every catalog sync →
  `no models in catalog for provider openai` on save.
- **emergent.memory #400** (merged + deployed) — openai-compatible catalog rows persisted
  with an empty `provider`, so provider-filtered reads never found them.
- **gateway** prefixed agent model names — `modelSelect` rendered bare catalog names while
  memory requires `provider/model`; commit `d01edaa` on master.
- **gateway #11** (merged) — conversation-hub background poller ran on `context.Background()`
  (no `X-Project-ID`) → chat history polls 400'd and live refresh never fired.
- **emergent.memory #407** (merged + deployed) — get-or-create conversations had a NULL
  `agent_definition_id`, so `StreamChat` took the legacy direct-LLM path and never invoked
  the agent executor. Now binds the agent on the first turn.
- **emergent.memory #411** (merged + deployed) — `/history` never merged `kb.chat_messages`
  with run items, and reasoning-model final answers streamed as *thinking* only (no text
  delta), so the reply never reached the transcript.
- **gateway #18** (merged) — transcript merge + re-render on stream finish.
- **gateway #23** (**open**) — `hx-boost="true"` on `<main id="main-content">` made htmx
  intercept the `#chat-form` submit and swap `#main-content`, reloading mid-turn and wiping
  the streaming bubble. Fixed with `hx-boost="false"` on `#chat-form`.

## Decisions

- **Separate `scenarios` Playwright project** (`tests/e2e/scenarios/`, top-level) — env-gated
  on a real LLM key, single worker, `setup`-dependent, so the default suite stays green.
- **Fresh scratch project per run** — provider/blueprint/object/agent state is project-scoped,
  the bootstrap project already has `personal-memory` applied, and teardown is one project
  delete (mirrors `agent-model-warning-ui`).
- **Compose from extracted helpers, not duplicated flows** — added
  `helpers/{providers,agents,blueprints,objects,chat}.ts` and refactored
  `provider-config-ui`, `agent-create-ui`, `object-create-ui` to use them (UI-first mutation
  policy preserved).
- **OpenAI-compatible provider via the `openai` slot** with `base_url = http://litellm:4000/v1`
  and prefixed `provider/model` values everywhere the UI submits a model (agent model, project
  default embedding).
- **Give the scenario agent the full tool whitelist (`*`)** — an empty whitelist means "no
  tools" in memory, so a bare chat agent cannot look the object up.
- **Fix product bugs the scenario exposes; do not weaken the assertion** — the scenario is the
  acceptance test for the whole journey.
- **Keep live-run verification env-gated and self-skipping** — a backend rejection skips with
  the backend's copy rather than failing (matches suite precedent).

## Changes

Gateway (`/root/alfred`):
- `tests/e2e/scenarios/blueprint-object-chat.spec.ts` (new) — the full journey.
- `tests/e2e/helpers/{providers,agents,blueprints,objects,chat}.ts` (new) — shared UI-step helpers.
- `tests/e2e/specs/{settings/provider-config-ui,agents/agent-create-ui,objects/object-create-ui}.spec.ts`
  — refactored onto the helpers.
- `tests/e2e/playwright.config.ts` — `scenarios` project + `chromium` exclusion.
- `tests/e2e/.env.e2e.example`, `tests/e2e/README.md` — scenario env vars + coverage.
- `gateway/agent.go`, `gateway/ui.templ`, `gateway/agent_ui_test.go`,
  `gateway/refactor_exact_test.go` — prefixed `provider/model` model select (`d01edaa`).
- `gateway/conversation_events.go` (+ test) — session-scoped hub poller (`c73f8fc`, PR #11).
- `gateway/extras.go`, `gateway/webui/static/js/chat.js` — transcript merge + finish re-render (PR #18).
- `gateway/chat.templ` — `hx-boost="false"` on `#chat-form` (PR #23, open).

Emergent Memory (`/root/emergent.memory`, worktree branches):
- `apps/server/domain/provider/repository.go` — upsert COALESCE via bun alias (`psm`) (#399).
- `apps/server/domain/provider/catalog.go` — stamp `provider` on openai-compatible rows (#400).
- `apps/server/domain/chat/handler.go` — bind agent on first turn; merge stored messages into
  history; text-delta safety net (#407, #411).
- `apps/server/domain/agents/executor.go` — emit final answer as a text delta (#411).

## Verification

- Gateway: `templ generate` + `go build ./...` + `go test ./...` — green; worktree branches
  rebased clean.
- E2E listing: `npx playwright test --list` — 69 tests; scenario under `[scenarios]` only.
- Refactored mutation specs (live): `provider-config-ui` + `agent-create-ui` + `object-create-ui`
  → **5 passed**.
- Full journey (live): `npx playwright test tests/e2e/scenarios/blueprint-object-chat.spec.ts
  --project=scenarios` → **setup + journey passed** (28.4s); assistant reply rendered, session
  in rail.
- Memory: `go build ./...` + `go vet` + `go test` on the touched domains — green per PR.
- Deploy probes: dev memory `/api/health` `v0.75.0` → `v0.76.0` as #407/#411 landed; gateway
  health commit tracks master.

## Open questions / follow-ups

- **Gateway PR #23 (`hx-boost` opt-out) is open** — merge + redeploy the gateway before the
  journey is green against a fresh deploy. The running dev gateway is green only because the
  edit is live in its working tree.
- **Shared checkout is parked on `omos/headroom-stats-fix`** with uncommitted gateway changes
  (`gateway/extras.go`, `gateway/webui/static/js/chat.js`, `gateway/chat.templ`) that duplicate
  merged #18 and open #23. The checkout's commit predates #18, so it needs those working-tree
  edits locally; reconcile against master before assuming the checkout equals master.
- The journey only runs with a real `E2E_SCENARIO_LLM_API_KEY` and a dev memory that can reach
  `http://litellm:4000/v1` and keep the openai catalog synced.
- Other JS-submitted forms inside the boosted `#main-content` were not audited for the same
  htmx-intercept class of bug.

## Tasks

- [hx-boost-js-form-audit](../tasks/hx-boost-js-form-audit.md) — audit JS-handled submits under the boosted `#main-content`.
- [checkout-reconcile-headroom-lane](../tasks/checkout-reconcile-headroom-lane.md) — reconcile the parked checkout + duplicate gateway WIP.
- [e2e-openai-litellm-live-save](../tasks/e2e-openai-litellm-live-save.md) — resolved by #399/#400 (marked done).
