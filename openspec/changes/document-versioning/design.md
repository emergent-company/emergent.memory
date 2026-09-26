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

### D5: Revision extraction is staged; apply reconciles by `(type, key)`, not canonical id

**Decision**: Keep the existing extraction staging branch as the isolation mechanism, but disable auto-merge when the extraction belongs to a revision, and make branch creation mandatory in that mode. `POST .../apply` SHALL NOT reuse `MergeBranch`'s classification. It reconciles staged objects explicitly against the main graph by `(type, key)`: absent → create; present with different content → new version (update); identical → no-op. Removal is a separate provenance pass (D6). Apply is atomic and idempotent, then drops the staging branch.

**Rationale**: `FindHeadByTypeAndKeyNS` filters strictly by `branch_id` (`graph/repository.go:1030`), and a staging branch is created empty, so `CreateOrUpdate` on the branch assigns **new canonical ids** to entities that already exist on main. `MergeBranch` classifies source objects by `canonical_id` and clones `added` objects under a fresh canonical id (`graph/service.go:3551`, `3955`). Reusing it would therefore treat an update as an addition and create a duplicate main-graph row. `(type, key)` is the graph's real entity identity for extracted objects, so reconciliation must use it. The staging branch still gives isolation, a discard target, and review-before-apply.

**Alternative considered**: Make the staging branch a child of main and teach `FindHeadByTypeAndKeyNS` to fall back to the main branch. That would let `CreateOrUpdate` create a branch-scoped version of the main canonical and let `MergeBranch` classify correctly, but it changes the semantics of every branch-scoped upsert (a much wider blast radius) for a single feature. Rejected in favour of an explicit reconciliation path.

### D6: Removal is driven by provenance to the *applied revision's* chunks, not chunk-id survival

**Decision**: An object is tombstoned on apply when it has provenance from an earlier revision of the same group, has **no** provenance from the applied revision's chunks, and appears in no staged object. Survivorship is provenance to the applied revision — never "the same chunk id appears in both revisions". Apply re-points provenance for reconciled objects to their final main object ids.

**Rationale**: Each revision is chunked independently and `RecreateChunks` assigns every chunk a fresh UUID (`chunking/service.go:134`), so a chunk id from the source revision can never survive into the applied revision. A rule phrased in terms of surviving chunk ids would therefore classify *unchanged* text as removed. Full re-extraction of the applied revision gives every still-present entity fresh provenance to the applied revision's chunks, so "has provenance to the applied revision" is a correct and stable survivorship test. Objects shared with other documents keep provenance from those documents and survive.

### D7: Phase 1 re-extracts the revision; incremental extraction is deferred

**Decision**: A revision runs the standard extraction over its own content (staged). Additions/updates/removals are derived by reconciling the staged result against main by `(type, key)` plus the provenance pass.

**Rationale**: Full re-extraction is correct and simple; it already upserts by `(type, key)` so unchanged entities collapse rather than duplicate. Incremental chunk-level extraction is a meaningful optimisation but a separate, riskier change.

### D8: Provenance is batch-scoped in phase 1

**Decision**: Write `kb.object_chunks` at batch granularity — every chunk that fed a batch is linked to every object extracted from that batch — and document this as batch-scoped, not exact span attribution.

**Rationale**: The pipeline passes each batch to the model as a plain string and `ExtractedEntity` has no source chunk/span field (`extraction/object_extraction_worker.go:410`, `domain/extraction/agents/schemas.go:20`), so exact per-entity attribution is not derivable. Batch-scoped provenance is sufficient for the phase-1 requirement — attributing an object to a revision's chunks for survivorship — and avoids overstating precision. Exact spans require the extraction output schema to return source references and are a follow-up.

### D9: Only unapplied revisions are discardable; provenance is immutable history

**Decision**: A revision may be discarded only when it is neither current nor applied. Applied revisions are permanent history. `DELETE /api/documents/:id` deletes the entire revision group.

**Rationale**: `kb.object_chunks.chunk_id` has `ON DELETE CASCADE` to `kb.chunks` (`migrations/00001_baseline.sql:3560`), so deleting a revision's chunks erases its provenance rows. If an *applied* revision could be discarded, provenance needed to detect later removals would vanish. Restricting discard to unapplied revisions keeps applied provenance intact. Making generic delete operate on the whole group avoids leaving a group with zero current revisions (the partial unique index enforces at-most-one, not at-least-one). Discarding a middle revision rewires its successor's `supersedes_document_id` to keep the chain valid.

### D10: `document_group_id` is NOT NULL with a unique version index

**Decision**: Add the column nullable, backfill it, then set `NOT NULL`; add a unique index on `(document_group_id, version_number)` and initialize the new fields in every creation path.

**Rationale**: A nullable `document_group_id` lets new rows escape the invariant (Postgres partial unique indexes treat NULLs as distinct), and a non-unique index leaves the promised version uniqueness unenforced. Existing creation paths (`Create`, `CreateFromUpload`) must set `document_group_id = id`, `version_number = 1`, `is_current = true` so standalone documents form valid single-revision groups.

### D11: `is_current` is the **applied** revision; new revisions are pending

**Decision**: `is_current` identifies the authoritative revision the main graph reflects. The first revision of a group is created current and applied. A newly uploaded revision is created **pending** (`is_current = false`, `applied_at = NULL`); it becomes current only when applied, at which point the prior current revision is demoted, in the same transaction. Revision states are therefore: current, pending, superseded.

**Rationale**: The earlier model (promote on upload) made the document's label drift ahead of the graph — the UI would say "v2" while the graph still reflected v1 — and left no undo, because discarding the current revision was forbidden to protect the one-current invariant. Making the *applied* revision current removes both problems: the documents list always shows the authoritative version, the graph never lags the label, and a pending revision is freely discardable (it was never current) with no promotion to unwind. The one-current invariant is preserved by moving the flag only in the atomic apply transition.

**Alternative considered**: Promote on upload, and allow discarding an unapplied current revision by promoting its predecessor. Workable, but it makes `is_current` mean "newest text" in one place and "authoritative" in another, and keeps the graph/label mismatch. Rejected.

**Consequence**: extraction for a revision is staged and the revision stays pending until apply; the revision list must expose `isCurrent` + `appliedAt` so clients can render current/pending/superseded. Diff defaults `to` to the newest revision (pending if present) and `from` to its predecessor.

## Risks / Trade-offs

- **[Risk] No provenance for pre-existing objects.** Objects created before this change have no `kb.object_chunks` rows, so they can never be classified as removed. *Mitigation*: document that removals only apply to objects extracted after this change ships; such objects remain live unless manually removed.
- **[Risk] Revision extraction must not leak to main.** The existing worker treats staging-branch creation as best-effort and falls back to writing on main (`extraction/object_extraction_worker.go:337`). *Mitigation*: revision mode makes branch creation and recording mandatory and aborts extraction on failure (spec: "Revision extraction is staged and aborts if staging fails").
- **[Risk] New canonical ids on the staging branch.** Branch-scoped upserts create fresh canonical ids, so a canonical-id merge would duplicate updated entities. *Mitigation*: apply reconciles by `(type, key)` and never relies on `MergeBranch` classification (D5).
- **[Risk] Chunk-id instability.** Re-chunking assigns new chunk ids per revision, so survivorship cannot use chunk-id equality. *Mitigation*: survivorship is provenance to the applied revision's chunks (D6).
- **[Risk] Batch-scoped provenance overstates source chunks.** Every object from a batch is linked to every chunk in the batch. *Mitigation*: explicitly labelled as batch-scoped; exact spans deferred to a follow-up requiring extraction schema changes (D8).
- **[Risk] Applied-provenance loss via discard.** `object_chunks.chunk_id` cascades on chunk deletion. *Mitigation*: only unapplied revisions are discardable, so applied provenance is never deleted (D9).
- **[Risk] Staging branch accumulation.** Each revision creates a branch; abandoned revisions can leak branches. *Mitigation*: discard deletes the branch; a reaper for orphaned revision branches is filed as a follow-up issue.
- **[Risk] Apply duration.** Reconciling a large revision into main can be slow; a synchronous endpoint may time out. *Mitigation*: start synchronous with the delta surfaced by the diff endpoint; if needed, move apply to a job in a follow-up (tracked as an open question).
- **[Risk] Migration cost on large `kb.documents`.** Backfilling `document_group_id` and building two indexes touches every row, and `SET NOT NULL` requires a table scan. *Mitigation*: the migration is additive and idempotent (`ADD COLUMN IF NOT EXISTS`, `UPDATE ... WHERE document_group_id IS NULL`); index builds are assessed against table size in the deploy window.
- **[Risk] Concurrent revision uploads.** Two uploads racing to become current. *Mitigation*: transaction + partial unique index; the loser retries `max(version_number)+1`.
- **[Risk] Large-content diffs.** Diffing two very large parsed documents in memory. *Mitigation*: line diff is O(n) memory; the endpoint bounds response size and reports truncation if the diff exceeds a cap (open question).
- **[Risk] `parent_document_id` confusion.** `parent_document_id` already exists and is *not* the revision link. *Mitigation*: the spec explicitly forbids using it for revisions; revision links live only in `supersedes_document_id` / `document_group_id`.
- **[Risk] Generic delete semantics change.** `DELETE /api/documents/:id` now removes a whole group. *Mitigation*: explicitly specified and documented; single-revision removal uses the discard endpoint.

## Migration Plan

1. **Schema (additive, reversible)**: add `document_group_id` (nullable), `version_number` (`NOT NULL DEFAULT 1`), `supersedes_document_id`, `is_current` (`NOT NULL DEFAULT true`), and `applied_at` (timestamptz, nullable) to `kb.documents`; backfill `document_group_id = id` and `applied_at = created_at` (or `now()`) for existing rows; then `SET NOT NULL` on `document_group_id`; add the unique index on `(document_group_id, version_number)` and the partial unique index on `(document_group_id) WHERE is_current`.
2. **Backend**: initialize the new fields in `Create` and `CreateFromUpload`; ship revision endpoints, mandatory staging mode, batch provenance, and the `(type, key)` reconciliation apply. All additive; existing document endpoints keep their current behaviour for single-revision groups.
3. **Clients**: SDK methods, CLI subcommands, and the web UI Revisions section.
4. **Rollback**: dropping the new columns and indexes fully reverts the schema; revision endpoints are additive and can be left dormant. No existing single-revision document semantics change, so a rollback does not corrupt existing data. Applied-revision graph changes are ordinary graph versions and are reversible through the existing graph version history.

## Open Questions

- Should `apply` be synchronous or run as a background job with SSE progress, given reconciliation duration on large revisions?
- What is the maximum diff response size, and should oversized diffs return a truncated diff plus a "download full diff" affordance?
- When reconciling relationships on apply, should an endpoint object created on main be matched by its post-apply `(type, key)` only, or also by the staged→main id map when keys collide?
- Is `version_number` ever expected to be user-supplied (e.g. importing an external document's own version label), or always server-assigned?
