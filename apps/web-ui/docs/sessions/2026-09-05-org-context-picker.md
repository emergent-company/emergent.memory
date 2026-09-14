# 2026-09-05 — Org context + project picker redesign

## Goal
Elevate organization from a mere grouping label in the project switcher to a first-class navigation context (own sidebar, landing, members, tool settings, delete), and redesign the project/org picker (single column, conditional "Recent" section, sticky footer, clickable org headers + cogwheel). Persist the last-used project per account for auto-select on return.

## Outcome
Done. All implementation committed, build/lint/vet/tests green, plus a live `curl` smoke test of the dev server. The two manual-browser tasks (mobile full-width check, full interactive smoke) remain pending in the OpenSpec change's `tasks.md`, but were exercised interactively this session — the user reported and I fixed indent, project-click, width, and label-centering issues across several follow-up commits.

## Decisions
- **Three-state context (none/org/project)** reuses the existing session `ProjectID`/`OrgID` fields via new `activateOrg`/`clearContext` — no schema or cookie-shape change.
- **Durable `memory_recent_projects` signed cookie** (per-account keyed by `Sub`, survives sign-out, mirrors `memory_install`) drives last-used auto-select + the picker Recent section — chosen over `localStorage` because the app is server-rendered and the server must read the last project on the first request.
- **Sidebar selected from context, not route** — the context is authoritative and already resolved once per request in `page()`.
- **No Memory-side change** — every org endpoint already exists (`/api/orgs/{id}`, `/members`, admin tool-settings); only gateway `MemoryClient` plumbing + UI were added.
- **`groupProjectsByOrg` includes empty orgs** (project-less orgs stay reachable) but returns nil on zero projects (preserves the "No projects yet" empty state).
- **Project switch from org context redirects to the project landing** (`/agents`), not back to the Referer org page — whose `resolveOrg` re-activates the org and would undo the switch.

## Changes
- `gateway/ui.go` — `orgSidebarGroups()`, context-aware sidebar selection in `page()`, recent-projects computation, dropped the "Workspace" group from project nav.
- `gateway/ui.templ` — `appShell` renders the sidebar only when `groups != nil`; new params (`activeOrg`, `recentProjects`, `showRecent`).
- `gateway/auth_ui.templ` — picker redesign (single column, org headers + cogwheel, Recent section, sticky footer, wide-on-mobile); `newProjectModal` preselects the active org.
- `gateway/org_context.templ` + `org_context.go` (new) — org landing/members/tool-settings pages + delete; handlers (`uiOrg`, `uiOrgMembers`, `uiOrgToolSettings`, `uiDeleteOrg`, `uiRoot`); `resolveOrg` activates the org and mirrors it into the request context so `page()` renders the org sidebar on the activating request.
- `gateway/project_ui.go` — `uiActivateProject` redirect fix + `isOrgContextPath`.
- `gateway/main.go` — org routes + `/` → `uiRoot`.
- `gateway/org_members_ui.go` — `uiCreateOrg` redirects to the new org landing.
- `gateway/session.go` / `auth.go` / `project_handlers.go` — `activateOrg`, `clearContext`, `memory_recent_projects` cookie (Phase A lanes).
- `gateway/memory.go` / `backend.go` — `GetOrg`, `DeleteOrg`, `ListOrgMembers`, org tool-settings client methods.
- `gateway/org_context_test.go` (new) — org landing/members/tool-settings/delete/redirect/sidebar/isOrgContextPath tests.
- `openspec/changes/org-context-and-project-picker/` — proposal/design/specs/tasks.

## Verification
- `go build ./...` — pass
- `PATH=/root/go/bin:$PATH templ generate` — pass
- `PATH=/root/go/bin:$PATH golangci-lint run ./...` — 0 issues
- `go vet ./...` — clean
- `go test -count=1 ./...` — all green
- `curl http://localhost:8095/agents` — picker + org header links + sticky footer render

## Open questions / follow-ups
- Manual browser tasks 4.5 (mobile full-width) + 6.5 (interactive smoke) still pending in the OpenSpec change — see `archive-org-context-picker`.
- Org rename/description — no Memory backend endpoint; needs Memory-side work (see `org-rename-description`).
- Org-scoped invites — currently folded into the members page, no dedicated org-invites surface.
- `groupProjectsByOrg` + picker behavior overlaps the in-flight `add-auth-project-frame` / `add-org-members-ui` changes — reconcile on archive.
- Server-side (Memory) user preferences for "recent projects" — the durable cookie is per-browser; cross-device sync is a future Memory feature (roadmap, not a task).

## Tasks
- [archive-org-context-picker](../tasks/archive-org-context-picker.md) — browser-verify + archive the change
- [org-rename-description](../tasks/org-rename-description.md) — org rename/description (Memory backend work)
