## Why

Deleting a document silently leaves its derived graph objects and relationships behind, and `GET /api/documents/:id/deletion-impact` plus the `DELETE` response both report `graphObjects: 0, graphRelationships: 0`. Verified root cause: `kb.graph_objects.extraction_job_id` is never written by application code (0 of 246 rows set in dev), so the cascade/impact join `object_extraction_jobs.document_id -> graph_objects.extraction_job_id` always matches nothing. The failure is not specific to the discovery-jobs pipeline described in the report — it affects every extraction path.

This makes the one purpose-built surface for previewing and attesting a deletion's blast radius (e.g. a data-subject erasure request) reliably misleading.

## What Changes

- Persist graph-object provenance: add `ExtractionJobID` to the graph object create request and write it on object create/upsert, on new versions, and when cloning objects across branches (merge/fork).
- Preserve provenance across versioning and branch operations so auto/manual merge of extraction staging branches does not lose it.
- Backfill existing `kb.graph_objects.extraction_job_id` rows from the `properties._extraction_job_id` JSONB already written by the extraction worker.
- Rewrite document deletion cascade and deletion-impact (single and bulk) to resolve derived objects and relationships correctly using canonical identity, so counts are truthful and erasure actually removes the document's derived graph data.
- Report entity-level counts that match what deletion removes.

## Capabilities

### New Capabilities
- `document-deletion-provenance`: truthful preview and cascade of a document's derived graph objects and relationships.

### Modified Capabilities
<!-- none -->

## Impact

- `apps/server/domain/graph/` — request DTO, service create/upsert/versioning, branch merge/fork/copy paths, `BranchObjectHead`.
- `apps/server/domain/extraction/object_extraction_worker.go` — stamp the originating job on created objects.
- `apps/server/domain/documents/repository.go` — `DeleteWithCascade`, `GetDeletionImpact`, `BulkDeleteWithCascade`, `GetBulkDeletionImpact`.
- `apps/server/migrations/` — backfill provenance column.
- No API shape change: existing response fields now return correct non-zero values.
