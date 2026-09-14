## Why

The Project Settings page (`/settings`) is one long scroll with seven stacked panels (project info, assistant agent, agent overrides, remember & dedup, iOS setup, devices, voice). It is hard to scan and navigate. The agent details pages already solve this with a vertical sub-nav rail (Dashboard / Settings / Sandbox / Sessions); the Settings area has no such structure.

## What Changes

- Introduce a vertical sub-navigation rail in the Settings area, reusing the same visual pattern as the agent details sub-nav (sticky left rail on desktop, horizontal scroll on small screens).
- Split the single Project Settings page into focused sub-pages, each with its own URL, so panels are grouped by concern:
  - **General** (`/settings`) — Project info, Assistant agent, Agent overrides, Remember & dedup.
  - **Voice** (`/settings/voice`) — Voice settings.
  - **Devices** (`/settings/devices`) — iOS setup + Devices.
  - **Approvals** (`/settings/approvals`) — existing tool-approval audit trail, now reached from the same sub-nav.
- Each sub-page keeps its existing save flows (PRG + flash toasts) and per-section error handling unchanged; only the layout and URLs move.
- Sub-nav highlights the active section and preserves breadcrumbs consistent with the agent sub-nav pattern.

## Capabilities

### New Capabilities

- `settings-navigation`: A vertical sub-menu that organizes the Settings area into General, Voice, Devices, and Approvals sections, each scoped to project settings.

### Modified Capabilities

- `project-voice-settings`: Voice configuration moves from an inline section on the Project Settings page to its own `/settings/voice` sub-page reachable from the settings sub-nav.

## Impact

- `gateway/project_settings.templ` — split panels across sub-pages; add a settings sub-nav rail.
- `gateway/main.go` — add routes `/settings/voice` and `/settings/devices`; keep existing `/settings` (General) and `/settings/approvals`.
- `gateway/settings_handlers.go` — split `uiProjectSettings` data loading / rendering across the new sub-pages; existing save handlers unchanged.
- `gateway/approvals.templ` / `gateway/approvals_handlers.go` — render within the new settings sub-nav layout.
- `gateway/ui.go` — Settings sidebar group still links to `/settings`; no top-level sidebar change required.
- Tests: `gateway/project_settings_ui_test.go`, `gateway/settings_handlers_test.go`, `gateway/voice_settings_test.go` — update for the new page structure and sub-nav rendering.
