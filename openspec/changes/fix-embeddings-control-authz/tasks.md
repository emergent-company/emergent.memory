## 1. Route gating

- [x] 1.1 Split the `/api/embeddings` group into read (`RequireProjectTokenScope` + `RequireProjectMember`), operator-write (`RequireSuperadminFull`), and diagnostic (`RequireSuperadminFull`) posture groups in `embedding_control_routes.go`.
- [x] 1.2 Verify no endpoint in the group is left with only `RequireAuth()`.
- [x] 1.3 Add `pkg/auth` `RequireSuperadminFull` middleware + `IsSuperadminFull`/`superadminRole` helpers; delegate `dbSuperadminRole` to the shared query.

## 2. Project-scoped progress

- [x] 2.1 Add `GraphRelationshipEmbeddingJobsService.StatsByProject` (join `kb.graph_relationships` on `project_id`), mirroring the object service's `StatsByProject`.
- [x] 2.2 Rewrite `EmbeddingControlHandler.Progress` to scope counts to the caller's project, falling back to the deployment-wide view only for `superadmin_full` callers with no project context.
- [x] 2.3 Keep the response shape (`objects`/`relationships` with the six queue states) unchanged.

## 3. Tests (TDD)

- [x] 3.1 Add `apps/server/domain/extraction/embedding_control_authz_test.go`: an org_admin-minted `admin:all` token gets 403 on all operator writes, `diagnose`, and project-less `progress`; a non-admin (no-scope) token gets 403; a `superadmin_full` principal gets 200; a project member's `progress` returns only their own project's counts; a non-member's `progress`/`status` for a foreign project is 403.
- [x] 3.2 Verify the fail-first run (against the scope-gated version) shows the org_admin `admin:all` token reaching `pause`/etc with 200.

## 4. Spec

- [x] 4.1 Add the `embedding-status` delta spec documenting the control-group authorization posture (admitted set: `superadmin_full` only).

## 5. Verify

- [x] 5.1 `go build ./...` clean.
- [x] 5.2 `bash scripts/lint-ratchet.sh` — auth guards <= 13, apperror Style A <= 1201.
- [x] 5.3 `golangci-lint run --new-from-rev=origin/main ./domain/extraction/... ./pkg/auth/...` — 0 new issues.
- [x] 5.4 `openspec validate --all --strict` green.

