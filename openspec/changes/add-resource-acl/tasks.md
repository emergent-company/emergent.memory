## 1. Migration — ACL schema + backfill (TDD)

- [ ] 1.1 New migration `apps/server/migrations/<n>_create_acl.sql`: `kb.acl_entries`, `kb.groups`, `kb.group_members` per design D1, with unique constraints and indexes on `(resource_type, resource_id)` and `(principal_type, principal_id)`.
- [ ] 1.2 Backfill migration: for every existing project member, grant `read` on that project's existing resources (documents, graph canonical ids, sources).
- [ ] 1.3 (TDD) Migration test: up/down round-trips; backfill yields read access parity — a pre-existing member can still read every resource they could before; a non-member still cannot.

## 2. ACL domain — entries, groups, resolution (TDD)

- [ ] 2.1 New `domain/acl` package: entities, `store.go` (grant/revoke, list), recursive group resolution (cycle-safe `WITH RECURSIVE` or visited-set).
- [ ] 2.2 (TDD) Store unit test: grant/revoke idempotent (no duplicates); revoke overrides default; duplicate grant no-ops.
- [ ] 2.3 (TDD) Group resolution unit test: direct grant resolves; transitive group membership resolves; a cycle terminates and returns the correct result.
- [ ] 2.4 (TDD) Effective-permission helper unit test: decision table covering member-default-read, explicit-deny, non-member-default-deny, explicit-grant, and admin bypass.

## 3. `authorizeResources()` enforcement helper (TDD)

- [ ] 3.1 Implement `authorizeResources(ctx, principal, projectID, resourceTypes) → allowed id sets`, collapsing to project-id-only in the common case (member, no overrides).
- [ ] 3.2 (TDD) Unit test: member with no overrides → allowed set equivalent to "all project resources"; member with a deny → that id excluded; non-member with a grant → only granted ids included.
- [ ] 3.3 Implement the admin bypass in this single helper.

## 4. Search + graph hybrid enforcement (TDD)

- [ ] 4.1 In `domain/search/repository.go`, apply the authorization predicate to the graph, text/chunk, and relationship legs.
- [ ] 4.2 Apply the same helper to graph hybrid search.
- [ ] 4.3 (TDD) Unit test: an unauthorized resource does not appear in any leg; authorization is applied as a SQL filter (assert via query shape or integration fixture), not a post-hoc drop.

## 5. `PermissionSource` contract (TDD)

- [ ] 5.1 Define `PermissionSource` interface contract (sync external permissions → ACL entries) with a documented spec; no implementation.
- [ ] 5.2 (TDD) Unit test (contract-only): a trivial in-memory `PermissionSource` implementation satisfies the interface and produces ACL entries.

## 6. Verify + deferred scope

- [ ] 6.1 `task build` (server compile).
- [ ] 6.2 `task lint` for the touched modules.
- [ ] 6.3 Deferred (documented, NOT in this change): 50 connector ACL syncers (Phase 2); Postgres RLS alternative (rejected in design D3).
