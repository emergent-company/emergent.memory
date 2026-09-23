# Implementation tasks

This change is **design-only**. The tasks below are the follow-up implementation lanes, sequenced so every non-breaking change lands before any breaking one. Nothing here is executed by this change; `git diff origin/main --stat` for this PR must contain only `openspec/**`.

Sequencing rule: **never remove a grant path in the same release that removes its replacement.** Sections 1–6 are safe to land in Release N. Section 7 flips a default (Release N+1). Section 8 deletes code (Release N+2).

## 0. Preconditions

- [x] 0.1 #803 (`fix/oidc-role-scope-sets`) merged as `589923ff`; this change's `oidc-scope-mapping` delta is authored against the post-#803 main spec with its scenario names carried forward
- [ ] 0.2 Operator answers the eight open questions in `design.md`; re-scope the flag-timing tasks if the answers change the release calendar
- [ ] 0.3 Merge this design PR (open questions answered or explicitly deferred)

## 1. App-owned configuration vocabulary (non-breaking)

- [ ] 1.1 Add `MEMORY_OIDC_DEFAULT_SCOPES` to `ZitadelConfig` as the canonical name for the default scope set, keeping `ZITADEL_OIDC_DEFAULT_SCOPES` as an alias, and verify unit tests cover: canonical-only, alias-only, both-set (canonical wins)
- [ ] 1.2 Add `MEMORY_USERINFO_GRANT_ALL_SCOPES` with `ZITADEL_USERINFO_GRANT_ALL_SCOPES` as an alias, same precedence, and verify the same unit-test matrix
- [ ] 1.3 Emit a startup warning when a deprecated `ZITADEL_*` alias is used, naming both variables, and verify with a config/log unit test
- [ ] 1.4 Update `.env`/docs references to the app-owned names, noting the alias window

## 2. Token-scope trust flag, introduced default-on (non-breaking)

- [ ] 2.1 Add `MEMORY_OIDC_TRUST_TOKEN_SCOPES` (bool, default `true` in Release N) and thread it to the resolution point
- [ ] 2.2 Gate the `filterMemoryScopes` grant branch in `resolveOIDCScopes` on the flag; when off, skip the token branch and fall through to app-derived entitlements → app-owned default → empty, and verify unit tests for both flag states with the same token input
- [ ] 2.3 Emit a startup deprecation warning when the flag is on, stating that token-carried Memory scopes stop being honoured in the next release and naming the variable, and verify it with a config/log unit test
- [ ] 2.4 Verify the flag-off path returns exactly the app-derived set (no union with token scopes) with an exact-set-equality test, not a length check

## 3. Permissive default made loud (non-breaking)

- [ ] 3.1 Emit a startup warning when `UserinfoGrantAllScopes && !introspectionConfigured`, naming the knob and the introspection precondition, and verify with a unit test
- [ ] 3.2 Add a **dedicated** `scope_authority` object to the health response (the `Check` type is `{Status, Message}` and cannot carry booleans) exposing `token_scopes_trusted`, `permissive_all_grant`, and `introspection_configured`, and verify with a health handler unit test
- [ ] 3.3 Decide health auth posture per open question 5 and apply it; verify the field is absent-or-false in a non-permissive config

## 4. Organization entitlement tier (non-breaking)

- [ ] 4.1 Add the entitlement tiers to the OIDC resolution point: superadmin → full catalogue (terminal); `org_admin` → the organization-administration set (`org:read`, `org:invite:create`, `org:project:create`, `org:project:delete`); project membership → the #803 role set. Verify each tier's exact set with set-equality tests, the org∪project combination, and no-entitlement → default → empty
- [ ] 4.2 Thread the request organization into the resolution seam (owning org of `X-Project-ID`, or the standalone org), add the org-scoped `kb.organization_memberships` query, and verify an org-isolation test (org_admin in A is not org_admin for a project in B)
- [ ] 4.3 Add a single app-side **decision** check behind one seam — `superadmin OR org_admin`, **no project tier**, preserving existing any-organization semantics — and route `apitoken.CanGrantAdminAll` through it instead of its bespoke `EXISTS` query; verify the existing `admin:all` minting tests still pass plus an `org_admin` allowed case, a bare `project_admin` refused case, and a neither-entitlement denied case
- [ ] 4.4 Verify `org_admin` does **not** widen the session's project scope set — tier 2 carries no `project:*`/data/schema/agent scope, so the org∪project union adds nothing a pure member would lack — with an exact-set test
- [ ] 4.5 **Deferred follow-up (separate change):** route the remaining org-scoped decision points (org settings, org member invitation, project create/delete within an org) through the shared entitlement check. Recorded so it is not lost, but out of this implementation's scope (design open question 8)

## 5. Vocabulary reconciliation: `org_admin` is authoritative (non-breaking, small widening)

- [ ] 5.1 Change `domain/standalone/bootstrap.go` to write `role = 'org_admin'` for the bootstrapped organization membership, and verify with a bootstrap unit test
- [ ] 5.2 Add a Goose migration normalising `kb.organization_memberships` rows with `role = 'owner'` to `org_admin`, idempotent, with a documented no-op down path, and verify up/down locally
- [ ] 5.3 **Review the widening explicitly:** standalone requests already hold `GetAllScopes()` via `checkStandaloneAPIKey`, so the only principal that genuinely widens under D5 is a **non-standalone** `owner` row (newly eligible to mint `admin:all`). Confirm with the operator (open question 4) that no non-standalone `owner` rows exist; record the row count in the PR
- [ ] 5.4 Correct the stale `owner`-is-the-org-role claims in `openspec/specs/project-viewer-role/spec.md` and the migration 00165 comment, and verify `openspec validate --specs --strict` passes

## 6. Spec sync (non-breaking)

- [ ] 6.1 Archive this change's deltas into the main specs (`scope-authority` created; `oidc-scope-mapping` and `project-viewer-role` modified) as part of the first implementation PR, not separately
- [ ] 6.2 `openspec validate --all --strict` passes

## 7. [BREAKING] Flip the token-scope trust default off (Release N+1)

- [ ] 7.1 Change `MEMORY_OIDC_TRUST_TOKEN_SCOPES` default to `false`; operators who need it set it explicitly and get a per-boot warning
- [ ] 7.2 Remove the `ZITADEL_OIDC_DEFAULT_SCOPES` and `ZITADEL_USERINFO_GRANT_ALL_SCOPES` aliases
- [ ] 7.3 Update release notes with the exact break ("anyone who configured Memory scope names in Zitadel loses those grants on upgrade; set `MEMORY_OIDC_TRUST_TOKEN_SCOPES=true` to retain them temporarily") and verify unit tests assert the new default
- [ ] 7.4 Verify the flip only narrows: with the flag off, no caller receives a token-derived scope (exact-set regression test per tier). D4's entitlement widenings are Release-N and already reviewed; this task asserts the flip itself introduces no new grant

## 8. [BREAKING] Remove the duplicate authority (Release N+2)

- [ ] 8.1 Delete the token-trust flag and the `filterMemoryScopes` grant branch; keep `memoryScopeVocabulary` only as the API-token vocabulary check
- [ ] 8.2 Delete `UserinfoGrantAllScopes`, the `authSourceUserinfo` all-grant branch in `finalizeOIDCUser`, and the `permissive_all_grant` health field
- [ ] 8.3 Verify the userinfo fallback now uses the standard fail-closed resolution with an exact-set test; verify `openspec validate --all --strict` passes
- [ ] 8.4 Re-scope #736 item 3 against the reduced live-Zitadel surface and update the issue

## 9. Verification (every implementation PR)

- [ ] 9.1 `cd apps/server && PATH="/root/go/bin:$PATH" go build ./...`
- [ ] 9.2 `PATH="/root/go/bin:$PATH" go test -count=1 ./pkg/auth/... ./domain/apitoken/... ./domain/health/... ./domain/standalone/... ./internal/config/...`
- [ ] 9.3 `PATH="/root/go/bin:$PATH" golangci-lint run ./...`
- [ ] 9.4 Integration/migration test for 5.2 against a throwaway Postgres (`TEST_DATABASE_URL` + `REQUIRE_DB=1`)
- [ ] 9.5 `openspec validate unify-scope-authority --strict` passes for this design; re-run for the archiving PR
