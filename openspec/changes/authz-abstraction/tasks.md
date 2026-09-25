# Implementation tasks

This change is **design-only**. The tasks below are the follow-up implementation lanes, sequenced so every non-breaking change lands before any breaking one. Nothing here is executed by this change; `git diff origin/main --stat` for this PR must contain only `openspec/**`.

Sequencing rule: **never remove an existing guard before the module it protects is migrated and its conformance matrix is green.** Steps 1–2 are additive (no behaviour change). Steps 3–4 change enforcement of a live surface and ship only with a passing conformance matrix. Steps 6–7 delete old guards per-module.

## 0. Preconditions

- [ ] 0.1 Operator answers the seven open questions in `design.md` (grant/scope dual representation, org-tier non-inclusivity, share/agent principal source, child-resolver enumeration, `admin` disposition, route-discovery mechanism, `SuperadminOnly` folding)
- [ ] 0.2 Merge this design PR (open questions answered or explicitly deferred)

## 1. `pkg/authz` core (additive, no callers)

- [ ] 1.1 Add `pkg/authz` with `Level`, `Kind`, `Kind.RequiredLevel`, `Resource`, `Principal`, `PrincipalKind`, `Grant`, `OwnerResolver`, `RouteParams`, `Credential` (design B1/B4), and unit-test the tier/kind vocabulary including the `Level >= Kind.RequiredLevel()` ordering and the `Grant.Satisfies` family/scope matching (org id cannot satisfy a project grant, platform is a superset)
- [ ] 1.2 Implement `RequireAuthority(ctx, p, res, level, r)` with child-ownership resolution via `OwnerResolver.ChildProject` (design B3); unit-test: platform superset, org-in-scope, org-out-of-scope (org A vs org B), project-member, project-non-member, child-resolved-through-project, bare-child-id-denied, and 401/403/404 decision typing
- [ ] 1.3 Implement `DerivePrincipal(ctx, cred Credential, res Resource, r OwnerResolver)` (design B4) taking the neutral `Credential` (NOT `*auth.AuthUser`, to avoid the `pkg/authz`→`pkg/auth` import cycle); unit-test that a client-supplied `X-Project-ID` can never mint a grant (token-bound project wins, mismatch → 403; OAuth/account token requires membership in the derived project's org)
- [ ] 1.4 Add `pkg/auth.CredentialFromAuthUser(u *AuthUser) authz.Credential` and the DB-backed `OwnerResolver` in `pkg/auth`; verify `pkg/authz` compiles with no import of `pkg/auth` or Echo
- [ ] 1.5 Implement `Authorize` (design B5) and `PrincipalFromContext`/`ContextWithPrincipal` as the single choke point and the principal propagation contract

## 2. Transport-agnostic enforcement — MCP in-process (mechanism 7)

- [ ] 2.1 Add `authz.AuthorizeTool(ctx, def *ToolDefinition)` that projects **all three** markers — `RequiredScope`, `AgentOnly`, and `SuperadminOnly` — against the principal from `ctx` (design E2), with a single scope→level registry (no parallel table)
- [ ] 2.2 Wire `mcp.Service.ExecuteTool` to call `AuthorizeTool` after the share-allowlist check and before dispatch (design E2); verify with a unit test that an in-process call of a `RequiredScope`-gated or `AgentOnly` tool is now denied without the scope/agent principal, and that `SuperadminOnly` tools still require `superadmin_full` (issue #948 preserved — a bare `admin` token never satisfies them)
- [ ] 2.3 Point the three HTTP transport per-tool checks (`handler.go`, `sse_handler.go`, `streamable_http_handler.go`) at `AuthorizeTool` and delete their duplicated inline checks; verify legacy-rpc / streamable / SSE decisions are unchanged and `SuperadminOnly` protection is not lost in the fold

## 3. Token-mint authority — bare `admin` (mechanism 2/3)

- [ ] 3.1 Add a single scope→required-level mapping for token mint (design E1): `admin` → `LevelPlatform`, `admin:all` → `LevelOrg`-or-platform (preserving #812 semantics), project scopes → `LevelProject`
- [ ] 3.2 Replace `domain/apitoken.Service.create`'s and `CreateAccountToken`'s bespoke `checkAdminAllGrant`-only gate with `checkMintAuthority` over the mapping; verify a non-superadmin/org-admin can no longer mint `admin`, and `admin:all` minting still requires org/superadmin (audit-core F1 closed)
- [ ] 3.3 Decide the disposition of the nine `RequiredScope:"admin"` MCP tools (design E1, open question 5): re-key them to a non-mintable scope or fold them under the mint-level check, and conformance-test the mint surface with the caller-class matrix

## 4. Declarative surface registry — first HTTP domain (mechanisms 1, 3, 4, 5)

- [ ] 4.1 Add the `SurfaceEntry`/`Registry` types (transport-neutral core) and the `pkg/authz/echo` adapter with `Enforce(entryID)` (design B2), including registration-time assertions (Level >= floor, unique ID, non-nil Resolve)
- [ ] 4.2 Migrate one project-scoped domain end-to-end (recommend `documents` or `projects`) to `Enforce(entryID)` + `DerivePrincipal` as the reference pattern; fold its `RequireProjectTokenScope`+`RequireProjectMember` chain into the entry
- [ ] 4.3 Migrate one child-scoped entrypoint to `Kind: KindChild` with a parent resolver, proving ownership resolution (design B3)
- [ ] 4.4 Keep the old guards in place on that domain until its conformance matrix (step 5) is green, then remove them

## 5. Conformance kit + CI guard (mechanism 8)

- [ ] 5.1 Implement `Conformance(entry)` (class list) and `TestConformance(t, entryID, fixtures, authorize)` (design B6) so each row executes against the real `Authorize` path with module-supplied fixtures (real member, foreign member, token, share, agent) — not re-derived from `(Kind, Level)` alone
- [ ] 5.2 Implement `MarkTested`/`TestRegistryComplete` (design B7) using the test-registration ledger (NOT `len(Conformance(e))`), and wire it into CI; verify the build fails on a registered-but-untested entry
- [ ] 5.3 Add a **mandatory** `go/analysis` pass over `RegisterRoutes` call sites to flag handlers not backed by a `SurfaceEntry`, and gate CI on it (open question 6)
- [ ] 5.4 Enforce the no-skip and production-fixture obligations: a lint rule disallowing `t.Skip` in conformance tests, and a requirement that conformance fixtures use the same owner-resolver as production (design B7 "what the guard does and does not cover")

## 6. Rollout — remaining domains (strangler)

- [ ] 6.1 Migrate org-scoped domains (orgs, invites, project CRUD) to `KindOrganization` entries with org-admin level checks
- [ ] 6.2 Migrate platform-scoped domains (mcpregistry, sandbox images, extraction admin) to `KindPlatform` entries with `LevelPlatform`
- [ ] 6.3 Migrate remaining project/child domains in batches, each with its conformance matrix

## 7. Deprecation and cleanup

- [ ] 7.1 Deprecate `auth.GetProjectID`/`user.ProjectID` header reads and add a lint rule steering new code to `DerivePrincipal`
- [ ] 7.2 Remove old `Require*` guards per-module once each module's conformance matrix is green; keep `RequireAuth` (authentication is out of scope for this change)

## 8. Verification (every implementation PR)

- [ ] 8.1 `cd apps/server && PATH="/root/go/bin:$PATH" go build ./...`
- [ ] 8.2 `PATH="/root/go/bin:$PATH" go test -count=1 ./pkg/authz/... ./pkg/auth/... ./domain/mcp/... ./domain/apitoken/...`
- [ ] 8.3 `PATH="/root/go/bin:$PATH" golangci-lint run ./...`
- [ ] 8.4 `openspec validate authz-abstraction --strict` passes for this design; re-run for the archiving PR
