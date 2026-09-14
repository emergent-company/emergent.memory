## Why

Settings pages in the gateway each wrap their fields in a whole form with an explicit "Save" button and a PRG round-trip. Most settings are independent single fields that can be persisted the moment they change, so the button is pure friction. Inline auto-save with toast feedback (success/error) makes settings feel immediate and removes a step the user shouldn't have to think about.

## What Changes

- Add a reusable inline-save pattern for the gateway's settings fields: a field persists itself on change (debounced for text inputs) and surfaces save outcome as a toast, removing the need for a Save button where fields are independent.
- Apply the pattern to the Voice settings panel (master toggle, TTS/STT provider/model/voice/language, LiveKit endpoints, turn tuning), removing the "Save voice" button.
- Apply the pattern to the Assistant panel (assistant agent dropdown), removing "Save assistant".
- Apply the pattern to the Remember & Dedup panel (remember agent dropdown, dedup threshold), removing "Save remember & dedup".
- Apply the pattern to the Project info panel (name, project info, chat prompt template, auto-extract/auto-merge toggles, budget), removing "Save project".
- Apply the pattern to the Providers panel's per-model rate override (input + output price saved as a coupled group), removing the "Override"/"Update rate" button and laying the model name and price inputs out in one row; the remove action stays a button.
- Define the rule for when a dedicated Save button is still required (destructive actions and atomic multi-field groups such as agent overrides) and keep the button in those cases.

## Capabilities

### New Capabilities

- `inline-form-saving`: the reusable pattern for auto-saving form fields with toast success/error feedback, including the rule for when a dedicated Save button is still necessary.

### Modified Capabilities

- `project-voice-settings`: the Voice settings save interaction changes from an explicit "save" step to inline auto-save with toast feedback.

## Impact

- **gateway/** (Go + templ): `toastTrigger` helper (HX-Trigger header), an app.js `alfred-toast` listener, and per-field inline-save handlers + template rewrites for the Voice, Assistant, Remember & Dedup, and Project info panels.
- **Settings REST surface**: per-field write routes `POST /settings/voice/:key`, `/settings/voice/group/:group`, `/settings/assistant`, `/settings/remember/:field`, and `/settings/project/:field`, replacing the whole-form PRG flows.
- **Memory service REST API** (read/write dependency, no service change): per-key `SetProjectSetting`/`DeleteProjectSetting` and `UpdateProject` (PATCH) are already in use.
- **Tests**: gateway unit tests for the per-field handlers and `.templ` render tests asserting no Save button + inline-save wiring, following existing `settings_handlers_test.go` / `project_settings_ui_test.go` patterns.
