## Why

Two defects in `apps/server/domain/graph/repository.go`, both silent-wrong-behaviour bugs
found by independent review (issues #798 and #799).

1. **#798 — `applyBulkFilterToUpdate` ignored its `limit`.** The function declared a
   `limit int` parameter, never referenced it, and its doc comment falsely claimed
   *"Uses a subquery for limit support."* Consequently every **update**-based bulk action
   (`update_status`, `soft_delete`, `merge`/`replace_properties`, `set`/`add`/`remove_labels`)
   executed an uncapped `UPDATE … WHERE <filters>`, mutating **all** matched rows. A caller
   passing `BulkActionRequest.Limit = N` to affect at most N rows mutated every match. Only
   hard-delete honoured a cap (its own `id IN (SELECT … LIMIT n)` subselect, made
   deterministic by #797). This is data-correctness, not presentation.

2. **#799 — `GetBranchRelationshipEmbedding` selected a non-existent column.** It read
   `kb.graph_relationships.embedding_v2`; the real columns are `embedding` and
   `embedding_updated_at` (migrations 00011 / 00013). Every call errored, so
   `applyMerge`'s relationship similarity-merge branch was dormant. #797 fixed the same
   dead-column mistake in the sibling `FindSimilarRelationshipInBranch` but deliberately
   did not activate this one.

## What Changes

- **Enforce the bulk-update cap deterministically.** `applyBulkFilterToUpdate` becomes a
  method that appends
  `id IN (SELECT id FROM kb.graph_objects WHERE <base + filter predicates> ORDER BY created_at ASC, id ASC LIMIT n)`.
  The subselect mirrors the base predicates and every filter predicate so the `LIMIT` is
  consumed only by rows the update would accept; the `id` (PK) secondary key sits **inside**
  the limited set, matching the #797 hard-delete convention (oldest-first, total order).
- **`limit = 0` / unset semantics are preserved, not changed.** `BulkActionByFilter` already
  maps `params.Limit <= 0` to `1000` before the switch (the same default the service applies
  via `bulkActionDefaultLimit`), and the helper now honours that value. "Unset" has always
  meant *the default cap of 1000*, never "unlimited"; what changes is that update-based
  actions finally respect it.
- **Correct the doc comment** so it describes the subselect cap instead of claiming one.
- **Fix the relationship embedding column.** `GetBranchRelationshipEmbedding` reads
  `kb.graph_relationships.embedding`; the stale "dormant by design" comment is replaced.
- **Activate the relationship similarity-merge path deliberately**, covered by a new
  service-level merge test.

## Capabilities

### New Capabilities

- `graph-bulk-actions`: how filter-then-action bulk operations cap the set of rows they
  mutate, and what `limit` unset/zero means.
- `graph-relationship-similarity-merge`: how a branch merge probes relationship embeddings
  and absorbs a staged relationship into an equivalent existing target relationship.

### Modified Capabilities

<!-- None. No existing spec covers bulk-action limits or relationship similarity merging. -->

## Impact

- **Server** `apps/server/domain/graph/repository.go`: `applyBulkFilterToUpdate` (signature +
  body) and its 7 call sites; `GetBranchRelationshipEmbedding` column reference and comment.
- **Tests** `apps/server/domain/graph/ordering_tiebreak_integration_test.go`
  (`TestBulkActionUpdate_DeterministicLimit`, `TestGetBranchRelationshipEmbedding_ReadsEmbeddingColumn`)
  and `apps/server/tests/integration/merge_policy_test.go`
  (`TestMergePolicy_RelationshipSimilarity_AbsorbsDuplicate`).
- **Behaviour change (intended):** update-based bulk actions are now capped. A caller that
  previously relied on an unset/small `Limit` silently mutating every matched row will now
  mutate at most `Limit` (default 1000) rows, oldest-first and deterministically.
- **Behaviour change (intended):** relationship similarity merging is live. A staged
  relationship that matches an existing target relationship (same remapped endpoints, cosine
  distance within the similarity threshold) is absorbed as a new version with merged
  properties instead of always being inserted. Endpoint remapping still only applies to
  source objects that were themselves matched by the object similarity probe.
- **No semantics change** to relevance/ordering beyond the deterministic cap and tie-break.
