## Context

The gateway is a Go/templ server-rendered app. The session (`sessionContext`) already carries both `ProjectID` and `OrgID`; `activateProject`/`activateProjectInSession` re-issue the signed session cookie on project switch (`project_handlers.go`). The signed-cookie infrastructure for a durable, logout-surviving cookie already exists (`memory_install`, `auth.go:121`). Memory's backend already exposes the org endpoints this change surfaces: `GET/POST/DELETE /api/orgs[/:id]`, `GET /api/orgs/:id/members`, and `GET/PUT/DELETE /api/admin/orgs/:orgId/tool-settings`. See proposal.md for motivation.

## Goals / Non-Goals

**Goals:**
- Add organization as a navigation context with its own sidebar and landing, plus session switching between none / org / project.
- Redesign the picker into a single-column vertical list with org headers/cogwheels, conditional recent section, and sticky footer.
- Persist last-used project per account in a durable cookie and auto-select it on return.
- Surface the org members and org tool-settings backend endpoints through new `MemoryClient` methods and gateway pages.

**Non-Goals:**
- Organization rename/description (no Memory endpoint exists — separate backend work).
- Cross-device preference sync (cookie is per-browser; server-side user preferences are a future Memory feature).
- Org-level provider/model configuration (project-scoped only).
- Re-parenting the project-scoped `/members` page is deferred; this change adds org-scoped members and does not relocate project membership management.

## Decisions

### 1. Three-state context reuses the existing session fields

Encode context as: `ProjectID=="" && OrgID==""` (none/wizard), `OrgID!="" && ProjectID==""` (org), `ProjectID!=""` (project). Add `activateOrg` (set `OrgID`, clear `ProjectID`) and `clearContext` (clear both), mirroring `activateProjectInSession`.

*Rationale:* the session already has both fields and already re-issues the cookie on switch; no schema or cookie-shape change. *Alternative considered:* a separate enum field — rejected, redundant with the existing pair and risks desync.

### 2. Durable last-used cookie mirrors `memory_install`

A signed `memory_recent_projects` cookie, long-lived and not cleared on sign-out, holding a per-account (keyed by `Sub`) ordered list of recent project IDs. Auto-select = `recent[0]` when the session has no active project; the picker's Recent section = the list intersected with still-accessible projects, shown only when total project count > 10.

*Rationale:* the app is server-rendered, so the server must read the last project on the first request — a cookie avoids the client-side flicker that `localStorage` would cause. *Alternative considered:* `localStorage` — rejected (needs a JS boot round-trip to activate; can't drive server-side redirect or the server-rendered Recent section).

### 3. Sidebar is derived from context, not route

The shell resolves the active context (none/org/project) once per request and picks the sidebar group set accordingly: project context → current `sidebarGroups()`; org context → Projects/Members/Invites/Tool settings/Delete; none → no sidebar (wizard). The project sidebar's "Workspace" group is dropped (its entries move to org scope).

*Rationale:* the context is authoritative and already resolved in `page()` (`ui.go`); route-only detection would mislabel deep org pages. *Alternative considered:* a separate layout per context — rejected, one shell with a context switch is simpler and keeps the topbar/picker consistent.

### 4. New `MemoryClient` methods, no backend changes

Add `GetOrg`, `DeleteOrg`, `ListOrgMembers`, and org tool-settings list/upsert/delete to the gateway `MemoryClient`, hitting existing Memory endpoints. No Memory-side work.

*Rationale:* every needed endpoint already exists in the Memory `orgs` domain; this is pure gateway plumbing.

### 5. Landing redirect is context-aware

`/` resolves: last-used project (durable cookie) → activate it; else org context if the user has ≥1 org; else the no-org wizard. Replaces the current unconditional `/agents` redirect.

*Rationale:* a fresh user with no org should land on the wizard, not a broken project page; a returning user should land where they left off.

### 6. Picker is a single flex `<ul>` with a sticky footer

The dropdown list becomes a flex column: the grouped list is the scroll region (`overflow-y-auto`), the footer (`Add New Project` + `New Organization`) is `shrink-0` so it never scrolls. Org headers are clickable (→ org context); each has a cogwheel (→ org context); project rows POST `/projects/activate`. Recent-section visibility and total count are computed server-side from `len(projects)`.

*Rationale:* matches the existing daisyUI menu markup while fixing the horizontal overflow (the current untruncated `menu-title` org headers are the overflow source) and pinning the actions.

### 7. Role gating mirrors existing conventions

Org tool settings and delete are shown only to `org_admin`; members are visible to all org members. The backend already enforces `requireOrgMember`; the UI reflects role rather than relying on it.

*Rationale:* consistent with the role-scoped access already specified in `org-membership`; avoids surfacing actions the user cannot perform.

## Risks / Trade-offs

- [Durable cookie grows with recent-project history] → Cap the list (≈8 entries) and namespace by `Sub` so multiple accounts on one browser stay separate.
- [Recent section references deleted/inaccessible projects] → Intersect the cookie list with `ListProjects` before rendering; drop unknown IDs.
- [Context-aware `/` adds a redirect hop] → Resolve synchronously in the `/` handler with the already-available session + cookie; no extra round-trip.
- [Pinning the footer inside daisyUI's `.menu` markup is fragile] → Keep the `<script>`/modal outside the `<ul>` (existing `projectSwitcher` already documents this trap) and place the footer as a final `shrink-0` list item, not a sticky-positioned overlay.
- [Org members vs project members confusion] → Label the org page clearly as organization members; project members remain on the existing page and are not part of this change.

## Migration Plan

1. Add `activateOrg`/`clearContext` + durable recent-projects cookie (backend only; existing flows unaffected).
2. Add `MemoryClient` org methods (`GetOrg`, `DeleteOrg`, `ListOrgMembers`, tool-settings).
3. Build org context: sidebar, landing, members page, tool-settings page, delete confirm.
4. Redesign the picker (single column, headers/cogwheels, recent section, sticky footer).
5. Add the no-org wizard and re-point `/`.
6. No database migration is required (cookie + session reuse only); rollback is a revert of the gateway change.

## Open Questions

- Exact cookie name and encoding for the recent-project list (JSON map `sub → [ids]` vs a single flat list) — cosmetic, no spec impact.
- Whether the picker's "Recent" section should also render when the current project would otherwise be the only recent entry — minor polish, no spec impact.
