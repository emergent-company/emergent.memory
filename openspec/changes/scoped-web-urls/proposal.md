## Why

The web console is tethered to a single active project per browser: the active
org/project live only in the signed `memory_session` cookie (`ActiveProjectID`,
`OrgID`), which every tab shares. Two tabs cannot show two projects, and no
project-scoped page carries its org/project in the URL, so most screens are not
shareable — a link to "this object in this project" is impossible to express.

Putting scope in the URL fixes both at the root: each tab's URL is its context,
and any view is linkable.

## What Changes

- **Scope in the URL.** Every organization page is served under `/o/{orgRef}`
  and every project page under `/o/{orgRef}/p/{projectRef}`. The signed session
  cookie is demoted to a `/` landing default; it is no longer the source of a
  page's org/project context.
- **Refs are opaque.** `{orgRef}`/`{projectRef}` are UUIDs today and slugs later;
  the resolver accepts either, so adding slugs is not a second URL migration.
- **A scope resolver** validates the refs against the caller's org→project
  access tree (fail closed, 404 on unknown/cross-org) and threads the resolved
  ids through the existing request `sessionContext`, so `X-Project-ID` /
  `X-Org-ID` and every existing handler keep working unchanged.
- **Every internal app link and form action** is rewritten to the scoped base;
  the sidebar, picker, and shell thread the base path.
- **Client-side scope propagation** for AJAX/SSE (`fetch`, htmx, EventSource)
  that no longer inherits a cookie project: htmx sends `X-Org-ID`/`X-Project-ID`
  from shell-rendered body attributes (validated against the access tree);
  streams carry scope in their URL or derive it from the entity.
- **BREAKING (URLs):** existing cookie-scoped URLs (`/objects`, `/agents/:id`,
  `/orgs/:id`, …) are removed, not redirected. Access to a project now always
  requires the scoped path.

Out of scope (follow-up changes): backend-generated slugs/short ids; pushing
in-page state (filters, search, drawers, modals) into the URL.

## Capabilities

### New Capabilities
- `web-routing`: the URL contract for org/project context — scoped paths, the
  ref→id resolver and its fail-closed rules, the scoped landing redirect, link
  and form-action generation, and client-side scope propagation.

### Modified Capabilities
<!-- `org-context`, `project-picker`, and `user-auth` are still un-archived
     deltas (changes org-context-and-project-picker / add-auth-project-frame),
     not yet under openspec/specs/. This change introduces web-routing rather
     than editing those in-flight deltas; the overlap (session context
     switching, picker links) is noted under Impact and will reconcile when
     those changes sync. -->

## Impact

- **Gateway routing** (`main.go`): app routes move under `/o/:orgRef` and
  `/o/:orgRef/p/:projectRef` groups; global routes (`/api`, `/auth`, `/share`,
  `/static`, `/assets`, `/webhooks`, `/profile`, `/orgs` list) stay top-level.
- **New** `scope.go`: `resolveOrgScope` / `resolveProjectScope` middleware.
- **Context** (`session_context.go`, `auth.go`, `org_context.go`,
  `project_handlers.go`): path becomes authoritative; cookie default only for
  `/`; `resolveDefaultProject` removed.
- **Shell / templates** (`ui.go`, `ui.templ`, ~18 `*.templ`): base-path
  threading, `linkURL` helper, sidebar/picker scoped hrefs, body data attributes.
- **Client JS** (`webui/static/js/*.js`): scope headers on fetch; stream URLs.
- **Tests**: gateway render/route tests and the Playwright e2e suite (URL
  fixtures) updated; new resolver and two-tab isolation tests.
- **Memory backend**: no API change in this change (UUID refs only).
