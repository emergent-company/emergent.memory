## 1. Server — `project:admin` token scope

- [ ] 1.1 Add `project:admin` to `ValidApiTokenScopes` and to the `oneof` tags of `CreateApiTokenRequest` / `CreateAccountTokenRequest` / `UpdateApiTokenScopesRequest` (`domain/apitoken/entity.go`). Verify: `go build ./...` compiles.
- [ ] 1.2 Add `ScopeImplies["project:admin"]` as a flat, explicit project-tier list (data/schema/agents/graph/branches/journal/skills/documents/search/chat) with **no** `admin` / `admin:all` / `admin:read` / `admin:write` / `mcp:admin` / `org:*` / `project:invite:create` / `discovery:*` members (`pkg/auth/middleware.go`). Verify: `go build ./...` compiles and the expansion test passes.
- [ ] 1.3 Add the mint gate `checkProjectAdminScopeGrant` in `Service.Create` / `CreateAccountToken` / `UpdateScopes` (project-bound required; account token refused with `403 project-admin-scope-denied`) backed by `Repository.CanGrantProjectAdmin` (project_admin of the project OR org_admin of the owning org). Verify: `go test ./...` in `domain/apitoken` passes.
- [ ] 1.4 Pin the expansion and the mint gate with unit tests (mirroring `TestValidApiTokenScopes` / `TestRoleScopeUmbrellaExpansionIsPinned`): project admin mints, org_admin mints, user/viewer refused, account token refused, expansion contains no platform/org scope. Verify: `go test ./...` passes.

## 2. Server — `PATCH /api/projects/:id/members/:userId`

- [ ] 2.1 Register `g.PATCH("/:id/members/:userId", h.UpdateMemberRole)` (`domain/projects/routes.go`) and the handler gated by `authorizeProject(..., accessProjectAdmin)` (project_admin or org_admin). Verify: `go build ./...` compiles.
- [ ] 2.2 Implement `Service.UpdateMemberRole`: validate role vocabulary (`project_admin | project_user | project_viewer`, else `400 invalid-role`), resolve membership, last-admin guard (`403 last-admin` on demoting the sole admin), persist, and revoke project tokens on downgrade via `tokenRevoker.RevokeByProjectAndUser` (non-fatal on failure). Verify: `go build ./...` compiles.
- [ ] 2.3 Add `UpdateMemberRoleRequest` DTO and swagger `@Router /api/projects/{id}/members/{userId} [patch]` annotations. Verify: `go build ./...` compiles.
- [ ] 2.4 Write unit tests covering: admin promotes/demotes, org_admin promotes/demotes, viewer/user refused (403), last-admin demotion refused (403), unknown role (400), missing member/project (404), downgrade revokes tokens, upgrade/no-op revokes nothing. Verify: `go test ./...` passes.

## 3. Gateway — Members section + scope picker

- [ ] 3.1 Add `/settings/members` route + `uiProjectMembers` handler rendering the members table; add a Members entry to `settingsSubNav` and the Settings sidebar group. Verify: `go build ./...` compiles and `/settings/members` renders.
- [ ] 3.2 Add member actions: `POST /settings/members/:userId/role` (role change), `POST /settings/members/:userId/remove`, and invite (reuse the existing invitation surface). Gate admin actions on the caller's project role (hide, not disable). Verify: `go build ./...` compiles.
- [ ] 3.3 Member visibility follows the existing `GET /api/projects/:id/members` contract (any project member or owning-org `org_admin`); the management actions (invite/remove/role) are admin-gated and hidden, not disabled. Verify: `go build ./...` compiles.
- [ ] 3.4 Offer `project:admin` in the token scope picker only to project admins of the active project; surface the server `403 project-admin-scope-denied` as a readable error otherwise. Verify: `go build ./...` compiles.
- [ ] 3.5 Write handler + `.templ` render tests (members list, admin-only controls hidden for non-admins, last-admin error surfaced, scope picker shows `project:admin` only for admins). Verify: `go test ./...` passes.

## 4. CLI + docs

- [ ] 4.1 Surface the new scope and the member role-change in the CLI where relevant (`apps/cli/`), keeping the token scope list and `projects team` surface consistent. Verify: `go build ./...` compiles.
- [ ] 4.2 Update `docs/security/authz-model.md` (project tier may hold `project:admin`; platform `admin`/`admin:all` unchanged; PATCH member-role endpoint in the project-membership rules). Verify: no build needed.
- [ ] 4.3 Run `task lint` and `go test ./...` across touched packages until clean. Verify: both succeed.

## 5. Verification

- [ ] 5.1 `openspec validate project-admin-scope-and-members` passes.
- [ ] 5.2 `go build ./...` from each touched module (server, gateway, CLI) compiles.
