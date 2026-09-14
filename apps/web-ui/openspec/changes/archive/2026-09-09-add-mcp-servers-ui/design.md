## Context

Gateway (Go + templ + go-daisy/daisyUI) already proxies memory's MCP registry over JSON (`/api/mcp-servers` CRUD — `main.go:92-96`, client `extras.go:132-165`, handlers `extras_handlers.go:55-105`, structs `extras.go:107-130`; interface entries `backend.go:42-46`). No page exists; the agent page (`agent.templ:570-587`) shows registered servers read-only and its empty state ("Register an MCP server…") has no link. Missing client/handler/route surface for `sync`, `inspect`, `GET /:id/tools`, `PATCH /:id/tools/:toolId`; memory's admin API (`/root/emergent.memory`, `domain/mcpregistry/routes.go`) already implements all of these. Navigation has two levels: global sidebar groups (`sidebarGroups`, `ui.go:68-106`) and section sub-nav rails (`settingsSubNav`, `project_settings.templ:34-60`).

## Goals / Non-Goals

**Goals:**
- One self-contained project-scoped management surface for MCP servers that follows existing page architecture so it is cheap to build and test.
- Full lifecycle in UI: register (stdio/sse/http), edit, delete, enable/disable, sync tools, inspect, per-tool toggle; plus the agent-page empty-state link.
- Gateway-only change — zero memory backend edits, zero agent/chat behavior change.

**Non-Goals:**
- Official-registry browsing (modelcontextprotocol.io search/install) and per-tool `config`/`configKeys` editing (memory supports them; defer until the management surface proves out).
- Editing servers from the agent page (agents keep a read-only picker + whitelist).
- Voice/iOS/other clients.

## Decisions

**D1 — Page home: standalone Settings-group sibling, not the settings hub rail.** API Tokens is the exact precedent — sidebar "Settings" group entry (`ui.go:97-104`), crumb-framed standalone pages under `/settings/tokens` outside the `settingsSubNav` rail. MCP servers get `{Label:"MCP Servers", Href:"/settings/mcp-servers"}` beside "API Tokens"; breadcrumb "Settings → MCP Servers". *Alternative rejected:* adding a `settingsSubNav` section couples the page to the hub's General/Assistant/… layout and implies per-section settings storage that doesn't apply.
*Alternative rejected:* top-level sidebar group — MCP servers are a settings/registry concern, not a primary surface like Agents/Chat.

**D2 — File trio mirrors api_tokens exactly (DRY by convention):**
- `mcp_servers.go` — page-data structs + PRG handlers + transport-mapping helpers.
- `mcp_servers.templ` — `MCPPage` (list table) + create/edit page (shared transport form) reusing `pageHeader`/`crumbsActive`/`flashToasts`/`pageError` (`ui.templ:481/568/820/179`), go-daisy `table` components (`ui.templ:276ff`), `ui.EmptyState` hero for the empty registry, `modalShell`+`confirmDeleteDialog` (`ui.templ:654/669`), `form.FormControl`/`form.FormActions` (`ui.templ:402/423`).
- `mcp_servers_handlers.go` — thin PRG/JSON wrappers; new client methods live in a new `mcp_servers_client.go` instead of growing `extras.go`, with `MemoryBackend` interface additions in `backend.go:42-46`.

**D3 — Transport-conditional create/edit form.** One shared form page (create = empty, edit = pre-filled), transport chosen by radio; the URL field + headers key/value rows (add/remove row buttons) show for `sse`/`http`; command/args/env rows show for `stdio`. Server-side validation returns inline fieldset errors (no partial save) on: empty name, duplicate name (409 from memory), missing/invalid URL, `http` without headers is fine, blank header names rejected. Copy pattern: muted `text-base-content/45 text-xs` helpers like `schedules.templ:158`.

**D4 — List rows carry tool state + row actions.** Table columns: name, type badge (`ui.Badge` + intent helpers), enabled toggle (`class="toggle"`, cf. `schedules.templ:126`), tool count, actions (Inspect, Sync, Edit, Delete). Detail affordance = expandable `<details>` per row (same interaction as `agentToolServerGroup`, `agent.templ:592-613`) showing cached tools with per-tool `toggle toggle-sm` (`agent.templ:625`) + description. This reuses the tool-picker interaction vocabulary instead of inventing a second one.

**D5 — Sync/Inspect as JSON fetch + re-render (not PRG).** They mutate tool cache and return lists — a POST fetch from the page (go-daisy toast on success/error via existing `flashToasts`/toast helpers, `toast.go`) keeps row state without full-page PRG cycles. Sync/inspect failures return the memory error text; cached tools stay untouched server-side, UI merely reports the error (per spec). Alternatives: PRG form posts per row add noise; full page reload loses expand state.

**D6 — Per-tool toggle = dedicated PATCH route.** Gateway gains `PATCH /mcp-servers/:id/tools/:toolId` proxying memory (`PATCH /api/admin/mcp-servers/:id/tools/:toolId`). Agents already persist their own whitelist on save (`def.Tools = c.Request().Form["tool"]`, `agent.go:237`); disabling a tool project-wide prevents new attach and memory drops it from running toolsets immediately — no agent changes needed.

**D7 — Client additions stay thin proxies.** New methods in `mcp_servers_client.go`: `SyncMCPServer`, `InspectMCPServer`, `ListMCPServerTools`, `SetMCPServerToolEnabled`, each `m.do`/`doH` over the session `X-Project-ID` plumbing (`memory.go:124-146`) — no custom transport logic in the gateway (memory owns stdio/SSE/HTTP client work; see design scope of emergent.memory `mcpregistry/proxy.go`).

**D8 — Agent empty-state link.** `agent.templ:575` description CTA becomes an anchor to `/settings/mcp-servers` (same project context). No behavior change to the whitelist editor.

**D9 — E2E targets the live-dev mutation suite.** New spec `tests/e2e/specs/settings/mcp-servers-create-ui.spec.ts` runs under the `mutations` project (serial, self-cleaning, unique `Date.now()` names, delete in `finally` via `page.request.delete('/api/mcp-servers/<id>')` — pattern: `skill-create-ui.spec.ts`). Because memory (dev) must reach the example server at sync/inspect time, the register/sync flow uses an env-provided URL (`E2E_MCP_EXAMPLE_URL` + optional header key added to `.env.e2e.example`); when unset the spec registers a server but skips sync/inspect assertions or marks them expected-skip — never fails on network state it can't control (pattern: scenario env-gating, `provider-openai-litellm-config.spec.ts`).

## Risks / Trade-offs

- **Secrets in headers** are stored plaintext in memory and now editable in browser UI → keep the API-tokens-style reveal/edit pattern and rely on existing security-spec caveats (docs/spec/09-security.md); do not render secrets as plain text in the list — only in the edit form's fields.
- **stdio registration** spawns processes on the memory host → same trust boundary as today's manual API; mark stdio rows/fields with a warning hint in the UI.
- **Sync/inspect latency** against slow/unreachable servers can hang a row action → run via the existing memory-side timeouts (inspect already 10s); gateway request path just relays, no extra timeout work in v1.
- **Live-dev e2e state leaks** (registrations persist if cleanup fails) → unique name-tagged servers + `finally` delete; if a run dies mid-spec the leftover is identifiable by the `E2E MCP <ts>` name.
- **Page count creep** if a full inspect detail page is added → v1 keeps inspect results inline in the `<details>` row; a dedicated page is an easy follow-up.

## Migration Plan

Gateway-only additive change: new files + new routes + sidebar entry; agent empty-state link update. Rollback = revert commit (no DB/data migration; memory registry untouched). Deploy: standard `task build` + restart; e2e new spec merges into the existing mutations project with no config change.

## Open Questions

- Whether to surface official-registry search/install now (D-non-goal: defer; memory API already exposes `/api/admin/mcp-registry/*` if demand appears).
- Whether per-tool `config`/`configKeys` (memory's tool-config surface) needs UI in v1 — deferred; cached-tool display shows description + toggle only.
