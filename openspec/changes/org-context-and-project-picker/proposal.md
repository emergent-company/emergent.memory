## Why

The project switcher is a fixed-width dropdown that overflows horizontally once a user has many projects, and organizations exist only as inert labels — a project-picker grouping and a read-only tree page. There is no "organization" as a place to work: users can't open an org to see its projects, members, or settings, and a brand-new account has no guided path to create its first organization.

## What Changes

- Replace the project switcher dropdown with a single-column, vertically-scrolling picker grouped by organization, wide enough for long project names and full-width on mobile.
- Make each organization header clickable, with a per-org cogwheel, both opening an organization context (org sidebar).
- Add a "Recent" section to the picker, shown only when the user has more than ten projects.
- Add a sticky picker footer with "Add New Project" and "New Organization" actions.
- Introduce an organization navigation context — an org sidebar with Projects, Members, Invites, Tool settings, and Delete — reached from the picker's org header or cogwheel.
- Add context switching on the session: activate a project (default), activate an organization, or clear context entirely.
- Add a no-organization onboarding wizard as the only view shown when the user belongs to zero organizations.
- Auto-select the last-used project on return, persisted in a durable signed cookie (per account, survives sign-out).
- Fold the project sidebar's "Workspace" group (Organizations, Members) into the org context rather than keeping account-level entries inside project navigation.

## Capabilities

### New Capabilities
- `org-context`: organization as a first-class navigation context — the org sidebar (projects, org members, invites, tool settings, delete), the org landing view, the no-org onboarding wizard, and session context switching between none / org / project.
- `project-picker`: the redesigned project/org picker dropdown — single-column vertical-scroll list grouped by org, clickable org headers with cogwheels, a conditional "Recent" section, a sticky footer, and last-used-project auto-selection on return.

### Modified Capabilities
<!-- None. The adjacent capabilities this builds on (project-management from
     add-auth-project-frame, org-membership from add-org-members-ui) are still
     un-archived delta specs, not yet merged under openspec/specs/. This change
     introduces new org-context and picker behavior rather than editing those
     in-flight deltas; the overlap is noted under Impact. -->

## Impact

- **Gateway (Go + templ)**: `projectSwitcher` and `newProjectModal` (auth_ui.templ), the app shell and sidebar (`ui.templ`, `sidebar_user_templ.go`, `ui.go`), session context switching (`session.go`, `session_context.go`, `project_handlers.go`, `auth.go`), and route registration (`main.go`).
- **New gateway surfaces**: org context sidebar + landing, org members page, org tool-settings page, org delete confirm, no-org wizard, and the redesigned picker.
- **New `MemoryClient` methods**: `GetOrg`, `DeleteOrg`, `ListOrgMembers`, and org tool-settings list/upsert/delete — plumbing for backend endpoints that already exist (`/api/orgs/{id}`, `/api/orgs/{id}/members`, `/api/admin/orgs/{orgId}/tool-settings`).
- **Landing behavior**: `/` no longer always redirects to `/agents`; it resolves to the last-used project, the org context, or the wizard depending on state.
- **Overlaps** `add-auth-project-frame` (project switch/create) and `add-org-members-ui` (org list/create, members, invites). This change builds a navigation layer on top of both; the merged capability definitions will be reconciled when those changes sync.
- **Not in scope**: organization rename/description (no Memory backend endpoint exists), org-level provider/model config, cross-device preference sync.
