## Context

Documents are ingested through a two-phase pipeline: optional **conversion** (`kb.document_parsing_jobs` → parsed `content`) then **extraction** (`kb.object_extraction_jobs` → graph objects/relationships). Today each upload creates an independent `kb.documents` row; `file_hash` dedup was removed in issue #381, and raw-content create still dedups by `content_hash`. There is no relationship between two uploads of the "same" document.

The graph layer is already version-aware: `kb.graph_objects` carries `canonical_id`, `version`, `supersedes_id`, `change_summary` (an RFC 6901 JSON-pointer diff) and a `content_hash`; `graph.Service.MergeBranch` supports a dry-run + execute merge with policy-based conflict resolution; extraction already creates a per-job staging branch and auto-merges it once embeddings are ready (`extraction/object_extraction_worker.go`).

Two existing but unused assets matter here:
- `kb.documents` already declares `sync_version` and `external_source_id` (dormant; intended for external data-source sync, not manual revisions).
- `kb.object_chunks(object_id, chunk_id, extraction_job_id, confidence)` exists but is **never written** by Go code. It is the missing provenance link between a graph object and the text that produced it.

**Stakeholders**: end users maintaining living documents, and the graph/review workflow that consumes their extracted entities.

## Goals / Non-Goals

**Goals:**
- Give documents a logical identity and a monotonic revision chain, without breaking existing single-upload documents.
- Let users upload a new revision of an existing document.
- Show what changed between two revisions — both as a readable line diff and as a structured entity delta.
- Update the main graph from a revision only after review, adding/updating what the revision introduces and removing objects whose source text is gone.

**Non-Goals:**
- Chunk-level incremental extraction (re-extract only changed chunks). Phase 1 re-extracts the revision; incremental extraction is a follow-up.
- Backfilling `kb.object_chunks` for objects created before this change.
- Replacing or redesigning the existing graph version chain, branch merge, or journal.
- Re-using `sync_version` / `external_source_id` for revisions — those remain reserved for external data-source sync.
- A general-purpose document diff/merge UI beyond a single revision-vs-previous view.

## Decisions

### D1: Revision chain as additive columns on `kb.documents` (chained documents)

**Decision**: Model revisions as sibling `kb.documents` rows chained by new columns — `document_group_id`, `version_number`, `supersedes_document_id`, `is_current` — rather than a separate `kb.document_versions` table.

**Rationale**: Chunks, parsing jobs, extraction jobs, the extract pipeline, deletion cascade, and the web UI all key off `document_id`. Sibling rows reuse every one of those paths unchanged. A separate versions table would force chunks and extraction to reference a version id and would fork the pipeline. The user-selected model was explicitly "chained documents".

**Alternative considered**: `kb.document_versions` table keyed by a stable logical document id. Cleaner separation, but a much larger blast radius (chunks, extraction, deletion impact, backups) for no functional gain here. Deferred.

### D2: `is_current` is a stored flag guarded by a partial unique index

**Decision**: Store `is_current boolean NOT NULL DEFAULT true` and enforce one current row per group with `CREATE UNIQUE INDEX ... ON kb.documents (document_group_id) WHERE is_current`. Version demotion and promotion happen in one transaction that also computes `version_number = max + 1`.

**Rationale**: A partial unique index makes the invariant a database guarantee rather than a convention — concurrent revision uploads cannot both become current; the loser retries against the new head. Deriving "current" via `NOT EXISTS(child)` was rejected because it makes every document list query a correlated subquery and provides no write-time guard.

**Alternative considered**: Reuse `sync_version` as the version number and `external_source_id` as the group id. Rejected: those columns have external-sync semantics; overloading them would make future sync work ambiguous.

### D3: Provenance goes into the existing `kb.object_chunks` table

**Decision**: Carry chunk identity through extraction batch construction and write `kb.object_chunks` rows (object id, chunk id, extraction job id, confidence) for every object created or updated.

**Rationale**: Without knowing which chunks produced an object, "removed from the document" cannot be distinguished from "object still valid". The table already exists with the right shape; extraction batches already concatenate chunk text, so chunk ids only need to ride alongside the text. Alternative — stashing chunk ids inside object properties — was rejected because it bloats properties, is not queryable by join, and is not schema-backed.

### D4: Diff = line diff + entity delta from staged provenance

**Decision**: The revision diff endpoint returns (a) a unified, line-oriented diff of the two revisions' parsed `content` (using `github.com/pmezard/go-difflib`, already an indirect dependency → promoted to direct) and (b) an entity delta classified as added/updated/removed, assembled from the target revision's staged extraction objects plus provenance, matched by `(type, key)`.

**Rationale**: Users need to *see* the text change and *act on* the entity change. Line diff is cheap and explainable; the entity delta is what actually drives the graph. A purely semantic diff (LLM-based) is expensive and non-deterministic, so it is out of scope.

**Alternative considered**: Compute the entity delta by diffing main-graph extraction results of both revisions. Rejected — the target revision is staged on a branch, so the delta must be computed against staged objects, and the removal set must come from provenance, not from a graph scan.

### D5: Revision extraction is staged; apply is an explicit merge + tombstone

**Decision**: Reuse the existing extraction staging branch, but disable auto-merge when the extraction belongs to a revision. `POST .../apply` runs `MergeBranch` dry-run for the change report, then `MergeBranch` execute, then tombstones objects whose provenance is entirely removed.

**Rationale**: The staging-branch + merge machinery already exists and is battle-tested; reusing it gives a review gate and a dry-run delta for free, and keeps the main graph untouched until the user approves (`document-revision-graph-apply` requires this).

**Alternative considered**: Direct-apply extraction to main plus a compensating tombstone pass. Rejected — no review gate, and a failed apply would leave partial writes on the main graph.

### D6: Removal is a tombstone driven by provenance, not a content heuristic

**Decision**: An object is tombstoned on apply only when every chunk in its provenance belongs to the source revision and none of those chunks survive in the applied revision. Objects with any surviving provenance are left live.

**Rationale**: Tombstoning on "not re-extracted this run" would delete objects from unchanged text whenever extraction is non-deterministic. Provenance makes the decision evidence-based. `content_hash` on the object is a secondary signal, not the primary one.

### D7: Phase 1 re-extracts the revision; incremental extraction is deferred

**Decision**: A revision runs the standard extraction over its own content (staged). Additions/updates/removals are derived by diffing the staged result and provenance.

**Rationale**: Full re-extraction is correct and simple; it already upserts by `(type, key)` so unchanged entities collapse rather than duplicate. Incremental chunk-level extraction is a meaningful optimisation but a separate, riskier change.

## Risks / Trade-offs

- **[Risk] No provenance for pre-existing objects.** Objects created before this change have no `kb.object_chunks` rows, so they can never be classified as removed. *Mitigation*: document that removals only apply to objects extracted after this change ships; such objects remain live unless manually removed.
- **[Risk] Staging branch accumulation.** Each revision creates a branch; abandoned revisions can leak branches. *Mitigation*: discard deletes the branch; a reaper for orphaned revision branches is filed as a follow-up issue.
- **[Risk] Apply duration.** Merging a large revision into main can be slow; a synchronous endpoint may time out. *Mitigation*: start synchronous with the dry-run surfaced in the diff endpoint; if needed, move apply to a job in a follow-up (tracked as an open question).
- **[Risk] Migration cost on large `kb.documents`.** Backfilling `document_group_id = id` and adding a partial unique index touches every row. *Mitigation*: the migration is additive and idempotent (`ADD COLUMN IF NOT EXISTS`, `UPDATE ... WHERE document_group_id IS NULL`); index creation is a single concurrent-safe build assessed against table size in the deploy window.
- **[Risk] Concurrent revision uploads.** Two uploads racing to become current. *Mitigation*: transaction + partial unique index; the loser retries `max(version_number)+1`.
- **[Risk] Large-content diffs.** Diffing two very large parsed documents in memory. *Mitigation*: line diff is O(n) memory; the endpoint bounds response size and reports truncation if the diff exceeds a cap (open question).
- **[Risk] `parent_document_id` confusion.** `parent_document_id` already exists and is *not* the revision link. *Mitigation*: the spec explicitly forbids using it for revisions; revision links live only in `supersedes_document_id` / `document_group_id`.

## Migration Plan

1. **Schema (additive, reversible)**: add `document_group_id`, `version_number`, `supersedes_document_id`, `is_current` to `kb.documents`; backfill `document_group_id = id`, `version_number = 1`, `is_current = true`, `supersedes_document_id = NULL` for existing rows; add the partial unique index on `(document_group_id) WHERE is_current` and an index on `(document_group_id, version_number)`.
2. **Backend**: ship revision endpoints, provenance writing, staging mode, and apply. All additive; existing document endpoints keep their current behaviour.
3. **Clients**: SDK methods, CLI subcommands, and the web UI Revisions section.
4. **Rollback**: dropping the new columns and indexes fully reverts the schema; revision endpoints are additive and can be left dormant. No existing document semantics change, so a rollback does not corrupt existing data.

## Open Questions

- Should `apply` be synchronous or run as a background job with SSE progress, given merge duration on large revisions?
- What is the maximum diff response size, and should oversized diffs return a truncated diff plus a "download full diff" affordance?
- Should discarding a revision hard-delete its row or soft-delete it for auditability? (Cleanup of chunks/staging is required either way.)
- Is `version_number` ever expected to be user-supplied (e.g. importing an external document's own version label), or always server-assigned?
