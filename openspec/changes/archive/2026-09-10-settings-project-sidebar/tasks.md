## 1. Shared sub-navigation component

- [x] 1.1 Rename `agentSubNavItem` to `subNavItem` in `gateway/agent.templ` and add a `settingsSubNav(active string)` component rendering General (`/settings`), Voice (`/settings/voice`), Devices (`/settings/devices`), and Approvals (`/settings/approvals`) links. Verify `templ generate` and `go build ./...` succeed and the agent pages still render.
- [x] 1.2 Add a render unit test for `settingsSubNav` asserting all four links and the active highlight (mirror `agent_ui_test.go`). Verify `go test ./...` passes.

## 2. Routes and per-section handlers

- [x] 2.1 Add routes `GET /settings/voice` and `GET /settings/devices` in `gateway/main.go`, mapping to new handlers. Verify `go build ./...` succeeds and routes are registered.
- [x] 2.2 Split `uiProjectSettings` in `gateway/settings_handlers.go` into per-section handlers: General (project info, assistant, overrides, remember/dedup), Voice (voice settings only), Devices (iOS QR + devices only). Reuse `resolveVoiceSettings` and existing fetch helpers; keep save handlers unchanged. Verify `go build ./...` and that each handler loads only its own data.

## 3. Per-section templates

- [x] 3.1 Split `gateway/project_settings.templ` panels into per-section templates (General, Voice, Devices), each rendering `settingsSubNav` + `pageHeader` + its own panels, preserving save forms and `flashToasts`. Verify `templ generate` + `go build ./...` succeed.
- [x] 3.2 Update `gateway/approvals.templ` so the Approvals page renders inside the shared settings sub-nav layout. Verify `templ generate` + `go build ./...` succeed.

## 4. Tests

- [x] 4.1 Update `project_settings_ui_test.go`, `voice_settings_test.go`, and `settings_handlers_test.go` for the new page structure (sub-nav present, panels on correct pages, active highlight). Verify `go test ./...` passes.
- [x] 4.2 Add handler tests asserting `/settings/voice` and `/settings/devices` return 200 and render their sub-nav, and that save handlers still redirect to their section. Verify `go test ./...` passes.

## 5. Verification

- [x] 5.1 Run `templ generate`, `go build ./...`, and `task lint` (PATH="/root/go/bin:$PATH") — all clean. Verify no lint/compile errors.
- [x] 5.2 Start `task dev` and browser-test: visit each Settings section, confirm sub-nav active highlight, save a change on General/Voice/Devices, and confirm success toasts and persistence.
