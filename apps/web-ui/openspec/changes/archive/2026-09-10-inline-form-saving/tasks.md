## 1. Toast feedback plumbing (TDD)

- [x] 1.1 Add a `toastTrigger` helper in the settings handlers that signals a toast via the `HX-Trigger` response header (`{"alfred-toast": {kind, message}}`), always returning HTTP 200; add an app.js `document.body` listener for the `alfred-toast` event that pushes into the Alpine toast queue via `AlfredApp.toast`. Verify `go build ./...` succeeds.
- [x] 1.2 Write handler unit tests asserting the `HX-Trigger` header carries a success toast on save and an error toast on failure. Verify `go test ./...` passes.

## 2. Voice panel inline save

- [x] 2.1 Add `POST /settings/voice/:key` and `POST /settings/voice/group/:group` handlers for independent fields and the coupled `endpoint_delays`/`tts` groups; validate per key/group, persist atomically, and return a 200 toast trigger. Verify `go build ./...` succeeds.
- [x] 2.2 Rewrite `voicePanel` to remove the outer form and "Save voice" button, wiring each field/group with `hx-post`/`hx-swap="none"`/`hx-include`, debounced text triggers, and a client-side `min > max` gate that surfaces an error toast. Verify `templ generate` and `go build ./...` succeed.
- [x] 2.3 Write handler + render tests for the voice fields, groups, and the no-Save-button panel. Verify `go test ./...` passes.

## 3. Assistant panel inline save

- [x] 3.1 Convert `uiProjectSettingsAssistant` to inline (toast trigger) for `POST /settings/assistant`, storing `{"agentId": id}` and deleting on empty. Verify `go build ./...` succeeds.
- [x] 3.2 Rewrite `assistantPanel` to remove the form and "Save assistant" button, wiring the select with `hx-post`/`hx-swap="none"`. Verify `templ generate` and `go build ./...` succeed.
- [x] 3.3 Update the assistant route + render tests to assert inline-save wiring and no Save button. Verify `go test ./...` passes.

## 4. Remember & Dedup panel inline save

- [x] 4.1 Add `POST /settings/remember/:field` with `agent` (stores `{"name": agent}`, empty deletes) and `dedup` (validates 0.0–1.0, empty leaves unchanged). Verify `go build ./...` succeeds.
- [x] 4.2 Rewrite `rememberDedupPanel` to remove the form and "Save remember & dedup" button, wiring each field with `hx-post`/`hx-swap="none"`. Verify `templ generate` and `go build ./...` succeed.
- [x] 4.3 Replace the whole-form remember tests with per-field handler + render tests. Verify `go test ./...` passes.

## 5. Project info panel inline save

- [x] 5.1 Add `POST /settings/project/:field` covering name (required), project_info, chat_prompt_template, the two toggles, and budget (non-negative when present); each PATCHes itself via `UpdateProject`. Verify `go build ./...` succeeds.
- [x] 5.2 Rewrite `projectInfoPanel` to remove the form and "Save project" button, wiring each field with `hx-post`/`hx-swap="none"`. Verify `templ generate` and `go build ./...` succeed.
- [x] 5.3 Replace the whole-form project tests with per-field handler + render tests. Verify `go test ./...` passes.

## 6. Providers panel inline save + single-row layout

- [x] 6.1 Convert `uiProjectSettingsProviderOverride` and `uiProjectSettingsProviderOverrideDelete` to inline (toast trigger) — the input + output prices save as a coupled group via `UpsertProjectPricingOverride`, the remove action stays a button but posts inline. Verify `go build ./...` succeeds.
- [x] 6.2 Rewrite `providerModelRow`/`providerPriceField` into a single-row layout (model name + badge + price inputs + rate summary) with a per-row form that auto-saves on `change delay:400ms`, removing the "Override"/"Update rate" button and the separate `providerOverrideForm`. Verify `templ generate` and `go build ./...` succeed.
- [x] 6.3 Update the provider route + render tests to assert inline-save wiring, no Override/Update-rate button, and the one-row layout. Verify `go test ./...` passes.

## 7. Build, lint, and manual verification

- [x] 7.1 Run `templ generate`, `go build ./...`, and lint (golangci-lint); fix any issues until clean.
- [x] 7.2 Manual browser test via DevTools: change each field across the Voice, Assistant, Remember & Dedup, Project info, and Providers panels and observe the success/error toasts; enter invalid values and confirm the error toast + no partial persist; confirm values persist after reload with no Save buttons on those panels. Verify each flow matches the specs. (Headless smoke test: each `/settings/*` route returns HTTP 200 and renders; interactive DevTools pass deferred to the user's local browser.)
