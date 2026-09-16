## 1. Context switching foundation

- [x] 1.1 Add `activateOrg(ctx, orgID)` to set the session's `OrgID` and clear `ProjectID`, re-issuing the session cookie (mirrors `activateProjectInSession`); verify with a unit test that an active org + no project is persisted
- [x] 1.2 Add `clearContext(ctx)` to clear both `OrgID` and `ProjectID`; verify with a unit test that both fields are empty afterward
- [x] 1.3 Add `POST /orgs/:id/activate` wired to `activateOrg`, redirecting back to the referring page; verify the route test switches the active org and clears the project
- [x] 1.4 Add a signed `memory_recent_projects` cookie (issue/verify, survives sign-out like `memory_install`, per-account keyed by `Sub`, capped at 8); verify with unit tests for sign/verify, ordering, and the cap
- [x] 1.5 Record the project ID into the recent-projects cookie on `activateProjectInSession` (and clear the current context on `clearContext`); verify with a unit test that switching a project prepends it to the recent list

## 2. MemoryClient org methods

- [x] 2.1 Add `GetOrg` (GET `/api/orgs/:id`) and verify with a unit test decoding the org DTO
- [x] 2.2 Add `ListOrgMembers` (GET `/api/orgs/:id/members`) and verify with a unit test decoding member roles/identity
- [x] 2.3 Add `DeleteOrg` (DELETE `/api/orgs/:id`) and verify with a unit test for the delete request path
- [x] 2.4 Add org tool-settings list/upsert/delete (`/api/admin/orgs/:orgId/tool-settings`) and verify with unit tests for each verb

## 3. Org context UI

- [x] 3.1 Add the org sidebar group set (Projects, Members, Invites, Tool settings, Delete) and derive sidebar selection from the active context (none/org/project); verify a test asserts org context renders org nav and not project nav
- [x] 3.2 Add the org landing view (the active org's projects, with an empty state + create CTA); verify with a templ render test for the populated and empty states
- [x] 3.3 Add the org members page backed by `ListOrgMembers`; verify with a templ render test for members and the single-member case
- [x] 3.4 Add the org tool-settings page (list, update, delete); verify with a templ render test for the list state and an update POST test
- [x] 3.5 Add the delete-org confirm flow (`POST /orgs/:id/delete`) returning to the wizard/picker; verify with a route test for the admin delete and the non-admin rejection
- [x] 3.6 Pre-scope project creation to the active org (preselect `orgId` in the create modal); verify with a templ render test that the active org is preselected

## 4. Picker redesign

- [x] 4.1 Rebuild the picker as a single-column vertical-scroll list with truncating org headers (fix the horizontal overflow); verify with a templ render test that long names truncate and no horizontal scroll markup remains
- [x] 4.2 Make org headers clickable and add a per-org cogwheel, both activating the org context; verify with a templ render test for the header link and cogwheel action
- [x] 4.3 Add the conditional "Recent" section (shown only when total projects > 10); verify with tests for the over-threshold and under-threshold cases
- [x] 4.4 Add the sticky footer with "Add New Project" and "New Organization" actions; verify with a templ render test that the footer renders as a fixed action outside the scroll region
- [ ] 4.5 Render the picker full-width on mobile; verify with a manual browser check at a mobile viewport via the dev server
- [x] 4.6 Auto-select the last-used project on `/` when the session has no active project; verify with a route test for the auto-select and no-recent fallback

## 5. No-org wizard + landing

- [x] 5.1 Add the no-org onboarding wizard as the only view when the user has zero organizations; verify with a route/templ test for the zero-org state and the wizard submit
- [x] 5.2 Repoint `/` to resolve context: last-used project → activate; else org context when ≥1 org; else the wizard; verify with route tests for all three branches

## 6. Verification

- [x] 6.1 Run `go build ./...` from `gateway/` and confirm it compiles
- [x] 6.2 Run `templ generate` and confirm no template errors
- [x] 6.3 Run `task lint` and fix any findings
- [x] 6.4 Run `go test ./...` from `gateway/` and confirm all tests pass
- [ ] 6.5 Manual browser pass via `task dev`: picker scroll, org switch via header/cogwheel, no-org wizard, last-used auto-select
