## 1. Role scope sets (decision A)

- [x] 1.1 Add nested role scope sets (`project_user` = viewer + `data:write`; `project_admin` = user + `agents:write` + `schema:write`) in `scope_mapping.go`, and verify a unit test asserts each role's exact set with set equality
- [x] 1.2 Map all three canonical roles in `roleToScopes`, returning copies, and verify `TestRoleToScopes` covers each mapped role plus unmapped strings (`owner`, `admin`, typos, case variants)
- [x] 1.3 Assert the `project_admin ⊇ project_user ⊇ project_viewer` invariant, raw and after umbrella expansion, and verify `TestRoleScopeInvariantAdminSupersetUserSupersetViewer` passes

## 2. Bounded umbrella expansion

- [x] 2.1 Pin the exact expanded scope set for each canonical role with a set-equality test, and verify `TestRoleScopeUmbrellaExpansionIsPinned` additionally rejects any expanded scope in the `admin*`/`mcp:admin`/`org:*`/`project:invite:create`/`account:*` exclusion families
- [x] 2.2 Confirm by inspection of `scopeImplies` that no role expansion reaches an excluded scope (blocker check) and record the result in `design.md`

## 3. Unmapped-role policy (decision B)

- [x] 3.1 Change `resolveOIDCScopes` so the configured default applies only when the membership lookup returns an empty role; an unrecognised non-empty role returns an empty set, and verify unit + `validateToken` end-to-end tests cover both
- [x] 3.2 Verify the lookup-error path still returns an empty set even with a default configured (`TestResolveOIDCScopes` role-lookup-error case)

## 4. Spec

- [x] 4.1 Add the `oidc-role-scope-sets` OpenSpec change with delta specs for role sets, the configurable default, fail-closed resolution, and the pinned umbrella expansion; update the main `oidc-scope-mapping` spec wording to match
- [x] 4.2 `openspec validate --strict` passes for the change

## 5. Verification

- [x] 5.1 `go build ./...` in `apps/server` passes
- [x] 5.2 `go test -count=1 ./pkg/auth/...` passes, and the three DB-backed tests pass against a throwaway Postgres container via `TEST_DATABASE_URL` + `REQUIRE_DB=1`
- [x] 5.3 `golangci-lint run --new-from-rev=origin/main ./pkg/auth/...` reports 0 issues and `gofmt -l` is clean on the changed files

## 6. Out of scope

- [ ] 6.1 Live-Zitadel e2e (issue #736 item 3) — requires real Zitadel credentials in CI; decided separately, so #736 is referenced with `Refs`, not `Closes`
