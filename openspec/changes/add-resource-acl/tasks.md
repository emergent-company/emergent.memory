<!-- openspec:archive-hold: spec-only change; implementation intentionally deferred (PR #1410) -->
## 1. Migration — ACL schema + backfill (TDD)

- [ ] 1.1 New migration `apps/server/migrations/<n>_create_acl.sql`: `kb.acl_entries` (resource_type ∈ {document, object}, resource_id, principal_type ∈ {user, group}, principal_id, `permission CHECK (permission IN ('read','deny'))`, timestamps, `UNIQUE (resource_type, resource_id, principal_type, principal_id)`), `kb.groups` (project_id, name), `kb.group_members` (group_id, principal_type, principal_id). Indexes on `(resource_type, resource_id)` and `(principal_type, principal_id)`. Add project-scoped RLS policies to `acl_entries`, `groups`, `group_members` (matching the existing layer).
- [ ] 1.2 Backfill migration: for every `kb.organization_memberships` member, grant `read` on each of the org's projects' existing resources (documents + graph objects), projected per project.
- [ ] 1.3 (TDD) Migration test: up/down round-trips; backfill yields read access parity — a pre-existing org member (including one with **no** `kb.project_memberships` row) can still read every resource they could before; a non-member still cannot; `permission` CHECK rejects values other than `read`/`deny`.

## 2. ACL domain — entries, groups, resolution (TDD)

- [ ] 2.1 New `domain/acl` package: entities, `store.go` (grant/deny/revoke, list), recursive group resolution (cycle-safe `WITH RECURSIVE` or visited-set).
- [ ] 2.2 (TDD) Store unit test: grant/deny/revoke idempotent (no duplicates); `deny` overrides default; duplicate grant no-ops.
- [ ] 2.3 (TDD) Group resolution unit test: direct grant resolves; transitive group membership resolves; a cycle terminates and returns the correct result.
- [ ] 2.4 (TDD) Effective-permission helper unit test: decision table covering member-default-read, explicit-deny, non-member-default-deny, explicit-grant, and admin bypass.

## 3. `authorizeResources()` enforcement helper (TDD)

- [ ] 3.1 Implement `authorizeResources(ctx, principal, projectID, resourceTypes) → allowed id sets`, collapsing to project-id-only in the common case (member, no overrides). Map `object` resource `canonical_id → graph_objects.id` (head-resolved) at enforcement.
- [ ] 3.2 (TDD) Unit test: member with no overrides → allowed set equivalent to "all project resources"; member with a deny → that id excluded; non-member with a grant → only granted ids included.
- [ ] 3.3 Implement the admin bypass in this single helper.

## 4. Search + graph hybrid enforcement (TDD)

- [ ] 4.1 In `domain/search/repository.go`, apply the authorization predicate to the text/chunk leg and the relationship leg. The relationship leg SHALL hide relationships whose `src` OR `dst` object is unreadable (both endpoints must be readable).
- [ ] 4.2 Apply the same helper to the graph-object leg in `domain/graph` hybrid search (NOT `domain/search/repository.go` — the graph leg runs through `domain/graph.Service.HybridSearch`).
- [ ] 4.3 (TDD) Unit test: an unauthorized resource does not appear in any leg; authorization is applied as a SQL filter (assert via query shape or integration fixture), not a post-hoc drop.

## 5. `PermissionSource` contract (TDD)

- [ ] 5.1 Define `PermissionSource` interface contract (sync external permissions → ACL entries) with a documented spec; no implementation.
- [ ] 5.2 (TDD) Unit test (contract-only): a trivial in-memory `PermissionSource` implementation satisfies the interface and produces ACL entries.

## 6. Verify + deferred scope

- [ ] 6.1 `task build` (server compile).
- [ ] 6.2 `task lint` for the touched modules.
- [ ] 6.3 EXPLAIN/bench coverage for the authorization predicate on each leg, confirming the common case (member, no overrides) collapses to the existing project-id predicate with no material regression.
- [ ] 6.4 Deferred (documented, NOT in this change): 50 connector ACL syncers (Phase 2); a `source` resource type (no `kb.sources` table).
