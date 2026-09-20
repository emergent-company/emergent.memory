## Context

Graph objects are versioned: `kb.graph_objects` rows carry a physical `id` (version id) and a stable `canonical_id` shared by all versions of the same entity. Relationships (`kb.graph_relationships`) store `src_id`/`dst_id` as the **canonical** id of their endpoints, not the physical version id.

Extraction (`object_extraction_worker.go`) writes provenance only into `properties._extraction_job_id`. The column `graph_objects.extraction_job_id` exists but is never populated, so the deletion queries that join through it always return zero.

`CreateOrUpdate` dedups by `(project_id, type, key)` and **replaces** `properties` on an existing entity with the new extraction's properties (it does not merge previous values). Therefore an entity's stored content reflects the most recent extraction that wrote it.

## Goals / Non-Goals

**Goals**

- The reported `graphObjects` / `graphRelationships` counts equal what deletion actually removes, for single and bulk deletion.
- Deleting a document removes every graph object row whose stored content was derived from that document's extraction jobs, plus orphaned entities and their relationships.
- Provenance survives object versioning, staging-branch extraction, auto/manual branch merge, and branch fork.
- Existing data is repaired by a backfill migration.

**Non-Goals**

- Partial redaction of a shared entity whose `properties` were replaced by a later document's extraction. Because `CreateOrUpdate` replaces properties, an earlier writer's facts are no longer present in the entity; its version rows attributable to the earlier document are still deleted for erasure.
- Attributing relationships to a document independently of their endpoints (relationships have no provenance column).
- Changing extraction to be chunk-attributed.

## Decisions

### Single provenance column, not a link table

A new many-to-many provenance table (or reviving the unused `kb.object_chunks`) was considered and rejected: extraction entities are not chunk-attributed (the worker batches whole-document text), and because `CreateOrUpdate` replaces properties, an entity's current content is single-writer. A single-valued `extraction_job_id` per version is therefore semantically aligned with the data model. It is also far smaller and lower-risk.

### Set provenance explicitly on new versions

`CreateOrUpdate`'s restore and update branches MUST set `newVersion.ExtractionJobID = req.ExtractionJobID` when supplied. They must NOT rely on inheritance from the previous head: a re-extraction by a different document must re-tag the new version with the new job.

`CreateVersion` inherits `prevHead.ExtractionJobID` only when the caller did not supply one. This preserves provenance for patch and branch-merge fast-forward/conflict/similar paths.

### Branch-merge "added" clone must carry provenance

`applyMerge`'s `added` case clones a source object into a brand-new canonical id on the target branch. It constructs a `GraphObject` literal directly, so it must copy `src.ExtractionJobID`. This requires `BranchObjectHead` to carry `ExtractionJobID` and `GetBranchObjectHeads` to select the column. Without this, the common "extract on staging branch, auto-merge to main" flow loses provenance and the reported scenario still returns zero.

### Cascade resolves canonical identity

Deletion and impact compute, per document (or set of documents):

1. `jobIDs` — extraction job ids for the document(s).
2. `attributableRows` — graph object rows (`id`, `canonical_id`) with `extraction_job_id IN jobIDs` (scoped by project).
3. `fullyRemovedCanonicals` — the `canonical_id`s of attributable rows for which **no** other version exists with `extraction_job_id IS NULL` or `NOT IN jobIDs`. These entities are wholly derived from the document being deleted.
4. Objects to delete = every row with `canonical_id IN fullyRemovedCanonicals` **plus** attributable rows whose canonical is not fully removed (superseded versions still holding the document's facts — required for erasure).
5. Relationships to delete = rows where `src_id IN fullyRemovedCanonicals OR dst_id IN fullyRemovedCanonicals`.

This fixes the current bug where cascade matches relationships by physical `id` while relationships store canonical ids.

### No new foreign key

A `graph_objects.extraction_job_id -> object_extraction_jobs(id)` FK is intentionally **not** added. With `ON DELETE SET NULL` it would silently sever provenance when the admin `DeleteJob` / `DeleteCompleted` endpoints hard-delete an extraction job, recreating the silent-zero failure. Integrity is enforced by application code and the backfill.

### Reporting honesty

Counts are entity rows removed (including superseded versions and staging-branch rows), because those rows still contain the document's asserted facts and are what deletion removes. This is documented in the spec so operators are not misled.

## Risks / Trade-offs

- Row-based counts exceed "visible distinct entities"; the spec states the count semantics explicitly.
- If a future admin operation hard-deletes extraction jobs, provenance for surviving objects is lost; out of scope, documented as a known limitation.
- Backfill only sets values for rows whose property parses to a uuid and whose job row still exists; all other rows stay NULL and are correctly treated as non-derived.

## Migration Plan

1. Add a Goose migration that backfills `kb.graph_objects.extraction_job_id` from `properties->>'_extraction_job_id'` where the value is a valid uuid and a matching `kb.object_extraction_jobs` row exists.
2. No down-migration data restoration (backfill is additive and safe).
3. Deploy order: migration first, then application.

## Open Questions

None blocking.
