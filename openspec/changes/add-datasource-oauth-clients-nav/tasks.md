# Tasks

<!-- openspec:archive-hold: the predecessor change add-datasource-oauth-app-setup is still
archive-held on main, so the main spec this change ADDs to (web-data-sources) does not exist yet;
archiving now would sync requirements describing the OAuth-client pages onto an absent capability.
Archive after add-datasource-oauth-app-setup lands. -->

> Design/spec stage. Implementation ships on this same branch/PR (spec + implementation together);
> no task is complete until its code + tests land.

## 1. Shared nav payload

- [x] 1.1 Add `IsProjectAdmin` to `settingsNavCommon` and a `settingsNavPayload(c, ctx)` helper
        that resolves the providers-missing badge plus the caller's project-admin role
        (fail-closed), reusing `activeProjectIDFromContext` + `projectAdminForCaller`.
- [x] 1.2 Switch every settings handler that sets `data.ProvidersMissing` to populate the embedded
        `settingsNavCommon` via the helper.

## 2. Rails + pages

- [x] 2.1 `settingsSubNav` takes `isProjectAdmin` and appends the "Data source OAuth clients" entry
        (scoped `href /settings/oauth-clients`) between "Connected devices" and "Budget" for admins
        only.
- [x] 2.2 `orgSettingsSubNav` adds the "Data source OAuth clients" entry after "Tools", gated on
        `isAdmin`.
- [x] 2.3 `dsOAuthClientsPage` renders inside `layout.Rail`; `ProjectOAuthClientsPage` /
        `OrgOAuthClientsPage` pass the correct rail with the oauth-clients entry active.
- [x] 2.4 Thread `isProjectAdmin` through `ProjectMembersPage`, `providerConfigForm`, and
        `providerConfigPage`.
- [x] 2.5 Data Sources header shows an "OAuth clients" outline link (`data-testid`
        `ds-oauth-clients-link`) for project admins only.

## 3. Tests + verification

- [x] 3.1 Update existing render tests for the new signatures.
- [x] 3.2 Add tests: rail entry present for admin / absent for non-admin; OAuth page in-rail with
        active entry; Data Sources link admin-only.
- [x] 3.3 `templ generate`, `go generate`, `go build ./...`, `go test ./...`, `task lint` pass.

## 4. Docs

- [x] 4.1 Update `docs/site/user-guide/datasources.md` with the new discoverability paths (project
        rail, org rail, Data Sources page shortcut).
