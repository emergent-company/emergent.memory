# 2026-09-09 — Provider setup UX (empty state → add → models)

## Goal

Make the LLM-provider setup flow in the gateway settings clear enough to
complete on first run: surface when no provider is configured, guide the user
to the right place, and make adding the first provider + picking models
coherent. Also resolve gateway/memory contract mismatches surfaced along the
way (empty base URL, connection-test error copy, model pickers on the add
form).

## Outcome

Done. All gateway-only; no emergent.memory change (its behavior was already
correct). Full suite green, browser-verified live against the dev gateway.

## Decisions

- Warning badge lives on sidebar "Project" + settings-rail "Providers" tab when
  the project has zero providers — user confirmed placement; rail badge hidden
  while the Providers page is active (redundant there).
- Provider-config absence flag is computed once per request in the shell and
  cached ~10s per project (`settingsNavCommon` embedded in every settings page
  struct) — cheap enough to badge every page without an extra backend call each
  load.
- Empty providers page hides the Default-models/Rates boxes and shows one hero
  CTA explaining what an LLM provider is — the existing per-box empty states
  read as broken, not actionable.
- All provider-form fields are full width; base-URL field relabeled
  "OpenAI-compatible" (drop LiteLLM) with a neutral placeholder — narrower
  inputs vs full-width selects looked inconsistent.
- Connection testing: never probe with an empty API key (inline + handler tell
  the user first); a 401 shows a friendly line instead of raw upstream JSON.
- Buttons right-aligned in order Cancel → Test connection → Add/Save provider;
  primary label is dynamic ("Add provider" when adding) — primary action last.
- Empty base URL for openai means the official endpoint (`https://api.openai.com/v1`);
  memory already defaults this way (verified in its source), so the gateway
  probe + model seeding just substitute the default instead of rejecting.
- Provider ADD form is credentials-only; model pickers + Test-connection stay on
  EDIT. Memory auto-selects best generative/embedding models on save when none
  are posted — the add-time pickers duplicated project-default setup and were
  empty/confusing on first run.
- Seeding-after-test (fetch `{base_url}/models`, classify, swap the two fallback
  selects) was built for the add form, then retained on the edit form only.
- htmx 4.0.0-beta6 inherits `hx-push-url="true"` from `<main hx-boost>` onto
  every descendant hx action → non-boosted actions pushed POST URLs into
  history. Removed the explicit push-url from main (boosted nav auto-pushes);
  kept `hx-push-url="false"` on the test button as a guard.

## Changes

Gateway only (`gateway/`):
- `project_settings.templ` — settings rail warning badge (`settingsSubNav`
  gains `providersMissing`); zero-provider hero CTA on the Providers page;
  full-width form fields; base-URL label/placeholder; empty-key + 401 advisory
  copy; right-aligned Cancel→Test→Save actions with dynamic label; model-select
  swap container + `providerFallbackModelOptions` fragment; add form =
  credentials-only (pickers/test/hints gated `if p != nil`, openai save-gate
  edit-only, `refresh()` disables Save only when `isEdit`).
- `settings_providers.go` — `settingsNavCommon`/flag plumbing helpers;
  `projectHasNoProviders` (10s per-project cache, invalidated on save/remove);
  test-connection handler returns the model-select fragment on every response,
  seeds it from `{base_url}/models` after a successful probe, classifies via a
  mirrored `nameLooksEmbedding`, maps empty-key/401 to friendly copy,
  `effectiveTestBaseURL` substitutes the openai official endpoint when base URL
  is empty.
- `settings_handlers.go` — embed `settingsNavCommon` in all seven settings page
  structs; populate the flag in all eight handlers.
- `handlers.go` — `Server` cache fields for provider-presence.
- `ui.go` — shell computes `providersMissing` and threads it into `appShell`.
- `ui.templ` — `appShell` signature gains the flag; removed `hx-push-url="true"`
  from `<main id="main-content">` (history-push audit fix).
- `sidebar_user.templ` — sidebar "Project" row warns when providers missing.
- Tests: `settings_providers_test.go`, `project_settings_ui_test.go`,
  `css_consolidation_test.go` — aligned to the new empty state/copy/arity and
  added add-vs-edit form-shape + connection-test seeding/empty-key/401 coverage.

Note: two other sessions worked the same shared tree this day (API-key
PasswordField feature `15ce6e9`/`e9da587` then a revert `b817f2a`; go-daisy
htmx-v4 vendor refresh). The credentials-only add-form templ edits were carried
into HEAD through that shared-worktree churn; the final structure was verified
from HEAD, not from a dedicated commit.

## Verification

- `templ generate && go build ./...` — clean, every round.
- `go test ./... -count=1` — full suite green (both packages) each round.
- `golangci-lint run ./...` — 0 issues; `go vet ./...` — clean.
- Browser (chrome-devtools, dev gateway): zero-provider hero on
  `/settings/providers`, badges on sidebar + rail; uniform 793px field widths;
  empty-key advisory with no probe request; 401 → friendly copy; test-connection
  seeds generative vs embedding selects (validated against a throwaway local
  `/v1/models` endpoint: `gpt-4o`/`deepseek-v4-flash` generative,
  `text-embedding-3-small` embedding-only); URL stays on `/new` after test
  (before the fix it pushed `/settings/providers/test`); boosted rail nav still
  pushes and Back restores; openai + empty base URL probes the official endpoint
  (401-friendly toast with a fake key, no "base_url is required").

## Open questions / follow-ups

- The base-URL inline "Reachable" label is kept for now; user unsure it's the
  best wording.
- openai/deepseek have no embedding API (memory logs it); a project using only
  those needs a separate embedding provider for indexing — gateway gives no
  signal. → task `embedding-provider-guidance`.
- Gateway mirrors memory's embedding/generative classification heuristic
  (`nameLooksEmbedding`) — divergence risk if memory's catalog logic changes.
  → task `provider-model-classification-parity`.
- Per-provider fallback models are auto-selected by memory on save and now only
  fine-tunable on the provider edit form; not settable during add (intended).

## Tasks

- [embedding-provider-guidance](../tasks/embedding-provider-guidance.md) — surface embedding-provider guidance for generative-only providers
- [provider-model-classification-parity](../tasks/provider-model-classification-parity.md) — de-duplicate gateway/memory embedding classification
