## ADDED Requirements

### Requirement: Documents belong to a logical revision group
The system SHALL model a logical document as a group of one or more revisions. Every row in `kb.documents` SHALL carry a `document_group_id` (NOT NULL) shared by all revisions of the same logical document, a `version_number` unique within the group and increasing monotonically from 1, a `supersedes_document_id` pointing at the immediately preceding revision (null for the first revision), an `is_current` flag, and an `applied_at` timestamp (null until the revision has been applied to the main graph).

`is_current` identifies the group's **authoritative** revision — the revision whose extracted content the main graph reflects. A group SHALL have exactly one current revision at all times. A revision that is not current and not yet applied is **pending**; a revision that was applied but is no longer current is **superseded**. The first revision of a group is created current and applied, since it defines the document.

The database SHALL enforce these invariants:
- `document_group_id` is NOT NULL after migration.
- A unique index on `(document_group_id, version_number)` enforces version uniqueness within a group.
- A partial unique index on `(document_group_id) WHERE is_current` enforces exactly one current revision per group.

The existing `parent_document_id` column SHALL retain its current meaning (document hierarchy) and SHALL NOT be used to represent revisions.

#### Scenario: Existing document is backfilled as a single-revision group
- **WHEN** the migration runs against a project that already has documents
- **THEN** each existing document has `document_group_id = id`, `version_number = 1`, `supersedes_document_id = NULL`, `is_current = true`, and `applied_at` set

#### Scenario: Exactly one current revision per group
- **WHEN** an attempt is made to persist a second document row with `is_current = true` for the same `document_group_id`
- **THEN** the write fails on the partial unique index rather than producing two current revisions

#### Scenario: Version uniqueness within a group
- **WHEN** an attempt is made to persist two revisions with the same `(document_group_id, version_number)`
- **THEN** the write fails on the unique index

#### Scenario: New standalone document satisfies the invariant
- **WHEN** a document is created through any existing creation path (raw-content create or file upload)
- **THEN** it is inserted with `document_group_id = id`, `version_number = 1`, `is_current = true`, `supersedes_document_id = NULL`, and is treated as applied, so it forms a valid single-revision group

### Requirement: Create a revision
The API SHALL accept a new revision for an existing document via `POST /api/documents/:id/revisions` as a multipart upload, and SHALL create a new document row in the same revision group instead of a standalone document. The new revision SHALL inherit the base document's `document_group_id`, be assigned `version_number = max(version_number) + 1` within the group, set `supersedes_document_id` to the group's most recent revision, and be created **pending**: `is_current = false` and `applied_at = NULL`. The group's current revision SHALL NOT change when a revision is uploaded; it changes only when the revision is applied (see `document-revision-graph-apply`). Creation SHALL occur in a single transaction, retrying against the new head on a concurrent-insert conflict. The new revision SHALL enter the standard parse → chunk → extract pipeline, with extraction staged.

#### Scenario: Successful revision upload creates a pending revision
- **WHEN** a client uploads a valid file to a known document's revisions endpoint
- **THEN** a new document row is created in the same group with the next version number, marked pending, and the group's current revision is unchanged

#### Scenario: Current revision is unaffected by upload
- **WHEN** a revision is uploaded but not applied
- **THEN** the group's current revision and its `version_number` are unchanged

#### Scenario: Base document unknown
- **WHEN** a client uploads a revision to a document id that does not exist in the project
- **THEN** the API returns a not-found error and creates no document

#### Scenario: Concurrent revision uploads
- **WHEN** two revision uploads race against the same group
- **THEN** they resolve to distinct increasing version numbers and the group still has exactly one current revision

#### Scenario: Revision upload of a rejected file
- **WHEN** a client uploads an empty or disallowed file as a revision
- **THEN** the API rejects the request with a validation error and the group is unchanged

### Requirement: A revision remains pending until applied
A revision that has not been applied SHALL NOT become the group's current revision and SHALL NOT alter the main graph. Applying a revision (see `document-revision-graph-apply`) SHALL, in a single transaction, set that revision's `applied_at` and make it current while demoting the prior current revision, preserving exactly one current revision per group.

#### Scenario: Pending revision is not current
- **WHEN** a revision has been uploaded and its extraction has completed, but no apply has been requested
- **THEN** the revision reports `isCurrent = false` and no `appliedAt`, and the group's prior revision remains current

#### Scenario: Apply promotes the revision
- **WHEN** a pending revision is applied
- **THEN** that revision becomes current (and applied), and the previously current revision becomes a superseded applied revision

### Requirement: List revisions of a logical document
The API SHALL return the revisions of a document's group via `GET /api/documents/:id/revisions`, ordered by `version_number` descending, each with its id, version number, current flag, applied timestamp, supersession link, conversion status, and creation timestamp, so a client can distinguish current, pending, and superseded revisions. The document list (`GET /api/documents`) SHALL return only current revisions by default (one row per logical document) and SHALL include non-current revisions only when explicitly requested (e.g. `includeSuperseded=true`).

#### Scenario: Group with a pending revision
- **WHEN** a client lists revisions for a group that has one current revision and one pending revision
- **THEN** the response contains both, ordered newest-first, with exactly one current and the newer one marked pending (not applied)

#### Scenario: Single-revision group
- **WHEN** a client lists revisions for a document that has never been revised
- **THEN** the API returns a single entry for that document, marked current and applied

#### Scenario: Document list shows logical documents
- **WHEN** a client lists documents without the include-superseded option
- **THEN** the response contains exactly one row per logical document, the current revision, and no pending or superseded revisions

### Requirement: Discard a revision
The API SHALL allow discarding a revision via `DELETE /api/documents/:id/revisions/:revId`. Only a **pending** revision (not current and not applied) MAY be discarded; the current revision and any applied revision SHALL be rejected with a validation error. Discarding SHALL delete the revision row, its chunks, and its staging branch (including objects staged on it).

Because a pending revision is never current, discarding it restores the previous state cleanly and serves as the undo path for a mistaken upload. Discarding SHALL preserve revision-chain integrity: the discarded revision's immediate successor (if any) SHALL have its `supersedes_document_id` re-pointed to the discarded revision's predecessor. Remaining revisions' `version_number` values SHALL be left unchanged and SHALL NOT be renumbered.

#### Scenario: Discard a pending revision (undo)
- **WHEN** a client discards a pending revision that was uploaded but not applied
- **THEN** that revision, its chunks, and its staging branch are removed, and the group's current revision is unchanged

#### Scenario: Discard the current revision is rejected
- **WHEN** a client attempts to discard the currently current revision
- **THEN** the API returns a validation error and nothing is deleted

#### Scenario: Discard an applied revision is rejected
- **WHEN** a client attempts to discard a revision that has already been applied
- **THEN** the API returns a validation error and nothing is deleted

#### Scenario: Chain stays valid after discarding a middle revision
- **WHEN** a client discards a pending revision that is superseded by a later pending revision
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
A document id SHALL address exactly one revision row; read endpoints SHALL NOT implicitly resolve an id to the group's current revision. `GET /api/documents/:id` SHALL return the addressed revision's `documentGroupId`, `versionNumber`, `isCurrent`, `appliedAt`, and `supersedesDocumentId`. The group's current revision is identified by the `isCurrent` flag in the detail response or the revisions list.

#### Scenario: Fetching a pending revision
- **WHEN** a client fetches a document by a pending revision id
- **THEN** the response is that revision, with `isCurrent = false`, no `appliedAt`, and its `documentGroupId`

#### Scenario: Fetching the current revision
- **WHEN** a client fetches the current revision of a revised document
- **THEN** the response is that revision, with `isCurrent = true` and an `appliedAt`
