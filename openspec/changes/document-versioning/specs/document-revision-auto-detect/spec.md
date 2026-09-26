## ADDED Requirements

### Requirement: Upload auto-detects the logical document it belongs to
A document upload SHALL attempt to detect whether the uploaded file is a new revision of an existing logical document in the same project, using deterministic signals evaluated in priority order:

1. **External source id** — an upload carrying an `externalSourceId` that matches an existing revision's `external_source_id` in the project (used by connectors and data-source imports).
2. **File hash** — the uploaded file's `file_hash` exactly matches the `file_hash` of an existing revision in the project (a re-upload or revert of a known version).
3. **Filename** — the uploaded file's normalized basename (case-insensitive, trimmed) matches the filename of exactly one logical document's current revision in the project.

Detection SHALL be scoped to the caller's project and SHALL NOT match documents in another project. When `externalSourceId` is supplied, it SHALL be persisted on the created revision so subsequent uploads can match it. Detection SHALL NOT run when the caller explicitly targets a document (e.g. `?revisionOf=<id>` or the revisions endpoint) — an explicit target always wins.

#### Scenario: Same file re-uploaded
- **WHEN** a file is uploaded whose `file_hash` matches a revision in the project
- **THEN** the upload is detected as belonging to that revision's logical document

#### Scenario: Same filename as a single current document
- **WHEN** a file is uploaded whose normalized basename matches the filename of exactly one logical document's current revision in the project
- **THEN** the upload is detected as belonging to that document

#### Scenario: External source id match
- **WHEN** a connector uploads a file carrying an `externalSourceId` already present on a revision in the project
- **THEN** the upload is detected as belonging to that revision's logical document

#### Scenario: No detection across projects
- **WHEN** an uploaded file matches a document in a different project but nothing in the caller's project
- **THEN** no detection occurs and the upload is treated as a standalone document

### Requirement: A confident match is created as a pending revision
When detection finds exactly one candidate via a high-confidence signal (external source id or file hash), or exactly one candidate via filename, the upload SHALL create a **pending revision** of the detected logical document (see `document-revisions`) instead of a standalone document. The created revision SHALL record which signal matched and the detected document id, and SHALL be reviewable and discardable like any pending revision.

#### Scenario: Confident match auto-links
- **WHEN** an upload yields exactly one detected document
- **THEN** a pending revision of that document is created and the upload response identifies the detected document and the signal used

#### Scenario: Auto-linked revision is reversible
- **WHEN** an auto-linked pending revision was detected incorrectly
- **THEN** the user can discard it (a pending revision is never current), leaving the graph and the current revision unchanged

#### Scenario: Detection is recorded
- **WHEN** a revision is created by detection
- **THEN** the revision records the matched signal and the detected logical document id

### Requirement: Ambiguous or missing matches fall back to standalone
When detection finds no candidate, the upload SHALL create a standalone document as today. When detection finds multiple candidates (ambiguous), the upload SHALL create a standalone document and SHALL include a non-blocking suggestion of the candidate document(s) in the upload response, so a client can offer "make this a revision of …?"; the system SHALL NOT auto-link an ambiguous upload.

#### Scenario: No candidate
- **WHEN** an upload matches no existing document
- **THEN** a standalone document is created and no revision is created

#### Scenario: Ambiguous filename match
- **WHEN** an uploaded file's normalized basename matches the current revision of more than one logical document
- **THEN** a standalone document is created and the response suggests the candidate documents without linking

### Requirement: Detection can be disabled or overridden
Clients SHALL be able to disable detection for a single upload (e.g. `?detect=false` / `--no-detect`), forcing a standalone document, and SHALL be able to target a revision explicitly (`?revisionOf=<id>` / `--revision-of <id>`), which bypasses detection entirely. Disabling detection SHALL NOT affect the other upload semantics (size limits, MIME validation, staging, extraction).

#### Scenario: Detection disabled
- **WHEN** a client uploads a file that would otherwise auto-link, with detection disabled
- **THEN** a standalone document is created

#### Scenario: Explicit target bypasses detection
- **WHEN** a client uploads a file with an explicit revision target
- **THEN** the upload creates a revision of that target and detection is not consulted

#### Scenario: Batch uploads detect per file
- **WHEN** a batch of files is uploaded
- **THEN** detection is applied independently per file and each result reports whether it was linked, standalone, or suggested
