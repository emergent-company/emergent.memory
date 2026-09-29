# fix-graph-head-resolution-determinism

## Why

Issue #1245. `Repository.GetByID` resolves a lookup by `(id = ? OR canonical_id = ?)`
and then selected "the first HEAD" ordered by `supersedes_id ASC NULLS FIRST, id ASC`.
After an object is forked to a branch, two HEAD rows share one `canonical_id` (one on
`main`, one on the branch), so the winner was decided by the UUID `id ASC` tie-break.

A `Patch` that carries no explicit `BranchID` therefore landed on whichever HEAD
sorted first — main or the fork copy — non-deterministically. If the branch copy was
later merged, the divergence propagated. The same class of ambiguity existed in
`GetByIDIncludeDeleted`, `GetRelationshipByID`, and (with no ordering at all, so pure
heap order) `GetRelationshipHeadByCanonicalID`.

Found while repairing `TestMergeConflictCarriesActor` (#1243): the test's BranchID-less
`Patch` of main intermittently hit the branch HEAD, making an unrelated provenance
assertion fail only sometimes. That test worked around the ambiguity by ordering its
steps; this change removes the underlying non-determinism.

## What Changes

- **Deterministic resolution contract.** A lookup with no branch context resolves to
  the **main** HEAD (`branch_id IS NULL`). The contract, in priority order:
  1. an explicit physical id that is a HEAD — an explicit id always wins;
  2. the main-branch HEAD;
  3. any HEAD (branch-only object / several branch copies), smallest id first;
  4. the explicit physical id on a non-HEAD historical version;
  5. the stable caller-ordered first row.
- **Sibling resolvers aligned.** `GetByIDIncludeDeleted` and `GetRelationshipByID`
  share the object/relationship priority helper; `GetRelationshipHeadByCanonicalID`
  gains `ORDER BY (branch_id IS NULL) DESC, id ASC` so it prefers main instead of
  relying on heap order.
- **Explicit branch targeting preserved.** `Patch` still re-fetches with
  `GetHeadByCanonicalID(branchID)` when a `BranchID` is supplied; passing a fork
  copy's physical id still targets that fork copy (exact-id priority). No API shape,
  schema, or migration change.

## Capabilities

### New Capabilities

- `graph-head-resolution`: deterministic, main-preferring resolution of graph object
  and relationship HEADs when a canonical id is shared across `main` and one or more
  branch copies.

## Impact

- `apps/server/domain/graph/repository.go` — `resolveObjectByID` /
  `resolveRelationshipByID` helpers; `GetByID`, `GetByIDIncludeDeleted`,
  `GetRelationshipByID`, `GetRelationshipHeadByCanonicalID`.
- `apps/server/domain/graph/head_resolution_test.go` — repository-level fail-first tests.
- `apps/server/domain/graph/head_resolution_service_db_test.go` — service-level
  fail-first tests (`Patch` main/branch/fork-id targeting).
- No migration; no `.WithMessage`/`.WithInternal` additions; caller contract documented
  in the spec.
