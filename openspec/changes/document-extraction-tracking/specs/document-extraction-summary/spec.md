## ADDED Requirements

### Requirement: API provides per-document extraction summary
The system SHALL expose a `GET /api/documents/:id/extraction-summary` endpoint that returns a summary of extraction results for the most recently completed `ObjectExtractionJob` associated with that document.

The response SHALL include:
- `jobId`: ID of the most recent completed extraction job
- `completedAt`: timestamp when the job completed
- `objectsCreated`: total count of graph objects created
- `relationshipsCreated`: total count of graph relationships created
- `objectsByType`: map of object type label → count (e.g., `{"Person": 4, "Organisation": 2}`)
- `chunksProcessed`: number of chunks processed in that job
- `totalChunks`: total chunks available at time of extraction
- `hasErrors`: boolean — whether the job completed with any logged errors
- `errorSummary`: string or null — brief error description if `hasErrors` is true

If no completed extraction job exists for the document, the endpoint SHALL return HTTP 404 with a structured error.

#### Scenario: Summary returned for document with completed extraction
- **WHEN** `GET /api/documents/:id/extraction-summary` is called and a completed extraction job exists
- **THEN** the response SHALL be HTTP 200 with the summary fields populated
- **AND** `objectsByType` SHALL reflect counts from the most recent completed job only (not cumulative across all jobs)

#### Scenario: 404 for document with no completed extraction
- **WHEN** `GET /api/documents/:id/extraction-summary` is called and no completed extraction job exists for the document
- **THEN** the response SHALL be HTTP 404 with `{"error": "no completed extraction job found for this document"}`

#### Scenario: Summary reflects most recent job only
- **WHEN** a document has been extracted multiple times (multiple completed jobs)
- **THEN** the summary SHALL reflect the most recently completed job, not a cumulative aggregate

### Requirement: Documents list response includes compact extraction summary
The system SHALL include compact extraction summary fields on every document in the list response and single-GET response (not only via the dedicated summary endpoint):
- `lastExtractionAt`: timestamp of the most recent completed extraction job, or null
- `objectsCreated`: total objects created by the most recent completed extraction job, or null
- `relationshipsCreated`: total relationships created by the most recent completed job, or null

These fields SHALL be computed via JOIN/subquery (scanonly, not stored) and returned alongside existing fields.

#### Scenario: List response includes extraction counts for extracted document
- **WHEN** `GET /api/documents` returns a document that has a completed extraction job
- **THEN** each such document object SHALL have `lastExtractionAt`, `objectsCreated`, and `relationshipsCreated` populated

#### Scenario: List response shows nulls for unextracted document
- **WHEN** `GET /api/documents` returns a document with no completed extraction job
- **THEN** the document object SHALL have `lastExtractionAt: null`, `objectsCreated: null`, `relationshipsCreated: null`

### Requirement: UI displays extraction summary in document detail modal
The system SHALL display the extraction summary in the DocumentDetailModal on the Properties tab (or a new "Extraction" sub-section) after a document has been extracted.

The display SHALL include:
- Objects created (total + breakdown by type as a compact list)
- Relationships created (total)
- Last extracted timestamp
- A "Re-extract" button that re-triggers the extraction trigger flow

#### Scenario: Extraction summary shown in detail modal after extraction
- **WHEN** the DocumentDetailModal Properties tab is open and the document has `processingStatus=completed`
- **THEN** an extraction summary section SHALL be visible showing object counts, relationship counts, and last extraction time

#### Scenario: Extraction summary hidden before any extraction
- **WHEN** the DocumentDetailModal Properties tab is open and `processingStatus` is `ready_for_extraction`
- **THEN** the extraction summary section SHALL NOT be shown; instead a prompt to "Extract Objects" SHALL be displayed

### Requirement: Documents list shows compact extraction stats on extracted document rows
The system SHALL display compact extraction stats (object count + relationship count) on document list rows for documents with `processingStatus=completed`.

#### Scenario: Compact stats shown on completed document row
- **WHEN** the documents list renders a document with `processingStatus=completed`
- **THEN** the row SHALL display the `objectsCreated` and `relationshipsCreated` counts in a compact format (e.g., "14 objects · 6 relations")

#### Scenario: No stats shown on unextracted document row
- **WHEN** the documents list renders a document without a completed extraction job
- **THEN** the extraction stats area on that row SHALL be empty or show a dash
