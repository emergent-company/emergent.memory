## Context

The Alfred gateway is a single Go/echo binary that renders its web UI with `templ` + go-daisy (daisyUI) and talks to the Memory service over REST through `MemoryClient` (see `gateway/memory.go`) and, for a few reads, MCP. Navigation is a sidebar driven by `sidebarGroups()` in `gateway/ui.go`; pages follow the `s.page(c, title, component)` pattern where full loads get the shell and HTMX partials get bare content swapped into `#main-content`. The Settings sidebar group currently holds Blueprints and Skills.

Memory stores the project record in `kb.projects` (name, `project_info`, `chat_prompt_template`, extraction toggles, budget) and general per-project settings in `kb.project_settings` (a `(project_id, category, key)` JSONB store). The relevant REST endpoints (no Memory service change required):

- Project record: `GET /api/projects/current` (read, no project scope required) and `PATCH /api/projects/:id` (write, requires `projects:write`).
- Agent definition overrides (category `agent_override`, key = agent name): `GET /api/projects/:projectId/agent-definitions/overrides`, `GET/PUT/DELETE .../overrides/:agentName`.
- Generic settings (categories `remember_config` key `agent_name`, `entity_create` key `similarity_threshold`): `GET/PUT/DELETE /api/projects/:projectId/settings/:category/:key`.

## Goals / Non-Goals

**Goals:**

- One Project Settings page with three panels: Project Info, Agent Overrides, and Remember & Dedup.
- Read/edit flows for each, using the existing PRG + flash-toast UX (`uiAgentUpdate` / `uiSkill` pattern).
- `MemoryClient` methods for every read/write above, tested with `httptest`.

**Non-Goals:**

- No changes to the Memory service itself (separate repo, out of scope).
- No LLM provider configuration management, API-token management, or credential rotation.
- No new persistence in the gateway; the Memory service remains the source of truth.

## Decisions

### 1. Route and navigation

Add `GET /settings` (`uiProjectSettings`) plus `POST /settings/project`, `POST /settings/overrides`, `POST /settings/overrides/:agentName/delete`, and `POST /settings/remember` for the PRG write flows. Add a `{Label: "Project Settings", Href: "/settings", Icon: ...}` item to the Settings group in `sidebarGroups()`. Follows the existing page/route conventions in `main.go` and `ui.go`.

- Alternative: nest under `/agents/...` — rejected, project-level settings are not agent-scoped.

### 2. Project info read/write and scope

Read the project record via the session's active project id (`GET /api/projects/:id`) when a web session is attached, falling back to the token-bound `GET /api/projects/current` when no project id is resolvable (the API-key path). A web session's Zitadel access token is never project-bound, so `/current` returns null for it; the id lookup (via `projectIDFor`) is what actually resolves the project. Write via `PATCH /api/projects/:id` using the resolved project id. The write requires the `projects:write` scope; if the token lacks it, the save is rejected with a clear error surfaced in the flash toast rather than a crash.

- Alternative (rejected): `/current` only — simpler, but wrong for web sessions whose Zitadel token is not project-bound (see the auth-project-frame D3/D6 rationale).

### 3. Enumerating settings without a list endpoint

Memory exposes no "list all project settings" endpoint (only per-`category`/`key` reads and the agent-overrides list). The page therefore reads the known keys explicitly: `remember_config/agent_name` and `entity_create/similarity_threshold`, and lists overrides via the overrides endpoint. Each setting is fetched best-effort; an absent value renders as "not set".

- Alternative: add a list endpoint to the Memory service — rejected as out of scope for this repo.

### 4. One `MemoryClient` surface per setting

Add typed methods mirroring the existing client style: `GetCurrentProject`, `UpdateProject`, `ListAgentOverrides`, `SetAgentOverride`, `DeleteAgentOverride`, `GetProjectSetting`, `SetProjectSetting`, `DeleteProjectSetting`. Each wraps the REST call and a typed struct/JSON envelope; reuse `do`/`doH` rather than raw `http`.

### 5. Testing (TDD)

Unit-test the `MemoryClient` methods with `httptest` (mirroring `memory_test.go`), and the handler's happy/error paths with the `handlers_test.go` pattern. Add a `.templ` render test for the new page (mirroring `agent_ui_test.go`). E2E is out of scope per project convention.

## Risks / Trade-offs

- [New setting category appears later] → The page only renders known categories; a future category needs a new panel. Acceptable: settings categories are stable and explicit today.
- [Token lacks `projects:write`] → Project info edit saves fail with a clear error; read-only views still work.
- [Memory unreachable] → Page renders an error state and does not treat partial data as authoritative (spec requirement).

## Migration Plan

- Additive only: new routes, new client methods, new page, one sidebar item. No data migration.
- Deploy via the normal gateway build (`task dev`/`task build`); rollback is a revert of the change.
