## 1. #798 — enforce the bulk-update limit deterministically

- [x] 1.1 Convert `applyBulkFilterToUpdate` into a `*Repository` method taking `projectID` and append `id IN (SELECT id FROM kb.graph_objects WHERE project_id = ? AND supersedes_id IS NULL AND deleted_at IS NULL [filters] ORDER BY created_at ASC, id ASC LIMIT n)`; the subselect mirrors base + filter predicates and the `id` PK tie-break lives inside the limited set. Verify: `go build ./...`.
- [x] 1.2 Update all 7 call sites (`update_status`, `soft_delete`, `merge_properties`, `replace_properties`, `set_labels`, `add_labels`, `remove_labels`) to pass `params.ProjectID`. Verify: `go build ./...`.
- [x] 1.3 Correct the doc comment: describe the deterministic subselect cap and that callers pass an already-defaulted positive limit (`params.Limit <= 0` → 1000). Verify: comment matches behaviour.
- [x] 1.4 Add `TestBulkActionUpdate_DeterministicLimit` in `apps/server/domain/graph/ordering_tiebreak_integration_test.go`: for every update-based action, 12 same-`created_at` objects inserted descending, `Limit: 5` → `matched == 12`, `affected == 5`, and the mutation lands on exactly the 5 smallest ids. Verify: `go test ./domain/graph/ -run TestBulkActionUpdate_DeterministicLimit -count=10`.

## 2. #799 — fix the relationship embedding column and activate similarity merging

- [x] 2.1 Change `GetBranchRelationshipEmbedding` to select `kb.graph_relationships.embedding` (not `embedding_v2`) and replace the "dormant by design" comment. Verify: helper returns the seeded vector for an embedded relationship and `nil, nil` for an un-embedded one.
- [x] 2.2 Add `TestGetBranchRelationshipEmbedding_ReadsEmbeddingColumn` in `ordering_tiebreak_integration_test.go`. Verify: fails with `column "embedding_v2" does not exist` before 2.1, passes after.
- [x] 2.3 Add `TestMergePolicy_RelationshipSimilarity_AbsorbsDuplicate` in `apps/server/tests/integration/merge_policy_test.go`: a staged relationship whose endpoints are remapped onto existing main objects is absorbed into the pre-existing main relationship (enriched with the source's extra property), not duplicated. Verify: the `notes` enrichment assertion fails before 2.1 and passes after.

## 3. Verification

- [x] 3.1 `go build ./...` in `apps/server`. Verify: no output.
- [x] 3.2 `REQUIRE_DB=1 go test -count=1 ./domain/graph/...`. Verify: pass.
- [x] 3.3 `REQUIRE_DB=1 go test -count=10 ./domain/graph/ -run TestBulkActionUpdate_DeterministicLimit`. Verify: pass.
- [x] 3.4 `REQUIRE_DB=1 go test -count=1 ./tests/integration/ -run TestMergePolicySuite`. Verify: pass.
- [x] 3.5 `golangci-lint run --new-from-rev=origin/main ./domain/graph/...`; `gofmt -l` on changed files; `openspec validate --strict graph-bulk-limit-and-rel-merge`. Verify: clean.
