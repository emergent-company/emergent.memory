# 2026-09-09 — Org-create stale shell under hx-boost

## Goal

Follow-up triage of the documented session artifacts → highest-priority P0:
`boost-context-stale-shell` audit. Confirm every context-changing navigation
either full-loads or re-renders the shell, and fix what doesn't.

## Outcome

Done. Audit found exactly one real stale-shell defect — org creation. Fixed and
unit-tested; task `boost-context-stale-shell` flipped to done.

## Audit

Boost scope is `<main id="main-content" hx-boost="true" hx-target="#main-content"
hx-swap="innerHTML">` (`gateway/ui.templ`); only descendants of `#main-content`
boost-swap. Per-flow verdicts:

- Project switch (topbar rows, org-landing "Open", `/orgs` rows) — shell-native
  or `render.RedirectAfterMutation` (HX-Redirect full load) → safe.
- Org entry (`GET /orgs/:id` from topbar / server redirects) — shell-native → safe.
- Account switch + sign-out (`account_menu.templ`, topbar-only render site) —
  shell-native forms → safe.
- Org delete, avatar modal, transfer modal — `hx-boost="false"` → safe.
- **Org creation — BUG.** `/orgs/new` form (`org_members_ui.templ`) is inside
  `#main-content`; `uiCreateOrg` answered a boosted POST with a plain 303. htmx
  followed it as a boosted GET and swapped only `#main-content`, leaving the
  topbar org switcher / sidebar on the previous org.

## Changes

- `gateway/org_members_ui.go` — `uiCreateOrg` success redirect →
  `render.RedirectAfterMutation(...)` (200 + HX-Redirect for HTMX, 303 for
  plain POSTs), matching the org-delete / project-activate precedent. Creating
  an org activates it, so the shell must full-load into the new context.
- `gateway/org_members_ui_test.go` — HTMX create-org case: 200 + `HX-Redirect:
  /orgs/org-2` (plain POST 303 case unchanged).
- `docs/tasks/boost-context-stale-shell.md` — done + resolution notes.

## Verification

- `go build ./...` (gateway) — green.
- `go test ./...` (gateway) — green (full package incl. new HTMX case).
- `golangci-lint run ./...` (gateway) — 0 issues. (`task lint` unavailable:
  lefthook not installed on this box.)

## Open questions / follow-ups

- `POST /orgs/:id/activate` route (`main.go`, `project_ui.go:uiActivateOrg`) was
  orphaned — no UI/e2e/JS posts to it (all org/project switching goes through
  `/projects/activate` or `GET /orgs/:id`). Removed later this session:
  route registration + handler + its two tests deleted.
- Invite accept/decline (`uiAcceptInvite`/`uiDeclineInvite`) still plain-303
  under boost: content swap is consistent, but a just-joined org won't appear
  in the topbar switcher until a full load. Optional HX-Redirect hardening.
- Parallel lane (same checkout, uncommitted WIP left untouched) owns
  `e2e-model-warning-fixture-via-ui` — agent-model-warning e2e env fix deferred
  to that lane; not touched here.

## Also this session (2026-09-09, follow-on triage execution)

- **humanize-span-durations** — `formatSpanDuration` delegates ≥60s spans to
  `formatRunDuration` (75s → "1m", 2.5h → "2h 30m"); task closed.
- **Spec sync** — `docs/spec/04-go-application.md` §3 page list now matches
  shipped pages (MCP servers are API-registered + agent-tools-picker surface;
  added Sessions, Usage, Documents, Skills, Schedules, API Tokens, Approvals,
  Blueprints gallery). #383 merge recorded in its task file.
- **EmptyState children regression** — parallel lane's EmptyState refactor
  (`61662eb`) broke `TestUIOrgLandingEmpty`: go-daisy `EmptyState` took a
  variadic `children` param, but templ bodies travel via ctx, so every
  `EmptyState(props){CTA}` silently dropped its body. Fixed upstream in go-daisy
  (`53797be`), bumped in gateway (`3a5b56b`); verified full suite green.
- **rebrand-app-assets** — PWA icons (apple-touch 180/192/512) regenerated to
  the M monogram (oklch-matched gradient + ink, Pillow via uv); manifest's
  stale description dropped; task closed.
- **Delete-project flash softening** — async (202) delete now flashes
  "Deletion started." / "N deletions started." instead of claiming the project
  is gone; e2e expectation updated.
- **"No active project" UX** — `/settings` GET with no active/static project
  now redirects to `/orgs` (picker) instead of a misleading error card; the
  defensive inline-save path toasts "Select a project first."
- **account-switch-row-email-backfill** — cached switch-account rows get their
  email backfilled from the account's own Memory profile at the two
  registry-put transitions (select_account re-add + auth switch); task closed.
- **emergent.memory: agent-effective-model** — implemented on branch
  `feat/agent-effective-model` (see its task file): generative resolution now
  falls back to the provider-credential model (mirrors executor), GET reports
  per-agent overrides unconditionally, list gains `effectiveModel`. Awaiting
  review/merge there.
- Parallel lane kept committing gateway/e2e work throughout (EmptyState CTA
  migration, MCP management WIP in `backend.go`/`mcp_servers_client.go`,
  e2e scenario restructure) — all left untouched / committed separately.
- **go-daisy popover corner placements** — upstream `437a72d` (`-start`/`-end`
  corner placements + synchronous positioning); gateway pinned; CommandPalette
  keyboard fix upstream `1edc808` + workaround removed (`ad118ca`).
- **Two UI lanes (worktrees → PRs → merged, CI green):**
  - `PR #5` (`936fb98`): org-landing row action menus migrated to go-daisy
    `ui.Popover` (bottom-end, exclusive); hand-rolled popover script removed;
    transfer-dialog close via `goDaisy.popover.closeAll()`; e2e selectors →
    `data-gd-popover-*`.
  - `PR #6` (`b875f3c`): schema-declared object type icons/colors rendered on
    object rows, object detail header, and schema browser surfaces
    (`gateway/type_ui.go` + `type_ui.templ`; emoji vs iconify glyph handling;
    inline-style color accents; byte-identical fallback).
  - Both still need a browser visual/e2e pass (see their task files).
- **emergent.memory PRs opened** for the earlier branches: `#403`
  (agent-effective-model), `#404` (provider-model-catalog-resync) — awaiting
  review/merge there.
- OpenSpec archive batch (implemented-but-unarchived changes) remains gated on
  the sibling lane's long-uncommitted `BACKLOG.md`/spec docs.
