# Tasks — fix-graph-head-resolution-determinism

## 1. Reproduce deterministically (fail-first)

- [x] 1.1 Repository-level fixture: a main HEAD and a fork-copy HEAD sharing one
  `canonical_id`, with explicit UUIDs in both orders under `id ASC`
  (`head_resolution_test.go`).
- [x] 1.2 Show `GetByID` resolves the fork copy when it sorts first, main when it
  sorts first — i.e. flipping UUID order flips the target (RED).
- [x] 1.3 Service-level fixture: BranchID-less `Patch` lands on the fork copy when it
  sorts first (RED) (`head_resolution_service_db_test.go`).
- [x] 1.4 Audit every `GetByID` caller plus the sibling resolvers with the same
  `(id OR canonical_id)` pattern and record which are affected.

## 2. Fix — deterministic main-preferring, branch-aware resolution

- [x] 2.1 Add `resolveObjectByID` with the documented priority (exact id HEAD → main
  HEAD → any HEAD → exact id non-HEAD → first row).
- [x] 2.2 Add `resolveRelationshipByID` mirroring the object priority.
- [x] 2.3 Apply to `GetByID` and `GetByIDIncludeDeleted`.
- [x] 2.4 Apply to `GetRelationshipByID`.
- [x] 2.5 Make `GetRelationshipHeadByCanonicalID` deterministically prefer the main
  HEAD (`ORDER BY (branch_id IS NULL) DESC, id ASC`), since it had no ordering.
- [x] 2.6 Keep explicit branch resolution working: `Patch`'s `GetHeadByCanonicalID`
  re-fetch is unchanged; explicit physical ids win via priority 1.

## 3. Verify

- [x] 3.1 RED captured for both UUID orders; GREEN after the fix.
- [x] 3.2 `BranchID`-less patch targets main; explicit `BranchID` targets branch;
  explicit fork physical id targets branch; `TestMergeConflictCarriesActor`-style
  merge flows unaffected.
- [x] 3.3 `go build ./...`.
- [x] 3.4 `task lint`.
- [x] 3.5 `REQUIRE_DB=1 go test ./domain/graph/...` against a throwaway Postgres.
- [x] 3.6 `gofmt -l` clean; `openspec validate --all --strict`.
