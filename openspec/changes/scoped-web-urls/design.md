## Context

The gateway (`apps/web-ui/gateway`) is a stateless Go/echo/templ app fronting
the Memory REST API. Session auth attaches a per-request `sessionContext`
(`token`, `ProjectID`, `OrgID`) whose `ProjectID`/`OrgID` come from the signed
`memory_session` cookie; `MemoryClient` reads them to send `X-Project-ID` /
`X-Org-ID`.

Because cookies are shared across a browser's tabs, the active project is global
per browser, and no URL carries it. This change makes the path authoritative and
the cookie a fallback for `/` only.

## Goals / Non-Goals

**Goals**
- Org and project context in the URL: `/o/{orgRef}` and `/o/{orgRef}/p/{projectRef}/…`.
- Ref segments accept a slug or a UUID, so slugs can land later with no route change.
- Every tab is independently scoped; every screen is shareable.
- Resolver fails closed against the caller's access tree.
- Existing handlers, `MemoryClient`, and `X-Project-ID` semantics keep working.

**Non-Goals**
- No backend slug/short-id column (follow-up).
- No in-page state in the URL (filters/search/drawers/modals) in this change.
- No legacy URL redirects (hard cut, per decision).
- No change to the Memory API surface.

## Decisions

### D1 — Path shape `/o/{orgRef}/p/{projectRef}/…`

- Org pages: `/o/{orgRef}` (landing), `/o/{orgRef}/settings`, `/…/members`, `/…/invite`.
- Project pages: `/o/{orgRef}/p/{projectRef}/objects`, `/…/agents/:id`, `/…/settings/…`.
- Global (unscoped): `/`, `/orgs` (list/new), `/profile*`, `/auth/*`, `/api/*`,
  `/share/*`, `/static/*`, `/assets/*`, `/webhooks/*`, `/internal/*`.

**Why:** reserved `/o/` + `/p/` prefixes avoid collisions with global routes and
need no blacklist of user-chosen names (the rejected alternative `/{org}/{project}`
does). Both ids stay visible for readability.

### D2 — Refs accept slug or UUID; UUID-only now

`resolveRef` treats a value matching the UUID shape as an id and otherwise as a
slug. Today only ids resolve (slugs do not exist yet). When a backend slug lands,
the same resolver matches it with no URL change.

**Alternative rejected:** short id derived in the gateway from a UUID prefix —
opaque and needs collision handling; the backend slug is the real fix.

### D3 — Pre-routing prefix strip + resolver middleware

`scopeRewrite` is an echo `Pre` middleware (runs before routing): it matches
`/o/{orgRef}[/p/{projectRef}]`, stashes the refs on the request, and rewrites
the path the router sees — org-only scopes map onto the existing `/orgs/:id`
routes, project scopes strip the prefix entirely. The flat route table and all
handlers are therefore reused unchanged.

`applyScope` then runs after `authDispatch`, resolves the refs against the
request-cached `orgAccessTree`, and mirrors the resolved ids into the request's
`sessionContext`, so `sessionHeaders`/`projectIDFor`/`page()` are unchanged.

- Fail closed: unknown ref, wrong org for the project, or not in the caller's
  tree → 404.
- The resolver does **not** write the cookie.

**Alternative rejected:** moving 200+ route registrations into
`e.Group("/o/:orgRef", …)` / `e.Group("/p/:projectRef", …)`. It needs the same
outbound-URL work plus every handler-internal path string, for no extra
correctness; the `Pre` strip is a smaller, contained change. (`Pre` runs before
`Echo.findRouter`, so the rewrite does affect route matching.)

### D4 — Cookie demoted to the `/` default

`ActiveProjectID`/`OrgID` are still written on project activation (recent
lists, device flows) but no longer drive page context. `uiRoot` (`/`) redirects
to the last-used project's scoped URL (`/o/…/p/…/agents`), else `/o/{org}`, else
`/orgs/new`. `resolveDefaultProject` (cookie-based auto-select) is removed —
scope always comes from the path.

### D5 — Scope links and redirects at render time

Rather than edit ~400 link sites (literal `href`/`action`, `templ.URL(...)`, and
concatenated expressions), the gateway rewrites URL-bearing attributes once per
rendered response: `page()` renders to a buffer and `rewriteScopedHTML`
prefixes every `href`/`action`/`hx-*` absolute internal path with the request's
scope base (`/o/{org}[/p/{project}]`), leaving global paths (`/api`, `/auth`,
`/share`, `/profile`, `/orgs`, `/projects`, …) and external/fragment URLs
untouched. Legacy `/orgs/{id}` links map to `/o/{id}`.

Redirects are handled the same way centrally: `scopeLocation` rewrites
`Location`/`HX-Redirect` targets on scoped requests, so the hundreds of
handler-level PRG redirects stay context-free.

`linkURL`/`scopedPathFor` hold the single mapping rule (shared by the rewriter
and unit tests). The picker and post-create/switch redirects use
`projectScopeURL` for a *specific target* project, which the current-page base
cannot express.

**Audit rule:** a scoped page render must contain no unscoped app link; the
rewriter is the enforcement point, and `scope_test.go` pins the mapping.

**Alternative rejected:** per-site `linkURL(ctx, …)` edits — 400 brittle edits
across 18 templates with no central guarantee; the render-time layer is one
tested function.

### D6 — Client-side scope propagation

`fetch`/htmx/EventSource no longer inherit a cookie project, so:

- Shell sets `data-org-id`/`data-project-id` (resolved UUIDs) on `<body>`;
  htmx sends them via a global `hx-headers`, and `fetch` wrappers copy them.
- `EventSource` cannot set headers → the stream URL carries scope (query param)
  or the stream derives it from its entity id; each stream is audited.
- The gateway accepts client `X-Org-ID`/`X-Project-ID` on unscoped `/api/*`
  **only** after validating them against the caller's access tree; device/key
  paths ignore client scope headers.

### D7 — Hard cut in session mode

An unscoped app page GET is rejected (404) in **session mode** — the mode where
the URL is the authority on scope. In dev/API-key mode there is no session and
no URL-scope authority, so unscoped navigation still resolves (the mock e2e
harness relies on it); dev mode cannot express per-tab scope anyway.

Every page and htmx fragment passes through `rewriteScopedHTML`
(`page()`/`renderScoped`, and `renderPartial` for direct `render.RenderPartial`
callers), so internal links/actions are always scoped, including on global
pages (which map legacy `/orgs/{id}` links onto `/o/{id}`).

## Risks

- **Hard cut, no fallback.** A single missed link/action 404s. Mitigation: the
  D5 grep audit plus an e2e route sweep.
- **Sheer churn.** ~200 routes + ~110 link sites + e2e URL fixtures; shared
  files force one writer lane (no parallel lanes on `main.go`/`ui.go`/templ).
- **Streams** are the subtlest seam; audit each SSE endpoint for a scope source.
- **Cross-tenant header injection** — mitigated by access-tree validation in D6.

## Migration

Single PR: `openspec/changes/scoped-web-urls/` + the implementation above.
Archive after merge (`openspec archive scoped-web-urls`).
