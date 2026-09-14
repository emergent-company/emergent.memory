# 2026-09-08 — Org Settings hub (replaces Tool settings)

## Goal

In the Organization view, replace "Tool settings" with "Settings" — same functionality
(tools listed + a Danger Zone), laid out like the Project Settings pages with the in-page
side-menu rail.

## Outcome

**Done.** Three commits on `master`:

- `c3b15d7` — UI: org sidebar narrows to Projects / Members / Settings; new Settings hub
  (side rail Tools / Danger zone) mirroring the project-settings two-column layout.
- `8884310` — backend + tests: handlers/routes renamed, danger-zone page handler, PRG
  redirect repointing, 301 compat redirect, unit + e2e test updates.
- `add7a3c` — spec sync (`docs/spec/00-vision.md` D19, `docs/spec/04-go-application.md`).

Org tool-override rows, their Enable/Disable/Delete POST actions, and the delete-org form
action/confirm string are byte-identical to before.

## Decisions

- **Org sidebar = Projects / Members / Settings** — drop the separate "Delete" entry; org
  deletion lives in the Settings hub's Danger zone (user-confirmed). Previously "Delete"
  and "Tool settings" both pointed at the same page.
- **Settings is a hub with an in-page side rail** (Tools / Danger zone), mirroring the
  project Settings layout (`lg:grid-cols-[11rem_minmax(0,1fr)]` + sticky aside + section
  cards) — user picked this over a single stacked page.
- **Keep POST routes `/orgs/:id/tool-settings/...`** for the toggle forms — the tool rows
  and their actions are unchanged; only the page URL moved. Old page GET
  `/orgs/:id/tool-settings` 301-redirects to `/orgs/:id/settings` for back-compat.
- **Delete-org failure redirects to the danger-zone page** (`/settings/danger-zone?err=`),
  not the org landing — errors surface where the action lives.
- **Spec sync deferred until a parallel lane committed** its docs WIP — avoided sweeping
  another session's uncommitted changes into this commit.
- **Split lane work**: @designer owned the templ/nav UI; @fixer owned Go handlers/routes/
  tests. A shared exact symbol contract (`OrgSettingsPage`, `orgSettingsSubNav`, etc.)
  let the lanes run against each other and merge on build.

## Changes

- `gateway/ui.go` — `orgSidebarGroups`: org items now Projects / Members / **Settings**
  (`/orgs/:id/settings`, icon `lucide--settings`); "Tool settings" and "Delete" items removed.
- `gateway/auth_ui.templ` — project-switcher org cogwheel href → `/orgs/:id/settings`.
- `gateway/org_context.templ` — new `orgSettingsSubNav` (Tools / Danger zone rail),
  `OrgSettingsPage` (Tools section at `/orgs/:id/settings`: override rows in a bordered
  card, empty state "No tool overrides"), `OrgSettingsDangerZonePage` (breadcrumbed header +
  danger-zone card at `/orgs/:id/settings/danger-zone`); `orgToolSettingRow` preserved;
  old `orgDangerZone` templ removed (absorbed into the danger-zone page).
- `gateway/org_context.go` — `uiOrgToolSettings` → `uiOrgSettings`; new
  `uiOrgSettingsDangerZone`; `orgToolSettingsPageData` → `orgSettingsPageData` + new
  `orgSettingsDangerZonePageData`; toggle/delete PRG redirects now land on
  `/orgs/:id/settings(?updated=1)`; `uiDeleteOrg` error redirect → danger-zone page.
- `gateway/main.go` — new GET `/orgs/:id/settings` + `/orgs/:id/settings/danger-zone`;
  301 compat redirect for old GET `/orgs/:id/tool-settings`; POST routes unchanged.
- `gateway/org_context_test.go` — `TestUIOrgSettings`, new `TestUIOrgSettingsDangerZone`
  (200 + delete form + rail link), delete-failure redirect subtest, sidebar href
  assertions; fixture/registration URLs updated.
- `gateway/org_members_ui_test.go` — org sidebar href expectations → `/settings`.
- `tests/e2e/specs/org.spec.ts` — org settings test navigates `/orgs/{id}/settings`, expects
  /Settings/.
- `tests/e2e/specs/org-delete-ui.spec.ts` — reaches Danger zone via Settings → "Danger zone"
  rail link.
- `docs/spec/00-vision.md`, `docs/spec/04-go-application.md` — org sidebar set and Settings
  hub described (D19, Organizations bullet).

## Verification

- `PATH="/root/go/bin:$PATH" templ generate` — exit 0.
- `go build ./...` (module root `gateway/`) — pass.
- `go test -count=1 ./...` — pass (root + webui; org settings/delete/landing/sidebar targeted
  tests pass).
- `go vet ./...` — pass.
- `golangci-lint run ./...` — 0 issues (`task lint` unavailable — lefthook missing).
- No live-server/browser pass this session — server-rendered output covered by unit HTML
  assertions (titles, nav hrefs, rail links, delete form); layout is a verbatim mirror of the
  established project-settings pattern. See follow-up task.

## Open questions / follow-ups

- Org rename (the Settings hub would be the natural home for a "General" section) is already
  tracked as `org-rename-description`.
- The org-context-and-project-picker OpenSpec change (still unarchived) describes the old
  "Tool settings/Delete" sidebar — update/archive it alongside the existing
  `archive-org-context-picker` task when that change is finalized.

## Tasks

- [verify-org-settings-hub-browser](../tasks/verify-org-settings-hub-browser.md) — manual
  browser + e2e pass over the org Settings hub (rail, tools, danger-zone delete).
