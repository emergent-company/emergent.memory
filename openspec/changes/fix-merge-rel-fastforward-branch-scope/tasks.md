# Tasks — fix-merge-rel-fastforward-branch-scope

## 1. Reproduce (fail-first)

- [x] 1.1 DB fixture: one relationship `canonical_id` on `main`, a source branch and
  a target branch, with differing source content.
- [x] 1.2 Merge source → target branch (non-nil `targetBranchID`), execute.
- [x] 1.3 Show RED: the fast-forward lands on `main` and the target branch is left
  unchanged (`merge_rel_fastforward_db_test.go`).

## 2. Fix

- [x] 2.1 Add a `branchID` parameter to `GetRelationshipHeadByCanonicalID`; non-nil
  scopes to that branch, nil keeps the #1247 main-preferring default.
- [x] 2.2 Pass `targetBranchID` from the merge `fast_forward` path; existing
  branch-less callers pass `nil`.
- [x] 2.3 Guard `CreateRelationshipVersion`: an explicit requested branch that
  disagrees with the prev-head's branch fails loudly.

## 3. Verify

- [x] 3.1 GREEN: branch→branch fast-forward writes to the target branch and leaves
  `main` untouched.
- [x] 3.2 Guard unit test pins the loud-failure contract.
- [x] 3.3 #1247 determinism tests and merge-flow tests still pass.
- [x] 3.4 `go build ./...`.
- [x] 3.5 `task lint`.
- [x] 3.6 `REQUIRE_DB=1 go test ./domain/graph/...` against a throwaway Postgres.
- [x] 3.7 `gofmt -l` clean; `openspec validate --all --strict` (my change valid;
  4 pre-existing unrelated placeholder-Purpose spec warnings remain).
