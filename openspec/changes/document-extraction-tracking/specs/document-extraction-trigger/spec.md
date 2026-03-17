## ADDED Requirements

### Requirement: User can trigger extraction on a document from the documents page
The system SHALL allow authenticated users to initiate an extraction job for a specific document directly from the documents list page or the document detail modal, without navigating to the admin extraction-jobs panel.

The trigger SHALL open the existing `ExtractionConfigModal` (schema selector + job type) and on confirmation call `POST /api/admin/extraction-jobs` with `source_type=document` and `source_id=<documentId>`. The trigger button SHALL only be visible when the document's `processingStatus` is `ready_for_extraction`, `extraction_failed`, or `completed` (i.e., not while conversion or extraction is already in progress).

#### Scenario: Trigger button visible on eligible document
- **WHEN** a document has `processingStatus` of `ready_for_extraction`, `extraction_failed`, or `completed`
- **THEN** a "Extract Objects" action SHALL be available on the document row (via the actions menu) and in the detail modal

#### Scenario: Trigger button not visible while processing
- **WHEN** a document has `processingStatus` of `converting` or `extracting`
- **THEN** the "Extract Objects" action SHALL NOT be available (it may be shown as disabled with a tooltip)

#### Scenario: Extraction job created on confirmation
- **WHEN** user confirms the extraction config modal
- **THEN** the system SHALL call `POST /api/admin/extraction-jobs` with the document's `id` as `source_id`, `source_type=document`, and the selected schema/job-type options
- **AND** the document's `processingStatus` SHALL transition to `extracting` within the next SSE update

#### Scenario: SSE updates document status during extraction
- **WHEN** the extraction job status changes (queued → processing → completed/failed)
- **THEN** the documents list page SHALL receive an `extraction_job:*` SSE event and update the matching document row's status badge without a full page reload

### Requirement: Extraction progress shown on document row and detail modal
While a document's `processingStatus` is `extracting`, the system SHALL display progress information sourced from the active `ObjectExtractionJob` fields (`chunks_processed`, `total_chunks`, `objects_created`).

#### Scenario: Progress shown on document row during extraction
- **WHEN** `processingStatus` is `extracting` and the extraction job has `total_chunks > 0`
- **THEN** the document row status area SHALL display a progress indicator showing chunks processed (e.g., "12 / 40 chunks")

#### Scenario: Progress shown in detail modal during extraction
- **WHEN** the DocumentDetailModal is open and `processingStatus` is `extracting`
- **THEN** the Properties tab Processing Status section SHALL display a progress bar or counter for chunk processing progress and a running count of objects found so far
