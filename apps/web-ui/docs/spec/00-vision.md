# 00 — Vision & Decisions

## What Memory is

Memory is a **self-hosted, multi-agent platform** for talking to AI
assistants. The owner can **ad-hoc create an agent**, attach **MCP servers** (tools) and a
**prompt**, and immediately **chat with it** — by text in the web UI, or by voice on iOS/Mac.

Everything is configured **server-side through an API**. No per-agent deployment, no
hand-editing of worker config.

## Goals

1. **Ad-hoc agents.** Create/edit/delete an agent via API or web UI; it is chat-capable the
   moment it exists.
2. **MCP-first tooling.** An agent's capabilities come from MCP servers (its own tools), not
   hardcoded Python functions.
3. **One brain.** Text chat and voice chat drive the *same* agent loop — identical behavior,
   shared conversation history.
4. **Thin operators.** The Go app and the Python bridge contain **no agent logic and no
   durable state of their own** — they orchestrate and transport.
5. **Self-hosted.** No SaaS. One server, one memory project per deployment; browser access
   is authenticated by default (D17).

## Non-goals (v1)

- **Skills** (a loadable/searchable snippet library for agents) — deferred.
- **Agent-to-agent (A2A)** orchestration surfaced to users — deferred; it is memory-internal.
- **Voice in the web UI** — deferred until a LiveKit web SDK is easy to embed in Go.
- **Multi-user / multi-tenant** auth — browser auth uses Zitadel sign-in by default, but
  tenancy is a single memory project per deployment (D6); no per-user roles/ACLs yet.
- **Anonymous access** — the app is reachable on a public address but every route requires
  authentication; there is no anonymous entry.
- **Offline / no-cloud operation** — STT/TTS use Deepgram + Cartesia (cloud).

## Principles

1. **Memory owns all durable state.** Agents, conversations, MCP registry, skills, model
   config, session history — all live in Emergent Memory. The Go app and Python bridge
   persist nothing.
2. **One client-facing origin.** Clients talk to the Go app; the Go app talks to memory and
   LiveKit on their behalf. Clients never hold memory or LiveKit secrets.
3. **Chat is the interface.** Human→agent interaction always uses memory's streaming Chat API,
   never the raw A2A/run protocol.
4. **Voice is just another transport.** The voice bridge converts audio ⇄ text around the same
   chat loop; it adds no agent intelligence.
5. **Delete = stop.** Deleting/disabling an agent stops its worker; the supervisor reconciles
   workers to agent state.

## Decision log

| # | Decision | Choice |
|---|---|---|
| D1 | Control-plane language | **Go** — consolidated into the single Go binary |
| D2 | Worker lifecycle mechanism | **Child processes** — supervisor spawns/kills `python` bridge processes |
| D3 | Docker Compose boundary | **Memory app only** — LiveKit, memory, LiteLLM stay external |
| D4 | Go binary layout | **One binary** — gateway API + supervisor + web UI |
| D5 | Web voice | **No voice in web** — manage + text chat only |
| D6 | Tenancy | **None** — single owner, single memory project |
| D7 | Agent brain | **Memory = brain** — memory's chat loop runs the agent for text *and* voice |
| D8 | Voice path | **STT/TTS bridge** — Deepgram STT + Cartesia TTS; Gemini realtime retired |
| D9 | A2A | **Deferred** — memory-internal (`trigger_agent`/`spawn_agents`); enable later = grant tools |
| D10 | Skills | **Deferred** — future snippet library agents can load/search |
| D11 | Clients | Web (main UI), iOS (voice), Mac (wake-word console) |
| D12 | Client→memory interface | **Chat API** (`/api/chat/stream`), not A2A/ACP |
| D13 | Voice language | **Per-agent**, set on the agent definition; STT/TTS follow it |
| D14 | Conversation persistence | **Fresh per session**; long-term memory is per-agent opt-in (memory tools) |
| D15 | Build strategy | **Rip-and-replace** — retire Gemini-realtime now; new architecture is the sole path |
| D16 | Default model + providers | **`deepseek-v4-flash` via LiteLLM** default; model per-agent; one memory project; providers via UI |
| D17 | Browser auth | **Zitadel OIDC sessions, default** — `AUTH_MODE=session` is the default and mints a stateless signed cookie (authorization-code + PKCE); `AUTH_MODE=dev` is an explicit local-dev escape hatch (unauthenticated, validated + warned at startup). Cookie browser lifetime decoupled from the short-lived access token via `SESSION_MAX_AGE` (default 30 days); signed claims still enforce access-token expiry server-side |
| D18 | Tenancy in UI | **Org-grouped project switcher** — memory's org/project tenancy is surfaced through the gateway; org is a grouping header, the project is the active context |
| D19 | Organization as navigation context | **Org = first-class scope** — an organization gets its own sidebar (Projects/Members/Settings) and landing; **Settings** is a hub page with a side rail (Tools / Danger zone, `/orgs/:id/settings[/danger-zone]`) holding the org tool overrides and the delete-org danger zone; the session context is none/org/project (via `activateOrg`/`clearContext`); picker org headers + cogwheel open the org context |
| D20 | Row action menus | **Native popover API** (`popover="auto"` + `popovertarget` + capture-phase `toggle` positioning) — top-layer (never clipped by table overflow), "auto" popovers are mutually exclusive + light-dismiss (one open, Esc/outside close), and synchronous positioning paints once at the final spot (no jump). go-daisy `Popover` was tried and replaced: it positions via `requestAnimationFrame` (visible jump) and has no bottom-left corner placement |
| D21 | Model selector prefixing | **Credential-prefixed (`provider/model`)** — model dropdowns (project default + provider fallback) group by the configured **credential** provider and store `provider/model`. Memory resolves the prefix as the credential provider (model-config) or strips it as routing-only (provider fallback, pending PR #383) |
| D22 | Web CSS delivery | **Consumer-side single-sheet compilation** — the gateway compiles one Tailwind + daisyUI sheet that `@source`s the vendored go-daisy components (utilities/icons on demand), `@import`s go-daisy's co-located `custom.css`, and `exclude`s audited-unused daisyUI modules. go-daisy keeps a slim self-hosted `app.css` for its own gallery. Dropped loading go-daisy's monolithic 473KB `app.css` (two sheets 658KB → one ~283KB raw / 43KB gz) and removed the unlayered `:root` theme-override that fought go-daisy's `nord` default. Gotcha: Tailwind `@source not` globs are global over the final candidate set (order-independent), so an early `@source not ../../vendor/**` silently dropped the vendored go-daisy scan — navbar hamburger icons and the `.modal-toggle` rule vanished. Exclusions are now scoped per top-level vendor dep (see `2026-09-08-godaisy-css-scan` session) |
| D23 | In-content navigation | **hx-boost partial swaps** — in-content links (rows, breadcrumbs, edit) swap `#main-content` instead of full reloads, like the sidebar's `hx-get` nav; htmx v4 boost targeting a non-body element sends `HX-Request-Type: partial`, so the server's partial render path is reused. `onsubmit="return confirm()"` forms were converted to `hx-confirm` (boost ignores inline onsubmit `return false`); native `<dialog>` modals are exempted via `hx-boost="false"` |
| D24 | Account identity display | **Avatar-only topbar; details in the avatar dropdown** — the trigger shows just the avatar; name (line 1) + email (line 2) live in the active-account row of the dropdown. Profile identity card reads display name → email → phone (no repeated full-name line). **Email source: IdP token with Memory-profile fallback** — the gateway maps the profile's `email` (already returned by memory `GET /api/user/profile`) and backfills the account menu + `/profile` when the Zitadel id_token omits the claim (name already had this fallback) |
| D25 | Object property widgets | **Schema-driven inputs, auto-grow default for strings** — the object editor renders each property from its compiled schema: `type` picks the widget (boolean toggle, date, number/integer, array/object TagList); a string with a declared `enum` renders a native select (off-enum stored values kept as an extra option so agent-written data is never lost); an explicit `widget: "input"` forces single-line; any other string/unknown renders an **auto-growing textarea** (the schema cannot structurally distinguish short from long strings, so long text is handled by letting every string field grow). `widget: "textarea"` states that intent explicitly in packs. Type-level `ui` (`{icon, color}`) passes through `CompiledType` for future icon rendering (see [task](../tasks/object-schema-icon-rendering.md)); memory's compiled-types surfaced it in v0.66.0 |
| D26 | PRG feedback + titles under hx-boost | **Immediate-toast flash + partial `<title>`** — hx-boost partial swaps replace `#main-content` without a reload, so the old sessionStorage-only flash (flushed on `DOMContentLoaded`) never fired and `<title>` went stale (partials have no head). `flashToast` now pushes straight into the Alpine `#toast-container` queue when the shell is loaded and falls back to the sessionStorage stash only on cold full loads; every page partial is prefixed with an HTML-escaped `<title>` that htmx extracts and applies before the swap. Versioned object edits redirect to the new version id with `?updated=1` (success toast) or `?err=<reason>` (real error surfaced, no more silent 303). Session-context-changing POSTs (org delete) opt out of boost (`hx-boost="false"`) so the shell re-renders (see [task](../tasks/boost-context-stale-shell.md)) |
| D27 | Account avatar | **Override > social > initials; override in Memory object storage** — the signed-in avatar resolves as a manual upload (`core.user_profiles.avatar_object_key`, served through a gateway proxy `GET /api/user/avatar`) over the Zitadel `picture` claim (read from the OIDC userinfo endpoint, not the id_token) over an initials fallback. The profile page manages the photo through a modal (upload / remove-to-revert). **IdP→Zitadel picture propagation via Zitadel Actions is deferred** (see [task](../tasks/zitadel-social-avatar-sync.md)) |
| D28 | Schema-drift repair semantics | **Staleness = same-version lag, not a pending blueprint** — a stale object's stored `schema_version` lags the ACTIVE compiled schema (stamped only at object write; pack bumps/patches leave old or NULL stamps). Repair is a **from==to resync** migration against the active schema id that re-stamps conforming objects to the installed version; non-conforming objects are drops → risky/dangerous → still block without Force. When no upgrade path exists the migrate form prefills from==to with the most recent active history row; the `/blueprints` "upgrade the owning blueprint" guidance was removed (no such CTA exists in that state). A blueprint install redirects to `/blueprints/migrations` only when it **increased** the stale count — a pure-agent install (no schema) never does. Page states: upgrade / resync / neutral fallback / all-clear. See [2026-09-09-schema-migration-resync](../sessions/2026-09-09-schema-migration-resync.md) |
| D29 | Web htmx baseline | **htmx 4.0.0 final (go-daisy-bundled), v4-native events** — go-daisy completed its partial htmx v2→4 migration (PR #6): bundled core + hx-sse/hx-ws exts at 4.0.0 final, every component/gallery listener on v4 colon event names with `{ctx}` detail, morph on core `innerMorph`, `hx-on::after:request` attrs. alfred vendors go-daisy (pin `bbfb8a3`) and its own listeners are v4-native. Interim: alfred still self-hosts htmx `4.0.0-beta6` + a manual v2-compat config (`implicitInheritance`/`noSwap`/`defaultTimeout`) in the shell; converging onto the vendored copy and dropping the overlay is [task](../tasks/adopt-godaisy-htmx-400.md) |
| D30 | Empty-state / CTA rendering | **Shared go-daisy `ui.EmptyState`** — one component for empty states and call-to-action heroes across the gateway UI (compact in-card + page-level hero scales, tinted tile or muted icon, bordered chrome optional, convenience action + children slot). Replaces per-page bespoke markup and the local `emptyState` wrapper; component owns all spacing (guaranteed description→action gap). Children flow through the templ children context (implicit), never a declared children parameter (see [2026-09-09-emptystate-cta-component](../sessions/2026-09-09-emptystate-cta-component.md)) |
| D31 | Agents list → card grid | **Short chat-first cards, not a table** — the Agents page renders one card per agent (bot icon + truncated name + right-aligned chat icon) in a responsive 1/2/3-col grid. The whole card opens the agent dashboard (`/agents/<id>`); the chat icon is the single exception, a real `<a>` to `/chat?agent=<id>` that the `[data-href]` delegate ignores. Model/tools/flow/visibility columns and the "With tools" stat are gone — cards show only name + icon. Edit/delete moved off the card (settings reachable from the dashboard). See [2026-09-09-agent-cards-ios-pwa](../sessions/2026-09-09-agent-cards-ios-pwa.md). **Cards are mobile-only since D37.** |
| D32 | iOS PWA (Add to Home Screen) | **Standalone via meta tags + `apple-touch-startup-image`, not the manifest** — iOS never reads splash/display/orientation from the web manifest, so launch polish uses `apple-mobile-web-app-capable` + `apple-mobile-web-app-status-bar-style` (black-translucent) + `viewport-fit=cover` with `env(safe-area-inset-*)` padding on the chrome and chat composer. Splash = one `apple-touch-startup-image` link per device geometry class (portrait + landscape, 40 images). Maskable 512 icon is Android-only (iOS uses `apple-touch-icon`). Standalone detection = `navigator.standalone` first, then `(display-mode: standalone|fullscreen)` (iOS reports `fullscreen` — WebKit bug 264218), surfaced as `data-standalone` on `<html>`. See [2026-09-09-agent-cards-ios-pwa](../sessions/2026-09-09-agent-cards-ios-pwa.md) |
| D33 | MCP server management UI | **In-browser registry management** — a standalone Settings-group page (`/settings/mcp-servers`) mirrors the API Tokens page pattern: list/register/edit/delete external servers (`stdio`/`sse`/`http`, headers as key/value rows), per-server sync (`POST /:id/sync`) + inspect (`POST /:id/inspect`), per-tool enable toggle (`PATCH /:id/tools/:toolId`), cached-tool groups, and read-only builtin servers. Gateway ships thin client proxies for the four memory admin routes the page needs. See [2026-09-10-mcp-servers-ui-tool-calls](../sessions/2026-09-10-mcp-servers-ui-tool-calls.md) |
| D34 | Agent tool whitelist ↔ pool key contract | **Whitelist stores bare tool names; memory resolves them to pooled keys** — the agent tools picker persists the bare name memory's tools API returns (e.g. `web_search_exa`), never the namespaced key. Memory builds external pool keys as `<slugified server>_<toolName>` (server names slugified to yield valid LLM function names, ≤64 chars) and resolves bare-name whitelist entries via a bare→prefixed alias; tool calls route back to the raw server by slug-prefix match. Requires memory ≥ the release containing PRs #406/#410; earlier builds silently dropped whitelisted tools (only `set_session_title` reached the model). See [2026-09-10-mcp-servers-ui-tool-calls](../sessions/2026-09-10-mcp-servers-ui-tool-calls.md) |
| D35 | Model-availability warnings | **Warn, never mutate** — an explicit `provider/model` whose provider prefix matches no configured project provider (or a project with no providers at all) is flagged **error** wherever the model is presented: the agent dashboard notice, the agent-settings Model section, blueprint-detail agent rows, and a pre-send banner in the chat workspace. The stored model is always kept — no prompting or dropping at install/apply; bundled blueprints are seeded as global/published records, so a per-project override cannot go through `ApplyBlueprint(ctx, id)`. Availability is a **provider-prefix match** against configured providers (catalog membership not required; a bare model with providers present is satisfied). The legacy "providers exist but no default pinned" state stays **warning** (chats still run). See [2026-09-10-model-availability-warnings](../sessions/2026-09-10-model-availability-warnings.md) |
| D36 | Agent chat turn surfacing | **Agent-bound turns + merged transcript** — get-or-create conversations (object chat, canonical) have no `agent_definition_id`, so the first agent-backed turn now binds it and the agent executor runs (previously the turn fell into the legacy direct-LLM path: no run, no assistant message). `GET /api/conversations/{id}/history` now merges stored `kb.chat_messages` with the run timeline items, and reasoning-model final answers emit a text delta so the reply reaches the transcript. The gateway re-renders the merged transcript when a stream finishes (without wiping an in-flight bubble), and `#chat-form` opts out of `hx-boost` (`hx-boost="false"`) so the JS-handled submit is not intercepted by the shell's `#main-content` swap. Requires memory ≥ the release containing PRs #407/#411. See [2026-09-10-scenario-chat-journey](../sessions/2026-09-10-scenario-chat-journey.md) |
| D37 | Agents list viewport split | **Desktop table, mobile cards** — the Agents page renders the restored data table (Agent / Model / Tools / Flow / Visibility / Updated / edit+delete) at `md` and up (`hidden md:block`), and the D31 chat-first card grid below `md` (`md:hidden`). The Model column reads the list summary's `effectiveModel` (no N+1 `agentDefs` lookup) and drops the old "(default)" suffix — the summary payload carries no explicit-vs-resolved signal. Restores the surface D31 removed for desktop only. See [2026-09-10-agents-responsive-table](../sessions/2026-09-10-agents-responsive-table.md) |
| D38 | go-daisy component boundary | **Backport only generic + absent; adopt existing; keep domain local** — the web UI converges on go-daisy for generic primitives. Released upstream as real minor tags (`v0.11.0` = `layout.Container`/`layout.Rail`/`shared.RelativeTime`; `v0.12.0` = `ui.AvatarFull`) and adopted in the gateway, deleting hand-rolled markup (56 page-container sites, 18 settings-rail shells, 3 avatars, 27 inline buttons, `settingsField`). Duplication that encodes app domain vocabulary stays local (11 status→intent maps, `*CountLabel` → one local `countLabel`, collapsible tool groups, checkbox pickers) rather than pushing it into the shared lib; components whose upstream API cannot preserve current behavior are skipped, not forced (`modal.*`, `layout.Sidebar`, `form.StructuredInput`). See [2026-09-10-godaisy-component-backport](../sessions/2026-09-10-godaisy-component-backport.md) |

| D39 | macOS connector app | **Embedded Go engine + Zitadel-native sign-in, file-backed secrets** — the Mac connector is a SwiftUI app (`Memory`, Dock + menu-bar) that runs the Go connector engine as a direct child process (single stable TCC identity), signs in with Zitadel Authorization Code + PKCE (no client secret), and keeps OIDC sessions/project tokens in **0600 files under `~/.config/memory-connector/accounts/<id>/`** rather than the Keychain (ad-hoc rebuilds reset Keychain access and prompted per item). Multi-account with strict per-account isolation (sessions, profiles, project tokens, connected project); built-in **Prod/Dev** environments. See [2026-09-13-memory-mac-connector](../sessions/2026-09-13-memory-mac-connector.md) |
| D40 | Relay node registry (offline/last-seen) | **Gateway-side registry in project settings KV** — relay sessions are in-memory in Memory, so offline nodes vanished. The gateway keeps a per-project registry (`mcp_relay_nodes`/`registry` setting) reconciled lazily on page load: live nodes upsert version/tool count/`lastSeen` (+ tool snapshot), absent nodes stay as **Disconnected** with `last seen <rel>`, and a confirmed **Remove** (`POST /settings/mcp-nodes/remove`, PRG) deletes stale entries. No backend/connector changes; a TTL/background reconciler is deferred ([task](../tasks/mac-connector-presence-ttl.md)). See [2026-09-13-memory-mac-connector](../sessions/2026-09-13-memory-mac-connector.md) |

## Open decisions

None — all decisions resolved.

## Glossary

| Term | Meaning |
|---|---|
| Memory / Emergent Memory | The backend service (`/root/emergent.memory`, Go, Postgres+pgvector) that owns agents, chat, MCP, skills, A2A, tenancy |
| Go app / `memory` | The single Go binary: gateway API + supervisor + web UI |
| Voice bridge / bridge | The Python LiveKit worker that converts audio ⇄ memory chat |
| Supervisor | The Go component that spawns/stops one bridge worker per agent |
| Agent | A configured assistant (prompt + model + MCP tools) owned by memory |
| MCP server | An external tool provider (stdin/http/sse) registered in memory's MCP registry |
