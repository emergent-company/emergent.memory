## ADDED Requirements

### Requirement: Documents belong to a logical revision group
The system SHALL model a logical document as a group of one or more revisions. Every row in `kb.documents` SHALL carry a `document_group_id` (NOT NULL) shared by all revisions of the same logical document, a `version_number` unique within the group and increasing monotonically from 1, a `supersedes_document_id` pointing at the immediately preceding revision (null for the first revision), and an `is_current` flag.

The database SHALL enforce these invariants:
- `document_group_id` is NOT NULL after migration.
- A unique index on `(document_group_id, version_number)` enforces version uniqueness within a group.
- A partial unique index on `(document_group_id) WHERE is_current` enforces at most one current revision per group.

The existing `parent_document_id` column SHALL retain its current meaning (document hierarchy) and SHALL NOT be used to represent revisions.

#### Scenario: Existing document is backfilled as a single-revision group
- **WHEN** the migration runs against a project that already has documents
- **THEN** each existing document has `document_group_id = id`, `version_number = 1`, `supersedes_document_id = NULL`, and `is_current = true`

#### Scenario: Exactly one current revision per group
- **WHEN** an attempt is made to persist a second document row with `is_current = true` for the same `document_group_id`
- **THEN** the write fails on the partial unique index rather than producing two current revisions

#### Scenario: Version uniqueness within a group
- **WHEN** an attempt is made to persist two revisions with the same `(document_group_id, version_number)`
- **THEN** the write fails on the unique index

#### Scenario: New standalone document satisfies the invariant
- **WHEN** a document is created through any existing creation path (raw-content create or file upload)
- **THEN** it is inserted with `document_group_id = id`, `version_number = 1`, `is_current = true`, and `supersedes_document_id = NULL`, so it forms a valid single-revision group

### Requirement: Create a revision
The API SHALL accept a new revision for an existing document via `POST /api/documents/:id/revisions` as a multipart upload, and SHALL create a new document row in the same revision group instead of a standalone document. The new revision SHALL inherit the base document's `document_group_id`, be assigned `version_number = max(version_number) + 1` within the group, set `supersedes_document_id` to the group's current revision, and become the group's current revision. The previous current revision SHALL be demoted (`is_current = false`). The demotion, version assignment, and insert SHALL occur in a single transaction, retrying against the new head on a concurrent-insert conflict. The new revision SHALL enter the standard parse → chunk → extract pipeline.

#### Scenario: Successful revision upload
- **WHEN** a client uploads a valid file to a known document's revisions endpoint
- **THEN** a new document row is created in the same group with the next version number, the previous revision is no longer current, and the response identifies the new revision and its version number

#### Scenario: Base document unknown
- **WHEN** a client uploads a revision to a document id that does not exist in the project
- **THEN** the API returns a not-found error and creates no document

#### Scenario: Concurrent revision uploads
- **WHEN** two revision uploads race against the same group
- **THEN** they resolve to distinct increasing version numbers and exactly one of them remains current

#### Scenario: Revision upload of a rejected file
- **WHEN** a client uploads an empty or disallowed file as a revision
- **THEN** the API rejects the request with a validation error and the group's current revision is unchanged

### Requirement: List revisions of a logical document
The API SHALL return the revisions of a document's group via `GET /api/documents/:id/revisions`, ordered by `version_number` descending, each with its id, version number, current flag, supersession link, conversion status, creation timestamp, and whether the revision has been applied to the main graph. The document list (`GET /api/documents`) SHALL return only current revisions by default (one row per logical document) and SHALL include superseded revisions only when explicitly requested (e.g. `includeSuperseded=true`).

#### Scenario: Group with multiple revisions
- **WHEN** a client lists revisions for a group that has three revisions
- **THEN** the API returns three entries ordered newest-first, exactly one marked current

#### Scenario: Single-revision group
- **WHEN** a client lists revisions for a document that has never been revised
- **THEN** the API returns a single entry for that document, marked current

#### Scenario: Document list shows logical documents
- **WHEN** a client lists documents without the include-superseded option
- **THEN** the response contains exactly one row per logical document, the current revision, and no superseded revisions

### Requirement: Discard a revision
The API SHALL allow discarding a revision via `DELETE /api/documents/:id/revisions/:revId`. Only a revision that is **not current** and **not applied** to the main graph MAY be discarded; the current revision and any applied revision SHALL be rejected with a validation error. Discarding SHALL delete the revision row, its chunks, and its staging branch (including objects staged on it).

Discarding SHALL preserve revision-chain integrity: the discarded revision's immediate successor (if any) SHALL have its `supersedes_document_id` re-pointed to the discarded revision's predecessor. Remaining revisions' `version_number` values SHALL be left unchanged and SHALL NOT be renumbered.

#### Scenario: Discard a staged, superseded revision
- **WHEN** a client discards a non-current revision that was never applied
- **THEN** that revision, its chunks, and its staging branch are removed, and the current revision is unaffected

#### Scenario: Discard the current revision is rejected
- **WHEN** a client attempts to discard the currently current revision
- **THEN** the API returns a validation error and nothing is deleted

#### Scenario: Discard an applied revision is rejected
- **WHEN** a client attempts to discard a revision that has already been applied to the main graph
- **THEN** the API returns a validation error and nothing is deleted

#### Scenario: Chain stays valid after discarding a middle revision
- **WHEN** a client discards a staged revision that is superseded by a later revision
- **THEN** the later revision's `supersedes_document_id` points at the discarded revision's predecessor, or is null when the discarded revision was the first

### Requirement: Generic document deletion removes the whole group
`DELETE /api/documents/:id` (and bulk delete) SHALL delete the entire revision group the document belongs to — every revision and their related chunks, extraction jobs, and graph objects — reusing the existing cascade behaviour. Single-revision removal SHALL be performed only through the revision discard endpoint. This prevents a generic delete from leaving a group with zero current revisions.

#### Scenario: Delete a revised document
- **WHEN** a client deletes a document that has three revisions
- **THEN** all three revisions and their cascaded entities are removed

#### Scenario: Delete does not leave an empty group
- **WHEN** any document deletion completes
- **THEN** no `document_group_id` remains with zero revisions

### Requirement: Document ids address revisions directly
A document id SHALL address exactly one revision row; read endpoints SHALL NOT implicitly resolve an id to the group's current revision. `GET /api/documents/:id` SHALL return the addressed revision's `documentGroupId`, `versionNumber`, `isCurrent`, and `supersedesDocumentId`. The group's current revision is identified by the `isCurrent` flag in the detail response or the revisions list.

#### Scenario: Fetching a superseded revision
- **WHEN** a client fetches a document by a superseded revision id
- **THEN** the response is that revision, with `isCurrent = false` and its `documentGroupId`

#### Scenario: Fetching the current revision
- **WHEN** a client fetches the current revision of a revised document
- **THEN** the response is that revision, with `isCurrent = true` and the group's highest version number
