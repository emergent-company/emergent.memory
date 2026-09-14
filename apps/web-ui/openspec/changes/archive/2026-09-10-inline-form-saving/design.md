## Context

The gateway renders forms with `templ` + go-daisy (daisyUI) and already ships HTMX v4 self-hosted (`gateway/ui.templ` loads `webui/static/js/htmx.min.js`) plus an Alpine.js toast queue (`gateway/toast.templ`). Navigation and partial swaps use HTMX; there is precedent for `hx-*` attributes (`spotlight.templ`). The Voice settings panel (`voicePanel` in `gateway/project_settings.templ`) is one `<form method="post" action="/settings/voice">` with a "Save voice" button; `POST /settings/voice` persists the whole panel via `persistVoiceSettings` and redirects to `?updated=1` / `?err=1`, surfacing a flash toast. Each voice field maps to one `(category="voice", key)` row in Memory via `SetProjectSetting`/`DeleteProjectSetting`, and `resolveVoiceSettings` already reads every key individually. See proposal.md for motivation.

Two relevant client mechanisms: the Alpine toast queue is pushed by `window.AlfredApp.toast(kind, message)` (app.js), and `htmx.config.noSwap = [204, 304, "4xx", "5xx"]` suppresses swaps for non-2xx responses — so a rejected save cannot rely on returning 4xx with a body.

## Goals / Non-Goals

**Goals:**

- A reusable inline-save pattern that surfaces save outcome as a toast, usable by any gateway form.
- The Voice settings panel auto-saves each field on change/blur, with no "Save voice" button.
- A documented rule for when a Save button remains necessary, applied consistently.

**Non-Goals:**

- Converting other settings panels (General, Assistant, Overrides, Providers, Remember & Dedup) to inline save in this change — several have atomic/required field groups (e.g. project name + info) that still warrant a button.
- Editing provider secrets inline — secrets stay set/not-set and non-editable.
- Any change to the Memory service itself.

## Decisions

### 1. Per-field save via HTMX on the input, one generalized endpoint

Each editable voice field carries HTMX attributes directly on the input/select/toggle: `hx-post="/settings/voice/:key"`, `hx-trigger` (`change` for toggles/selects, `change delay:400ms` for text inputs to debounce typing), `hx-include="this"` so the input's own `value` is sent, and `hx-swap="none"` (the response has no body; the outcome is a toast, not swapped markup). The outer `<form>` wrapper and "Save voice" button are removed.

A single generalized handler `POST /settings/voice/:key` accepts a form-encoded `value`, validates it per key, persists (empty → `DeleteProjectSetting`, otherwise `SetProjectSetting`), and returns 200 with an `HX-Trigger` header that surfaces a toast. This mirrors how the fields are already modeled (one key per setting) and avoids N bespoke routes.

- Alternative: keep `POST /settings/voice` and add a `?partial=1` mode → rejected, mixes PRG full-page and fragment responses on one route.
- Alternative: `PUT /settings/voice/:key` → rejected for consistency with the existing all-POST write routes (`/settings/project`, `/settings/overrides`, etc.).

### 2. Toast feedback via HX-Trigger, always HTTP 200

Save outcome is surfaced as a toast through the existing Alpine queue: the handler sets an `HX-Trigger: {"alfred-toast": {"kind": "success"|"error", "message": "..."}}` response header, and app.js listens for the `alfred-toast` event on `document.body` and pushes `{kind, message}` into the queue via `AlfredApp.toast`. Success is a generic "Saved" toast; failure carries the validation/write error message. This is more general than per-field inline markup: any HTMX endpoint can emit the same trigger, and there is no per-field feedback region to place or style.

Because `htmx.config.noSwap` includes `4xx`/`5xx`, the handler always returns HTTP 200 and encodes the outcome in the trigger detail, never the status code. The handler never persists a rejected value.

- Alternative: inline per-field feedback (checkmark below the field, error text below the field) → rejected: placement varies by control (input vs. select vs. toggle) and toasts are the more general, consistent solution.
- Alternative: `HX-Trigger` with a simple event name plus a second header for the message → rejected; JSON detail keeps kind + message in one place.
- Alternative: set per-request `noSwap` exceptions or return 422 with `hx-swap` overrides → rejected as fragile and inconsistent with the global HTMX config.

### 3. Save button rule

A dedicated Save button remains only for destructive/irreversible actions. Bound fields that must commit together are handled by the field-group mechanism (Decision 4), not gated behind a button. Voice has no destructive action, so the Voice page drops the button entirely. This rule is expressed in the `inline-form-saving` capability spec and applied to future panels; other settings panels are out of scope here.

### 4. Field groups for coupled fields

Fields that depend on one another are saved as a single atomic unit, not individually. Changing any member triggers a single group save (debounced) carrying all members' values; the group surfaces one toast on success or error. Cross-field constraints (for example, `endpoint_min_delay <= endpoint_max_delay`) are validated client-side before the request is sent (surfacing an error toast and blocking the request), so an invalid combination never reaches the server. The server still re-validates the combined state as a backstop (Decision 5).

In Voice, two groups apply:

- `endpoint_min_delay` + `endpoint_max_delay` → group with a client gate `min <= max`.
- `tts_provider` + `tts_model` + `tts_voice` → group; selecting a provider that does not use a model/voice (`client`/`none`) clears those two and saves them together.

- Alternative: save each member independently and rely on server rejection → rejected, produces the exact "error after editing one field" UX this change removes.
- Alternative: auto-adjust the sibling (cascade) → rejected for Voice as silent mutation; only used where semantics demand it and is always surfaced in feedback.

### 5. Server-side per-key validation

Validation stays server-side in the new handler: the delay/timeout fields must be numeric when non-empty (matching the existing "Handle invalid voice values" requirement), and the grouped endpoint delays are re-checked for `min <= max`. An empty value clears the key (falls back to env default) via `DeleteProjectSetting`; an invalid non-empty value triggers an error toast and persists nothing. Unknown keys trigger an error toast.

### 6. Reuse existing MemoryClient methods

No new `MemoryClient` surface: `SetProjectSetting`/`DeleteProjectSetting`/`GetProjectSetting` (added in `add-project-settings-ui`) already cover per-key writes/reads, and `resolveVoiceSettings` already reads the full voice config.

### 7. Apply the same pattern to the other settings panels

The same inline-save + toast pattern extends to the other independent settings fields, each with a per-field endpoint:

- Assistant (`POST /settings/assistant`): the assistant agent dropdown stores `{"agentId": id}` under category `assistant`/key `agent_id`; empty deletes.
- Remember & Dedup (`POST /settings/remember/:field`): `agent` stores `{"name": agent}` under `remember_config`/`agent_name` (empty deletes); `dedup` validates 0.0–1.0 and stores `{"value": th}` under `entity_create`/`similarity_threshold` (empty leaves it unchanged).
- Project info (`POST /settings/project/:field`): each field PATCHes itself via `UpdateProject` — name is required, budget must be a non-negative number when present, and the toggles are explicit on/off.
- Providers rate override (`POST /settings/providers/:provider/:model`): the input + output prices are a coupled group saved atomically on change (the form serializes both, debounced), persisted via `UpsertProjectPricingOverride`; the remove action (`.../delete`) stays a button but posts inline and surfaces a toast.

Because the value shapes are heterogeneous (`agentId`/`name`/`value`), each field gets a dedicated handler rather than the single generic `:key` handler Voice uses. Panels with atomic multi-field groups (agent overrides) or irreversible actions (device revoke) keep their Save buttons per Decision 3.

## Risks / Trade-offs

- [HTMX `noSwap` swallows error-status swaps] → return 200 for all outcomes and carry success/error in the HX-Trigger detail (Decision 2).
- [Debounce window means a value is not persisted until after the pause] → acceptable: the success toast only appears once the save lands; a `change` trigger on toggles/selects still fires immediately.
- [Per-field/group saves can interleave and race] → each key maps to one setting row; last-write-wins is the existing semantic and matches single-user editing.
- [A group save carries sibling values that are still mid-edit] → group requests include all members' current values; debounce the group trigger and rely on last-write-wins, with the client gate running against the currently-rendered values.
- [Rapid saves can stack toasts] → the queue already stacks with auto-dismiss and hover-pause; the debounced trigger keeps the volume low.
- [A future field that is destructive could be wrongly auto-saved] → the Save-button rule (Decision 3) is a spec-level contract; future fields must be classified before wiring auto-save.

## Migration Plan

- Additive only: new per-field routes (`POST /settings/voice/:key`, `/settings/voice/group/:group`, `/settings/assistant`, `/settings/remember/:field`, `/settings/project/:field`), a `toastTrigger` helper (HX-Trigger header), an app.js `alfred-toast` listener, and rewrites of the Voice, Assistant, Remember & Dedup, and Project info panels. No data migration; the Memory rows are unchanged.
- Deploy via the normal gateway build (`task dev` / `task build`); rollback is a revert of the change.
