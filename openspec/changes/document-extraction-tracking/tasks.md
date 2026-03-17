## 1. Backend — Document Status Lifecycle

- [x] 1.1 Add `ProcessingStatus` computed field to `Document` struct in `apps/server/domain/documents/entity.go` (scanonly, string)
- [x] 1.2 Update `apps/server/domain/documents/repository.go` list query to JOIN on `kb.object_extraction_jobs` and `kb.document_parsing_jobs` to derive `processing_status` using the precedence rules from spec
- [x] 1.3 Update `apps/server/domain/documents/repository.go` single-doc GET query to include the same `processing_status` derivation
- [x] 1.4 Add `ProcessingStatus` derivation helper function (or inline SQL CASE expression) covering all 6 statuses: `converting`, `conversion_failed`, `ready_for_extraction`, `extracting`, `extraction_failed`, `completed`

## 2. Backend — Compact Extraction Summary on Document List

- [x] 2.1 Add `LastExtractionAt`, `ObjectsCreated`, `RelationshipsCreated` scanonly fields to `Document` struct in `entity.go`
- [x] 2.2 Update list and single-GET repository queries to LEFT JOIN latest completed `kb.object_extraction_jobs` row per document and populate these three fields

## 3. Backend — Extraction Summary Endpoint

- [x] 3.1 Add `ExtractionSummary` response struct to `apps/server/domain/documents/entity.go` with fields: `jobId`, `completedAt`, `objectsCreated`, `relationshipsCreated`, `objectsByType`, `chunksProcessed`, `totalChunks`, `hasErrors`, `errorSummary`
- [x] 3.2 Add `GetExtractionSummary(ctx, documentID, projectID)` method to `apps/server/domain/documents/repository.go` that queries the most recently completed `ObjectExtractionJob` for the document and its associated `ObjectExtractionLog` entries to build the summary
- [x] 3.3 Add `GetExtractionSummary` method to `apps/server/domain/documents/service.go` delegating to repo, returning `apperror.ErrNotFound` when no completed job exists
- [x] 3.4 Add `GetExtractionSummary` handler method to `apps/server/domain/documents/handler.go` (`GET /:id/extraction-summary`)
- [x] 3.5 Register the new route in `apps/server/domain/documents/routes.go` under the `readGroup` with `documents:read` scope

## 4. Frontend — Status Badge System

- [x] 4.1 Add `processingStatus` field to the `DocumentRow` type in `/root/emergent.memory.ui/src/pages/admin/apps/documents/index.tsx`
- [x] 4.2 Add `lastExtractionAt`, `objectsCreated`, `relationshipsCreated` fields to `DocumentRow`
- [x] 4.3 Create a `getProcessingStatusBadge(status: ProcessingStatus)` helper in the documents page that maps each of the 6 statuses to a DaisyUI badge class + label
- [x] 4.4 Replace the existing separate conversion-status and extraction-status badge display in the document list row with a single `processingStatus` badge
- [x] 4.5 Update `DocumentDetailModal` Properties tab Processing Status section to use the unified `processingStatus` badge (replacing the existing `getConversionStatusBadge` display)

## 5. Frontend — Extraction Trigger from Document Row

- [x] 5.1 Update the document row actions menu in `apps/documents/index.tsx` to show "Extract Objects" when `processingStatus` is `ready_for_extraction`, `extraction_failed`, or `completed` (already calls `ExtractionConfigModal` — ensure it passes `documentId` correctly)
- [x] 5.2 Disable / hide "Extract Objects" action when `processingStatus` is `converting` or `extracting`, adding a tooltip explaining why
- [x] 5.3 Ensure SSE handler (`useDataUpdates('extraction_job:*', ...)`) updates individual document row status without full page reload (targeted row update rather than full list refetch)

## 6. Frontend — Extraction Progress on Document Row and Detail Modal

- [ ] 6.1 Update the documents list SSE handler to also fetch/merge the active extraction job's progress fields (`chunksProcessed`, `totalChunks`, `objectsCreated`) when `processingStatus` is `extracting`
- [ ] 6.2 Render a compact progress indicator on the document list row when `processingStatus` is `extracting` (e.g., "12 / 40 chunks" next to the Extracting… badge)
- [ ] 6.3 Update `DocumentDetailModal` Properties tab to show a progress bar with chunk counts and live object count when `processingStatus` is `extracting`

## 7. Frontend — Extraction Summary in Detail Modal and List Row

- [x] 7.1 Add `GET /api/documents/:id/extraction-summary` call to `/root/emergent.memory.ui/src/api/documents.ts` client as `getExtractionSummary(id)`
- [ ] 7.2 In `DocumentDetailModal` Properties tab, add an "Extraction Results" sub-section that lazy-loads `getExtractionSummary(id)` when `processingStatus` is `completed`; display object counts by type, relationship count, and last extraction timestamp
- [ ] 7.3 Show a "Re-extract" button in the Extraction Results section of the detail modal that re-opens `ExtractionConfigModal`
- [ ] 7.4 Render compact extraction stats (`objectsCreated` · `relationshipsCreated`) on the document list row when `processingStatus` is `completed`, sourced from the compact fields already returned in the list response (no extra API call)
- [ ] 7.5 Show an empty state / "Extract Objects" prompt in the detail modal Extraction Results section when `processingStatus` is `ready_for_extraction`
