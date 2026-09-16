## 1. Memory client methods

- [x] 1.1 Add org DTOs + `ListOrgs` (`GET /orgs`) and `CreateOrg` (`POST /orgs`, name) to `MemoryClient`; verify a unit test against the fake memory backend asserts the bare-array decode and create body
- [x] 1.2 Add `GetOrgsAndProjects` (`GET /user/orgs-and-projects`) returning the org→project access tree with per-level roles; verify a unit test decodes `OrgWithProjectsDto` including nested projects
- [x] 1.3 Add member methods `ListMembers` (`GET /projects/{id}/members`) and `RemoveMember` (`DELETE /projects/{id}/members/{userId}`); verify unit tests cover list decode and the `last-admin` 403 surfacing
- [x] 1.4 Add invite methods `ListInvites` (`GET /projects/{id}/invites`), `CreateInvite` (`POST /invites`), `AcceptInvite` (`POST /invites/accept`), `ListPendingInvites` (`GET /invites/pending`), `DeclineInvite` (`POST /invites/{id}/decline`), `CancelInvite` (`DELETE /invites/{id}`); verify unit tests assert the request DTOs carry the exact role enum strings
- [x] 1.5 Add `SearchUsers` (`GET /users/search?email=`) and profile `GetProfile`/`UpdateProfile` (`GET/PUT /user/profile`); verify unit tests cover the query param, decode, and update body

## 2. Handlers + routes

- [x] 2.1 Add org handlers (list, create) and routes behind the session middleware; verify handler tests cover empty list and create-success and create-conflict (409) paths
- [x] 2.2 Add member handlers (list, remove) and routes; verify handler tests cover list, remove-success, and last-admin-blocked paths
- [x] 2.3 Add invite handlers (sent list, create, pending list, accept, decline, cancel) and routes; verify handler tests cover each success path and the not-pending/forbidden error paths
- [x] 2.4 Add user-search and profile handlers and routes; verify handler tests cover search-with-matches, search-empty, profile-read, and profile-update
- [x] 2.5 Extend the create-project handler/form from `add-auth-project-frame` to require and forward `orgId`; verify a handler test rejects a project create missing `orgId`

## 3. Web UI (templ)

- [x] 3.1 Add the organizations view (access-tree list) with a "Create organization" button that opens a separate `/orgs/new` create page; verify render tests assert the list renders and the create-page form renders
- [x] 3.2 Add the merged Members view (confirmed members + pending invitations rendered as "Not responded" members) with role/identity columns; verify a render test asserts members render with role, pending invites render with the not-responded badge, and a sole-admin remove affordance is suppressed
- [x] 3.3 Add the "Add member" button + a separate invite page (`/members/new`) with email + search autofill + role selector; verify render tests cover the invite form, search results, and revoke on the members view
- [x] 3.4 Add the member details page (`/members/:userId`, read-only); verify a render test asserts name/email/role/joined render
- [x] 3.5 Add the profile page (view + edit) with a "Pending invitations" section (accept/decline for the signed-in user); verify render tests cover profile fields, edit form, and pending accept/decline
- [x] 3.6 Wire the org selector into the create-project form; verify a render test asserts the selector is populated and required

## 4. Verification

- [x] 4.1 `go build ./...` + `go vet ./...` + `go test ./...` in `gateway/` pass (verified on the live tree)
- [x] 4.2 `templ generate` produces no diff; `golangci-lint run` reports one pre-existing `auth_ui_test.go` QF1001 (not this change); `task lint` wrapper needs `lefthook` (not installed in this env)
- [ ] 4.3 Manual browser test: create org (separate page), create project under it, add a member (invite), accept a pending invite, remove a member, view member details, edit profile — each step observable in the DevTools browser
