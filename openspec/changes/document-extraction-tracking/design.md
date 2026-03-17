## Context

Documents in the system go through a two-phase processing pipeline:

1. **Conversion** (optional): Binary files (PDF, DOCX, etc.) are parsed into plain text via `DocumentParsingJob`. Plain text and markdown files skip this step (`conversion_status = not_required`).
2. **Extraction** (on-demand): Structured entities (people, organisations, concepts) are pulled from document content via `ObjectExtractionJob` and stored in the knowledge graph.

Currently:
- A document that needs no conversion has `conversion_status = null` or `not_required` with no explicit "ready to extract" signal, making it indistinguishable in the UI from a document that is mid-conversion.
- `ExtractionStatus` is a `scanonly` computed field (JOIN on the latest extraction job status). It maps to the extraction job's internal statuses but is never surfaced as a clear document-level state in the UI.
- There is no extraction summary stored on (or retrievable for) a document — the user must navigate to the superadmin extraction-jobs page to see what was found.
- Triggering extraction from the documents UI calls `POST /api/admin/extraction-jobs` with `source_type=document` but this is buried inside a configuration modal with no progress feedback on the document row.

**Stakeholders**: end-users uploading and managing documents, admins overseeing processing pipelines.

## Goals / Non-Goals

**Goals:**
- Define a clear, explicit document status that reflects the combined conversion + extraction lifecycle, shown on the document list and in the detail modal.
- Expose a per-document extraction summary (object/relationship counts by type, last-run time) via the API and UI.
- Allow users to trigger extraction for a document directly from the documents page without leaving to admin panels.
- Show live extraction progress on the document row/detail while a job is running.

**Non-Goals:**
- Redesigning the extraction job model or its internal worker pipeline.
- Bulk extraction triggering from the documents list (out of scope for this change).
- Changing how conversion is triggered or its job model.
- Automated extraction on upload (trigger remains manual).

## Decisions

### D1: Document status as a computed/derived field, not a new stored column

**Decision**: Derive a `processingStatus` field in the service layer (or as a DB computed expression) from `conversion_status` + the latest `object_extraction_job` status, rather than adding a new persisted column.

**Rationale**: The source-of-truth already exists across two tables. Adding a third stored field creates synchronisation risk (three things to keep in sync). A computed approach is consistent with how `ExtractionStatus` already works (`scanonly` field via LEFT JOIN).

**Alternative considered**: A stored `processing_status` enum column with triggers or service-layer writes on every state change. Rejected due to write amplification and risk of staleness.

### D2: Extraction summary as a new API endpoint, not a stored column

**Decision**: Add `GET /api/documents/:id/extraction-summary` that queries `kb.object_extraction_jobs` and the graph tables to produce a summary, rather than caching it in `kb.documents`.

**Rationale**: Extraction results are mutable (jobs can be re-run, objects deleted). Caching the summary adds a second source of truth. Query performance at document-level granularity is acceptable — it only fires on detail modal open, not on every list page load.

**Alternative considered**: A materialised `extraction_summary` JSONB column updated by the extraction worker on completion. Viable but deferred; can be added later as a performance optimisation if the query proves slow.

### D3: Trigger extraction via existing `POST /api/admin/extraction-jobs` with sensible defaults

**Decision**: The "Extract" button on the document row calls the existing `POST /api/admin/extraction-jobs` endpoint (already exposed to authenticated users via the admin routes group). The frontend opens a lightweight confirmation with schema-selector (already the `ExtractionConfigModal`) rather than building a new endpoint.

**Rationale**: No new backend endpoint needed. The admin extraction jobs endpoint already supports `source_type=document` + `source_id=<documentId>`. The UI just needs to make the trigger more discoverable on the documents page.

### D4: Live progress via existing SSE `extraction_job:*` events

**Decision**: Reuse the existing `useDataUpdates('extraction_job:*', ...)` SSE subscription already present on the documents page to update the displayed status in real time. No new event types needed.

**Rationale**: The documents page already subscribes to `extraction_job:*` and triggers a list refresh. Extending it to also update per-document inline status (from the `ExtractionStatus` scanonly field) requires only a smarter refresh strategy (update matching row, not full reload).

## Risks / Trade-offs

- **[Risk] Scanonly JOIN performance on large projects** → The `ExtractionStatus` LEFT JOIN on `kb.object_extraction_jobs` is already executed per document list. Adding `processingStatus` derivation in-query is safe. Mitigation: ensure the existing index on `(project_id, source_id)` in `kb.object_extraction_jobs` covers the JOIN predicate.

- **[Risk] Extraction summary query latency for documents with many jobs** → A document reprocessed many times accumulates extraction job rows. Mitigation: scope the summary query to the most recent completed job (add `ORDER BY created_at DESC LIMIT 1` to the job subquery).

- **[Risk] Status label confusion if conversion is in progress during extraction** → Unlikely (extraction is manual), but if both jobs are running simultaneously the derived status should prefer the in-flight conversion state. Mitigation: define strict precedence in the status derivation logic (see spec).

## Migration Plan

1. Deploy backend changes (new API endpoint, updated entity fields, summary query) — no schema migration required for the computed approach.
2. Deploy frontend changes — new status badges, extraction summary in detail modal, trigger button on document row.
3. No rollback complexity: the new endpoint is additive, and the status derivation is a read-only computed field.

## Open Questions

- Should extraction summary be shown on the document **list** row (compact) or only in the **detail modal**? Current proposal: compact counts on the list row (objects / relationships), full breakdown in the modal.
- Is there a need to surface conversion job error details on the document row, or only in the detail modal? Current proposal: error badge + tooltip on row, full error in modal.
