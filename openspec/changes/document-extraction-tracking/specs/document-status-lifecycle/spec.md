## ADDED Requirements

### Requirement: Document processing status reflects the full lifecycle
The system SHALL derive a `processingStatus` field for each document that reflects the combined state of conversion and extraction. The valid values and their precedence (highest to lowest) are:

1. `converting` — a `DocumentParsingJob` for this document is in `pending` or `processing` state.
2. `conversion_failed` — the document's `conversion_status` is `failed`.
3. `ready_for_extraction` — conversion is complete (`completed` or `not_required`) and no extraction job has ever run for this document, OR all past extraction jobs are `cancelled` or `failed` with no running job.
4. `extracting` — an `ObjectExtractionJob` with `source_type=document` and `source_id=<documentId>` is in `pending`, `processing`, or `queued` state.
5. `extraction_failed` — the most recent extraction job is in `failed` state and no job is currently running.
6. `completed` — the most recent extraction job has `status=completed`.

The `processingStatus` SHALL be returned on every document list response and single-document GET response as a top-level field.

#### Scenario: Plain text document uploaded with no prior extraction
- **WHEN** a plain-text document is uploaded (conversion not required) and no extraction job exists
- **THEN** the document's `processingStatus` SHALL be `ready_for_extraction`

#### Scenario: PDF document undergoing conversion
- **WHEN** a PDF document has an active `DocumentParsingJob` in `processing` state
- **THEN** the document's `processingStatus` SHALL be `converting`

#### Scenario: Conversion failed
- **WHEN** a document's `conversion_status` is `failed`
- **THEN** the document's `processingStatus` SHALL be `conversion_failed` regardless of any extraction job state

#### Scenario: Extraction running
- **WHEN** an `ObjectExtractionJob` for this document is in `pending` or `processing` state
- **THEN** the document's `processingStatus` SHALL be `extracting`

#### Scenario: Extraction completed
- **WHEN** the most recent `ObjectExtractionJob` for this document has `status=completed`
- **THEN** the document's `processingStatus` SHALL be `completed`

#### Scenario: Extraction failed
- **WHEN** the most recent `ObjectExtractionJob` for this document has `status=failed` and no job is currently running
- **THEN** the document's `processingStatus` SHALL be `extraction_failed`

### Requirement: UI displays processing status with meaningful badges
The documents list page and document detail modal SHALL render a status badge for each document using the `processingStatus` value, with the following visual treatments:

| Status | Badge style | Label |
|---|---|---|
| `converting` | warning (animated/pulsing) | Converting… |
| `conversion_failed` | error | Conversion Failed |
| `ready_for_extraction` | info | Ready for Extraction |
| `extracting` | warning (animated/pulsing) | Extracting… |
| `extraction_failed` | error | Extraction Failed |
| `completed` | success | Extracted |

The badge SHALL replace (not sit alongside) the existing separate conversion-status and extraction-status badge display on the documents list row.

#### Scenario: Document list shows status badges
- **WHEN** the documents list page renders
- **THEN** each document row SHALL display exactly one status badge derived from `processingStatus`

#### Scenario: Detail modal shows status badge
- **WHEN** the DocumentDetailModal is open on the Properties tab
- **THEN** the Processing Status section SHALL display the unified `processingStatus` badge
