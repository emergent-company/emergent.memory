# 04 — Go Application

The Go app is the **single client-facing origin** and the **operator**. One binary, three
roles (D1, D4). It owns **no durable state** and **no agent logic**.

- **Stack:** Go + go-daisy (templ + echo) for the UI, memory Go SDK for the backend.
- **Ports:** HTTP API + web UI on one port (e.g. `:8095`).

## Roles

### 1. Gateway API (client-facing)

Thin REST surface. It authenticates clients (session cookie by default, or `X-API-Key`)
and proxies/relays to memory. It is the only place the memory `emt_*` token lives
(server-side).

| Route | Action | Backs onto |
|---|---|---|
| `GET/POST /api/agents`, `GET/PUT/DELETE /api/agents/{id}` | agent CRUD | memory agent-definition CRUD |
| `POST /api/agents/{id}/activate` / `/deactivate` | enable/disable | memory `Agent.enabled` |
| `GET /api/orgs`, `GET/POST /api/projects`, `POST /api/projects/{id}/activate` | org/project list, create, switch (session mode) | memory org/project CRUD |
| `POST /api/orgs`, `GET /api/members`, `DELETE /api/members/{userId}`, `GET/POST /api/invites`, `GET /api/invites/pending`, `POST /api/invites/accept`, `POST /api/invites/{id}/decline`, `DELETE /api/invites/{id}`, `GET /api/users/search`, `GET/PUT /api/user/profile` | org/member/invite/profile management (session mode) | memory org/member/invite/profile endpoints |
| `GET /auth/login`, `GET /auth/start`, `GET /auth/callback`, `POST /auth/logout` | Zitadel OIDC sign-in flow + logout | Zitadel token endpoint |
| `GET/POST /api/mcp-servers`, `.../{id}`, `.../{id}/sync`, `.../{id}/inspect`, `.../{id}/tools`, `.../{id}/tools/{toolId}` | MCP server registry CRUD + tool sync/inspect/enable | memory MCP registry |
| `POST /api/chat` (SSE) | text chat with an agent | memory `/api/chat/stream` |
| `GET /api/conversations`, `GET /api/conversations/{id}/messages` | session history | memory chat conversations |
| `POST /api/token` | iOS room-join JWT | LiveKit API (server-side key) |
| `POST /api/setup` | one-time-token → per-device key exchange (no auth) | in-memory setup tokens + memory settings |
| `GET /api/sessions`, `GET /api/session` | iOS session log/records | memory conversations/history |
| `GET /api/memories`, `GET /api/memories/capability` | iOS memory list/capability | memory search/entity-query |

Notes:
- Chat is a **pass-through relay** (or the web UI could call memory directly — but relaying
  through the Go app keeps a single origin + hides the memory token, per D2 "one origin").
- iOS token minting is **moved here** from the retired `agent/admin.py`.

### 2. Supervisor (worker lifecycle)

Reconciles running bridge workers against memory's enabled agents (D2 "child processes").

```
loop (interval, e.g. 2–5s):
  desired = { enabled agents in memory }
  running = { bridge child processes (AGENT_NAME=x) }
  spawn  for name in desired − running
  stop   for name in running − desired
```

- Each worker is a child process: `python -m memory_bridge --agent <name>` (or via
  LiveKit's worker runner), `AGENT_NAME=<name>` env.
- Crash → restart with backoff (e.g. 1s, 2s, 4s, cap 30s).
- Delete/disable → terminate process, reconcile.
- Worker stdout/stderr → supervisor logs (structured, forwarded to the container's log
  driver).

### 3. Web UI (go-daisy)

Main user UI. Pages:

- **Agents** — list, create, edit, delete. The **model** field is a dropdown populated from
  memory's model catalog (`GET /api/v1/models` → `[{provider, modelName, displayName}]`),
  not a free-text input.
- **MCP servers** — management page (`/settings/mcp-servers`, plus `/new` and `/:id/edit`):
  list, register (`stdio`/`sse`/`http` with connection fields), edit, delete, per-server
  sync/inspect, and per-tool enable toggles; builtin servers render read-only. The agent
  **tools picker** groups the agent's tools per registered server (collapsible groups);
  tools no registered server offers render in an "Other" group so they are never silently
  dropped on save. The no-tools empty state links to the management page.
- **Chat** — text chat with a selected agent (streaming). The chat page **is** the sessions
  page: it shows a **session list** (past conversations, resumable) alongside the active
  conversation. The **New chat** button sits at the **top** of the session rail. The session
  rail is **drag-resizable** on desktop (grip on its right edge; width persisted under
  `memory.chat.rail.width.v1`, clamped 200–min(viewport−48, 560)px, default 288px — see
  `chat.js` / `#chat-rail-resize`). The conversation **persists** across navigation — leaving
  the page does not close it; returning resumes it via its `conversationId` (`?c=`).
- **Sessions** — recorded session history (`/sessions`), each with a full timeline and
  run trace (`/sessions/:id`, `/runs/:runId`). Span durations humanize with unit
  escalation (ms → s → m/h/d).
- **Usage** — token/usage stats (`/usage`).
- **Session transcript** — full timeline (user **and** agent messages) via
  `GET /api/conversations/{id}/history`, not just the user messages in `/messages`.
- **Documents** — project document store: list, upload (`POST /documents`), trigger
  extraction, detail view (`/documents/:id`).
- **Skills** — list/create/edit/delete agent skills (`/skills`).
- **Schedules** — list/create/edit/delete schedules + manual run trigger and
  enable/disable toggles (`/schedules`).
- **Organizations** — the account-scoped `/orgs` page lists the user's organizations as an
  access tree (org + role, nested projects + role); each project row is a submit form
  (`POST /projects/activate`) that opens the project (redirects to `/agents`). The org
  landing (`/orgs/:id`) lists the org's projects as a data table: multi-select checkboxes
  with a bulk-delete toolbar (`POST /projects/delete`) and a per-row "3-dot" action menu
  (Open / Delete) rendered as a native popover (D20). Delete opens a **styled confirmation
  dialog** (row + bulk, replacing the native `hx-confirm`) and shows a busy state; deletion
  is **async + soft**: the project stays listed as a pending row with a "Scheduled for
  deletion" badge (purge time, excluded from delete selection) and a **Cancel deletion**
  action (`POST /projects/restore`), and the page auto-refreshes while any project is pending
  (D40). Pending projects come from `GET /api/projects?include_pending=true`. The org
  sidebar is Projects / Members / Settings, where **Settings** (`/orgs/:id/settings`) is a hub
  mirroring the project-settings layout: a sticky side rail (Tools / Danger zone) over a
  two-column grid. Tools lists the org's tool overrides (`POST /orgs/:id/tool-settings/...`
  toggles); Danger zone (`/orgs/:id/settings/danger-zone`) hosts the delete-org form
  (`POST /orgs/:id/delete`). Org admins also invite at the **organization** level from
  the orgs access tree: an "Invite" action on an `org_admin` org row opens
  `/orgs/:id/invite`, whose form carries the org id + the `org_admin` role and **no
  projectId** (the project-scoped invite lives at `/members/new`). The delete form
  **opts out of hx-boost**
  (`hx-boost="false"` + `onsubmit` confirm): deleting the current org changes the
  session context (org/project), which lives in the shell outside `#main-content`,
  so only a true full-load PRG re-renders the topbar. The pre-hub
  `/orgs/:id/tool-settings` page URL 301-redirects to
  the hub.
- **Providers / Models** — configure memory providers (LiteLLM/DeepSeek/Gemini/OpenAI keys)
  + the default generative/embedding model (project model-config). Each provider also carries a
  fallback generative/embedding model, used when the project has no default. Both model
  selectors are credential-prefixed (`provider/model`) and grouped by configured provider;
  per-agent model override lives on the agent form (D16).
- **Settings** — memory connection, LiveKit connection, iOS QR onboarding (renders a
  fresh one-time setup token per page load; scanning it provisions a per-device key),
  a read-only "Voice gateway" section (LiveKit, STT/TTS, turn tuning, exit keywords —
  env-driven, display only), and the registered-device revoke list.
- **API Tokens** — project-scoped token management (`/settings/tokens`): create, revoke,
  regenerate, scope editing.
- **Approvals** — pending-question approvals (`/settings/approvals`): approve, reject,
  cancel.
- **Blueprints & schema migrations** — the Blueprints gallery (`/blueprints`) lists
  installable packs and installed/active versions; per-pack detail pages install, enable,
  and unapply (`/blueprints/:id`). Schema migrations migrate live graph data between
  schema versions: blueprint upgrades **auto-trigger** the migration and auto-uninstall the
  previous version; the manual form is for edge cases (blocked/re-migration). From/To are
  **dropdowns** (populated from install history, grouped by pack, active version marked);
  the gateway **auto-suggests** the from→to sequence (active → latest version per pack).
  **Preview** renders a dry-run plan (type/property renames, removed/added props & types,
  per-type risk) from memory's `migrate/preview` `plan` field; **Execute** applies it,
  with a Force checkbox to bypass the dangerous-migration block. The migrate form
  renders when objects are stale and an upgrade path exists (distinct from/to) **or**
  when staleness is same-version lag: objects' stored `schema_version` lags the installed
  active schema, so the page offers a **from==to resync** against the active schema id
  that re-stamps conforming objects to the installed version with no renames (risky
  non-conforming objects still block without Force). The resync target is **pack-aware**:
  the suggested target is labelled by its pack, and when more than one ACTIVE pack exists
  the user picks which active pack to re-stamp (pack owning the most stale objects is
  suggested). Schema health surfaces memory's per-object drift detail, grouped by owning
  pack (from `/compiled-types` `schemaName`, falling back to bundled packs) then object
  type, showing each object's key and the issue (NULL stamp vs stamp lag vs content
  write). Stale objects with no active schema install show neutral drift copy. An install
  redirects to the migrations page only when it increased the stale-object count (a
  pure-agent install never does); otherwise it returns to `/blueprints?installed=1`.
- **Backups** — list/create/download/delete project backups via Memory's `/api/v1`
  backups domain (org-scoped list/get/download/delete + project-scoped create). The list
  shows status/progress/size/created and links each row to a dedicated details page
  (`/backups/:id`) — async create polled to `ready`/`failed`. The details page shows the
  overview, contents statistics (objects, chunks, chat, files, …), include settings, a
  static archive-format explainer, and the full integrity checksums (checksums live on the
  details page, not the list). Download redirects to Memory's presigned URL; restore shown
  as unavailable (Memory returns 501); a 404 (feature gated off) degrades to an
  "unavailable" state.
- **Objects** — the graph-browser surface: list, create, detail/edit. Property inputs are
  rendered from the compiled schema (D25): `type` drives the default widget (boolean toggle,
  date, number/integer input, array/object chip TagList); `enum` string → native select
  (off-enum stored values preserved); `widget: "input"` → single line; other strings/unknown
  → **auto-growing textarea**. The type-level `ui` block drives icon/color rendering via
  `CompiledType` (`compiledTypeIcon`/`compiledTypeColor`): icons resolve through the
  `supportedTypeIconClasses` catalog, so a bare Lucide name (`FileText`, `GitBranch`) or an
  iconify class (`lucide--file-text`) renders as the matching icon; an unknown name falls back to
  the generic `lucide--box` (never raw text), emoji/glyph values render as a text glyph, and the
  declared color tints the tile/chip inline. **Edit save** is a
  versioned PATCH → PRG redirect to the new version id with `?updated=1` (success toast
  "Object updated."); failures redirect with `?err=<reason>` and the detail page shows the
  real error (older versions and the canonical id resolve to the head version, so stale links
  keep working).

Stat cards use go-daisy's `ui.StatCard` **with icons**; every UI element is a go-daisy
component, not hand-rolled daisyUI markup (exception: row action menus use the native
popover API — see D20).

**Navigation, titles & PRG feedback (D23/D26).** The shell navigates in content via
hx-boost partial swaps of `#main-content`. Two consequences are handled globally in
`page()`/`ui.go`:

- **Toasts on save flows (PRG).** `flashToast` renders an inline script that pushes
  straight into the Alpine `#toast-container` queue when the shell is already loaded
  (boosted swap) and falls back to a `sessionStorage` stash on cold full loads (flushed on
  `DOMContentLoaded`). Handlers signal feedback by redirecting with `?updated=1`
  (success message) or `?err=<reason>` (error); the inline flash is served with the target
  page, so no per-page wiring is needed.
- **Titles stay in sync.** Partials have no `<head>`, so every page partial is prefixed
  with an HTML-escaped `<title>` that htmx extracts, applies to `document.title`, and
  removes before the swap (boosted navigation and history restore).
- **Shell chrome changes reload the shell.** Anything that mutates chrome
  rendered outside `#main-content` must be a true full-load PRG, not a boost swap
  or a no-swap toast, or the chrome goes stale until a manual refresh. This
  covers session-context mutations (org/project switch, org delete —
  `hx-boost="false"` or `render.RedirectAfterMutation`) **and** settings that
  drive shell chrome: adding/removing a project provider (the sidebar "Project"
  warning badge) and setting the assistant agent (the topbar "Assistant" button +
  sidepanel) both answer with `render.RedirectAfterMutation` — `HX-Redirect` for
  HTMX/boosted submits, `303` for plain POSTs. See task
  [boost-context-stale-shell](../tasks/boost-context-stale-shell.md).

Voice is explicitly **not** in the web UI (D5).

## Patterns borrowed from Diane (see 11-reuse-from-diane.md)

- Per-entity API structs with `RegisterRoutes(mux)`.
- Auth middleware: `readOnlyMiddleware` vs `apiKeyAuthMiddleware`.
- Pairing endpoint (`POST /pair` → `{api_key}`, HMAC time-window 6-digit code) — candidate
  replacement for QR-with-shared-key iOS onboarding.
- Go-time JSON convention already matched by the (Diane-derived) Swift models.

## Config / env

| Var | Purpose |
|---|---|
| `MEMORY_URL`, `MEMORY_TOKEN` | memory endpoint + `emt_*` token (server-side secret) |
| `LIVEKIT_URL`, `LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET` | token minting + worker env |
| `LIVEKIT_PUBLIC_URL` | ws url returned to iOS clients |
| `PUBLIC_BASE_URL` | externally-reachable base URL (`scheme://host[:port]`) for client setup/config URLs; also pins the cookie `Secure` flag (`http://` disables it); empty = derive from request Host/scheme. Browser requests on a different `Host` are 302'd to this host (canonical-host redirect) so host-only cookies match the pinned `ZITADEL_REDIRECT_URI`. Session mode requires `https://` outside development |
| `AUTH_MODE` | browser-auth posture: `session` (default) requires Zitadel sign-in; `dev` = explicit unauthenticated local dev (validated + warned at startup) |
| `SESSION_SECRET` | HMAC key for the session cookie (required in session mode) |
| `ZITADEL_ISSUER`, `ZITADEL_CLIENT_ID`, `ZITADEL_REDIRECT_URI` | OIDC issuer + public client for browser sign-in (authorization-code + PKCE; no client secret) |
| `TOKEN_API_KEY` | **optional admin** `X-API-Key`; per-device keys issued via QR setup flow (Project Settings) |
| `MEMORY_PORT` | HTTP port (default `8095`) |
| `DEEPGRAM_API_KEY`, `CARTESIA_API_KEY` | passed to bridge workers (env template) |
| `LLM_BASE_URL`, `LLM_API_KEY` | LiteLLM (used by memory's model config; not directly by Go) |

## Trust / ownership

- The Go app is the **only** memory caller. All memory traffic flows through it (or through
  the bridge for chat streaming — see 05).
- The Go app holds LiveKit server keys for token minting; clients receive only short-lived
  room JWTs.
- No SQLite, no local persistence. Worker PIDs and caches are in-memory only.

## What the Go app must NOT do

- Run any LLM/agent loop.
- Store agent definitions or conversation history locally.
- Implement STT/TTS or audio handling.
- Implement MCP tool execution (memory does it).
