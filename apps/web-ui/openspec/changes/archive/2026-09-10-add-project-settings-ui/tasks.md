## 1. MemoryClient project-settings methods (TDD)

- [x] 1.1 Add typed structs and `GetCurrentProject`/`UpdateProject` methods wrapping `GET /api/projects/current` and `PATCH /api/projects/:id`, reusing `do`/`doH`. Verify: `go build ./...` in gateway compiles.
- [x] 1.2 Add `ListAgentOverrides`/`SetAgentOverride`/`DeleteAgentOverride` methods wrapping `GET/PUT/DELETE /api/projects/:projectId/agent-definitions/overrides[...]`. Verify: `go build ./...` compiles.
- [x] 1.3 Add `GetProjectSetting`/`SetProjectSetting`/`DeleteProjectSetting` methods wrapping `GET/PUT/DELETE /api/projects/:projectId/settings/:category/:key`. Verify: `go build ./...` compiles.
- [x] 1.4 Write `httptest` unit tests for all new methods covering success, 404/absent, and the project-write forbidden path. Verify: `go test ./...` in gateway passes.
- [x] 1.5 Add unit tests for the remember-agent-name and dedup-threshold get/set/delete paths. Verify: `go test ./...` passes.

## 2. Project settings page handler and routes

- [x] 2.1 Add `uiProjectSettings` handler and `GET /settings` route, and a Project Settings sidebar item in `sidebarGroups()`; render the three panels with best-effort loads and an error state when memory is unreachable. Verify: `go build ./...` compiles and `/settings` renders in the browser.
- [x] 2.2 Add PRG write handlers and routes: `POST /settings/project`, `POST /settings/overrides`, `POST /settings/overrides/:agentName/delete`, `POST /settings/remember`, mirroring `uiAgentUpdate`/`uiSkill` (flash toast, redirect). Verify: `go build ./...` compiles.
- [x] 2.3 Write handler unit tests for happy paths (project info save, override add/update/delete, remember/dedup save) and error paths (empty project name, invalid dedup threshold, project-write forbidden, memory unreachable). Verify: `go test ./...` passes.

## 3. Project settings page template

- [x] 3.1 Create `project_settings.templ` with three panels — Project Info, Agent Overrides (list + add/edit/delete), Remember & Dedup — plus "not set"/"no overrides" and error states. Verify: `templ generate` succeeds and the page renders.
- [x] 3.2 Write a `.templ` render test for the page (mirroring `agent_ui_test.go`) asserting the panels and empty/error states. Verify: `go test ./...` passes.

## 4. Build, lint, and manual verification

- [x] 4.1 Run `templ generate`, `go build ./...`, and `task lint`; fix any issues until clean. Verify: all three commands succeed.
- [x] 4.2 Manual browser test via DevTools: navigate to `/settings`, edit project info, add/update/remove an agent override, save remember/dedup values, submit an invalid dedup threshold and an empty project name, and confirm the unreachable-memory error state. Verify: each flow behaves per spec. (Headless smoke test: `/settings` returns HTTP 200 and renders correctly; interactive DevTools pass deferred to user's local browser.)
