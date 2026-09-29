# fix-merge-rel-fastforward-branch-scope

## Why

Regression from #1247 (graph HEAD determinism). `applyMerge`'s relationship
`fast_forward` path resolved the previous relationship HEAD with the *branch-less*
`GetRelationshipHeadByCanonicalID`, while `targetBranchID` can be a named branch.
Before #1247 that lookup had no `ORDER BY` (heap order, sometimes right by luck);
after #1247 it is deterministically **main-preferring**. For a branch→branch merge
it therefore returned the **main** HEAD, and `CreateRelationshipVersion` inherited
`prevHead.BranchID`, silently writing the fast-forwarded version onto **main**
while leaving the target branch untouched. The object merge cases never had this
bug because they use the branch-scoped `GetHeadByCanonicalID(..., targetBranchID)`.

## What Changes

- **Branch-scoped relationship HEAD lookup.** `GetRelationshipHeadByCanonicalID`
  gains an optional `branchID`. A non-nil branch resolves exactly that branch's
  HEAD; a nil branch keeps the #1247 main-preferring default (unchanged). The
  merge `fast_forward` path passes `targetBranchID`, mirroring the object path.
- **No silent wrong-branch writes.** `CreateRelationshipVersion` verifies that an
  explicit (non-nil) requested `BranchID` agrees with the resolved prev-head's
  branch and fails loudly otherwise. A nil `BranchID` keeps inheriting from
  prev-head (tombstone/restore/patch callers rely on this). We assert rather than
  blanket-honour the passed branch because several callers legitimately leave it
  nil to inherit; a mismatch means the caller resolved the wrong head, which must
  abort the merge transaction rather than corrupt two branches.
- **Main-target merges unchanged.** `targetBranchID == nil` still resolves and
  writes the main HEAD (branch-scoped lookup with nil → `branch_id IS NULL`).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `graph-head-resolution`: the branch-scoped lookup contract now covers the merge
  relationship fast-forward path; the fast-forward targets the intended branch.

## Impact

- `apps/server/domain/graph/repository.go` — `GetRelationshipHeadByCanonicalID`
  signature + branch scoping; `CreateRelationshipVersion` branch-mismatch guard.
- `apps/server/domain/graph/service.go` — merge `fast_forward` passes
  `targetBranchID`; existing branch-less callers pass `nil`.
- `apps/server/domain/graph/head_resolution_test.go` — guard unit test.
- `apps/server/domain/graph/merge_rel_fastforward_db_test.go` — branch→branch
  fail-first regression test.
- No migration; no `.WithMessage`/`.WithInternal` additions.
