# Implementation tasks

## 1. Shared org-administration seam

- [x] 1.1 Add `RoleOrgAdmin` and `CanAdministerOrg(ctx, db, orgID, userID)` to `pkg/auth/entitlement.go`: `kb.organization_memberships`, `role = 'org_admin'`, fail-closed, **no** platform-superadmin arm and **no** project tier
- [x] 1.2 Add `CanAdministerOrgOrPlatform(ctx, db, orgID, userID)` composing `CanAdministerOrg` with the shared `superadmin_full` read, for the invitation surfaces that already admitted a platform superadmin

## 2. Route the remaining org-scoped decision points through the seam

- [x] 2.1 `domain/orgs`: add `Repository.IsOrgAdmin` delegating to `CanAdministerOrg`; make `requireOrgAdmin` (org settings mutations, rename, delete, member list) consume it
- [x] 2.2 `domain/invites`: `Handler.mayAdministerOrg` (org-admin invite create, revoke) and `Service.inviterIsOrgAdmin` (accept-time re-check) consume `CanAdministerOrgOrPlatform`, replacing the bespoke `EXISTS` SQL
- [x] 2.3 `domain/projects`: `AuthorizeOrgAdmin` (REST + MCP project create), `Transfer` (source-org), and `authorizeProject`'s `accessOrgAdmin` branch (delete/restore) consume `CanAdministerOrg`, replacing local role comparisons

## 3. Raw `X-Org-ID` trust (Q10)

- [x] 3.1 `RequireAuth` no longer assigns the raw `X-Org-ID` header to `user.OrgID`; with no project-derived org the org is left empty in every posture (fail closed)
- [x] 3.2 Keep the mismatched-header `403` when a project-derived org exists

## 4. Fail-first and regression tests

- [x] 4.1 `pkg/auth`: fail-first `TestRequireAuthNilDBDoesNotTrustOrgHeader` (red before 3.1, green after)
- [x] 4.2 `pkg/auth`: `CanAdministerOrg` refuses a platform superadmin and a `project_admin`; `CanAdministerOrgOrPlatform` admits only `org_admin`/`superadmin_full` (not `superadmin_readonly`)
- [x] 4.3 `domain/orgs`: superadmin-not-org-admin is refused on rename/delete; spoofed `X-Org-ID` cannot pass an org-scoped decision

## 5. Verification

- [x] 5.1 `cd apps/server && PATH="/root/go/bin:$PATH" go build ./...`
- [x] 5.2 `PATH="/root/go/bin:$PATH" go test -short ./pkg/auth/... ./domain/{orgs,invites,projects}/...`
- [x] 5.3 DB-backed tests for `pkg/auth/...` and the org/invites/projects authorization matrices under a hermetic Postgres (`REQUIRE_DB=1`, never with `-short`)
- [x] 5.4 `openspec validate --all --strict`
- [x] 5.5 `apps/server/scripts/lint-ratchet.sh` — auth guards stay `<= 13`, no `.WithMessage`/`.WithInternal` added
- [x] 5.6 `gofmt -l` clean on changed files
