# 2026-09-09 — E2E: OpenAI-compatible (LiteLLM) provider scenario + scenarios folder promotion

## Goal

Build a full Playwright scenario that configures an OpenAI-compatible provider
against a LiteLLM proxy from a fresh project via the settings UI, verifying the
API-key show/hide toggle while the key is typed. Then promote the scenario specs
out of `specs/` into a top-level `tests/e2e/scenarios/` folder.

## Outcome

Done. The scenario was added, verified green through every UI assertion, and the
scenario folder was promoted to top level. Its final save step stays env-gated:
dev memory (`api.dev`) cannot sync the openai model catalog to a LiteLLM proxy,
so the live save is rejected there and the test skips with the backend's copy —
by design, matching the `blueprint-object-chat` skip precedent. Commits are on
`origin/master`.

## Decisions

- Env-gate the scenario (`E2E_OPENAI_BASE_URL` default `http://litellm:4000/v1`,
  `E2E_OPENAI_API_KEY` required) and skip on backend save rejection rather than
  fail — the openai save is live-validated (catalog sync + real generate test);
  rejection is an infra problem, not a product regression.
- Test the show/hide toggle against `input[type]` (password ↔ text) + the
  `lucide--eye`/`eye-off` icon swap — stable semantic anchors from the go-daisy
  `PasswordField` markup (`#api_key`, `aria-label="Toggle password visibility"`).
- Model the test on `scenarios/blueprint-object-chat.spec.ts`: fresh scratch
  project under the bootstrap org, best-effort cleanup (provider remove →
  reactivate bootstrap project → delete scratch project).
- Promote scenarios to `tests/e2e/scenarios/` and keep the project `testMatch`
  **unanchored** (`/scenarios\/.*\.spec\.ts/`) — Playwright here matches the
  full file path, so an anchored `^scenarios/` silently collected nothing.
- Use `git mv` for the reorg so a parallel lane's uncommitted WIP on
  `blueprint-object-chat.spec.ts` moved with the rename and stayed unstaged
  (their diff never landed in my commits).

## Changes

- `tests/e2e/scenarios/provider-openai-litellm-config.spec.ts` (new, `ef50e77`)
  — scenario: fresh project → `/settings/providers` hero CTA → `openai`
  provider select reveals the Base URL field → type API key while asserting the
  toggle flips `type` and the eye icon → fill LiteLLM base URL → save
  (`#provider-save-btn`) racing `?updated=1` redirect against the
  "Couldn't save provider" modal → verify provider row (`openai` slug, base URL,
  Edit href). Env vars at top; skips when key unset or save rejected.
- `tests/e2e/playwright.config.ts` (`f94c70b`, `b427342`) — scenarios group
  comment rewritten for the top-level folder; matcher kept unanchored.
- `tests/e2e/scenarios/blueprint-object-chat.spec.ts` (moved, `f94c70b`,
  `b427342`) — relocated from `specs/scenarios/`; helper imports fixed
  `../../helpers` → `../helpers` (depth change). Carried another lane's
  in-progress openai/LiteLLM conversion as unstaged WIP at the new path.
- `tests/e2e/specs/scenarios/provider-openai-litellm-config.spec.ts` → deleted
  by the move.
- `tests/e2e/.env.e2e.example` + `tests/e2e/README.md` — OPENAI scenario env
  vars + coverage paragraph. These were edited concurrently by another lane
  (blueprint defaults → openai); the merged result landed via the concurrent
  `c31690e chore: reconcile concurrent lanes' pending work`.

## Verification

- `npx playwright test --list --project=scenarios` — both scenario specs
  discovered under `[scenarios]` after the move + import fix.
- `npx playwright test scenarios/provider-openai-litellm-config.spec.ts
  --project=scenarios` (×3, full runs incl. setup) — setup login+seed passed
  (~6s); all UI assertions green (hero CTA nav, base-url field reveal, show/hide
  type + icon flips, value preserved); save rejected by dev memory with
  `could not save provider: … generative model test failed: no models in catalog
  for provider openai (sync models before testing)` → test skipped with that
  copy; teardown reuse mode, tenant left in place.
- `tsc` not available (no `typescript` dep) — compile validated via Playwright
  test loading (`--list`).
- `curl {base_url}/v1/models` with the real dev LiteLLM key → HTTP 200 from the
  dev box (key valid), confirming the skip cause is api.dev-side catalog sync,
  not the submitted credentials.

## Open questions / follow-ups

- E2E full success path for an openai (OpenAI-compatible) provider save is
  blocked until dev memory (`api.dev`) can reach a LiteLLM proxy and sync its
  model catalog. Until then the scenario passes/skips on the backend's copy.
  → [e2e-openai-litellm-live-save](../tasks/e2e-openai-litellm-live-save.md).
- Shared-checkout coordination: two lanes drove the same e2e scenario files this
  day (blueprint openai conversion + this scenario). Both landed on master, but
  the shared checkout was left parked on `omos/headroom-stats-fix` with another
  lane's unpushed commits (`c8dcaab` merge, `f8b9db0`, `7c5103b`, `2a06192`).
  Revisit that lane before assuming the local branch == master.

## Tasks

- [e2e-openai-litellm-live-save](../tasks/e2e-openai-litellm-live-save.md) — reach a live OpenAI-compatible provider from dev memory so the e2e openai save completes
