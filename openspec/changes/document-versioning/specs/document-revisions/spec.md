## ADDED Requirements

### Requirement: Documents belong to a logical revision group
The system SHALL model a logical document as a group of one or more revisions. Every row in `kb.documents` SHALL carry a `document_group_id` that is shared by all revisions of the same logical document, a `version_number` that is unique within the group and increases monotonically from 1, a `supersedes_document_id` pointing at the immediately preceding revision (null for the first revision), and an `is_current` flag. There SHALL be exactly one revision with `is_current = true` per `document_group_id`, enforced by a partial unique index.

The existing `parent_document_id` column SHALL retain its current meaning (document hierarchy) and SHALL NOT be used to represent revisions.

#### Scenario: Existing document is backfilled as a single-revision group
- **WHEN** the migration runs against a project that already has documents
- **THEN** each existing document has `document_group_id = id`, `version_number = 1`, `supersedes_document_id = NULL`, and `is_current = true`

#### Scenario: Exactly one current revision per group
- **WHEN** an attempt is made to persist a second document row with `is_current = true` for the same `document_group_id`
- **THEN** the write fails on the partial unique index rather than producing two current revisions

### Requirement: Create a revision
The API SHALL accept a new revision for an existing document via `POST /api/documents/:id/revisions` as a multipart upload, and SHALL create a new document row in the same revision group instead of a standalone document. The new revision SHALL inherit the base document's `document_group_id`, be assigned `version_number = max(version_number) + 1` within the group, set `supersedes_document_id` to the currently current revision, and become the group's current revision. The previous current revision SHALL be demoted (`is_current = false`). The new revision SHALL enter the standard parse → chunk → extract pipeline.

#### Scenario: Successful revision upload
- **WHEN** a client uploads a valid file to a known document's revisions endpoint
- **THEN** a new document row is created in the same group with the next version number, the previous revision is no longer current, and the response identifies the new revision and its version number

#### Scenario: Base document unknown
- **WHEN** a client uploads a revision to a document id that does not exist in the project
- **THEN** the API returns a not-found error and creates no document

#### Scenario: Revision upload of a rejected file
- **WHEN** a client uploads an empty or disallowed file as a revision
- **THEN** the API rejects the request with a validation error and the group's current revision is unchanged

### Requirement: List revisions of a logical document
The API SHALL return the revisions of a document's group via `GET /api/documents/:id/revisions`, ordered by `version_number` descending, each with its id, version number, current flag, supersession link, conversion status, and creation timestamp.

#### Scenario: Group with multiple revisions
- **WHEN** a client lists revisions for a group that has three revisions
- **THEN** the API returns three entries ordered newest-first, exactly one marked current

#### Scenario: Single-revision group
- **WHEN** a client lists revisions for a document that has never been revised
- **THEN** the API returns a single entry for that document, marked current

### Requirement: Discard a revision
The API SHALL allow discarding a revision via `DELETE /api/documents/:id/revisions/:revId`. Discarding SHALL delete the revision's chunks and any staged graph objects for that revision. The currently current revision of a group SHALL NOT be discardable; attempting to discard it SHALL return a validation error. Discarding a revision SHALL leave the remaining revisions' `version_number` values unchanged.

#### Scenario: Discard a superseded revision
- **WHEN** a client discards a non-current revision
- **THEN** that revision and its chunks are removed and the current revision is unaffected

#### Scenario: Discard the current revision is rejected
- **WHEN** a client attempts to discard the currently current revision
- **THEN** the API returns a validation error and nothing is deleted

### Requirement: Resolve a logical document to its current revision
Document read endpoints that operate on a single document SHALL expose `documentGroupId`, `versionNumber`, and `isCurrent` so clients can distinguish a revision from a logical document. Unless a specific revision id is requested, reads SHALL resolve to the group's current revision.

#### Scenario: Fetching a superseded revision
- **WHEN** a client fetches a document by a superseded revision id
- **THEN** the response includes that revision's `versionNumber` and `isCurrent = false` and its `documentGroupId`

#### Scenario: Fetching the current revision
- **WHEN** a client fetches the current revision of a revised document
- **THEN** the response includes `isCurrent = true` and the group's highest version number
