## ADDED Requirements

### Requirement: Documents are versioned via a supersedes chain

A document SHALL carry a stable `canonical_id` shared by all its versions, an optional
`supersedes_id` recording the version that superseded this row (its successor), and a monotonically
increasing `version`. The current version (HEAD) SHALL be the version whose `supersedes_id` IS NULL.
Creating a new version SHALL append a new HEAD document row that shares the HEAD's `canonical_id`
and increments `version`, and SHALL set the previous HEAD's `supersedes_id` to the new row's id so
the previous version is retired.

#### Scenario: New version appends to the chain

- **WHEN** a document's content changes
- **THEN** a new document row is created with the same `canonical_id` as the prior version, an incremented `version`, and `supersedes_id` NULL, and the prior version's `supersedes_id` is set to the new row

#### Scenario: Old versions are retained

- **WHEN** a new version is created
- **THEN** the prior version row remains in the database and is not deleted

#### Scenario: Exactly one HEAD per canonical document

- **WHEN** a canonical document has multiple versions
- **THEN** exactly one of its rows has `supersedes_id` NULL

### Requirement: Reads default to the HEAD version

Default document listing and retrieval SHALL return only HEAD versions unless history is
explicitly requested.

#### Scenario: Default listing returns HEAD only

- **WHEN** a client lists documents and a canonical document has multiple versions
- **THEN** only its HEAD version appears

#### Scenario: Head lookup by source identity

- **WHEN** the server looks up a document by `(source_id, source_external_id)`
- **THEN** it resolves the HEAD version for that source item

### Requirement: Document version history is retrievable

The server SHALL expose a document's full version history, ordered by `version`, so a client can
inspect prior versions of a canonical document.

#### Scenario: History returns all versions in order

- **WHEN** a client requests the version history of a canonical document with multiple versions
- **THEN** the server returns every version ordered by `version`

#### Scenario: History of a single-version document

- **WHEN** a client requests the version history of a document with one version
- **THEN** the server returns that single version, not an error

### Requirement: Documents are attributable to a single source schema

An ingested document SHALL carry a nullable `source_id` (FK to the source, `ON DELETE SET NULL`),
a nullable `source_external_id`, a nullable `source_revision_id`, a nullable `source_text_hash`, a
nullable `source_synced_at`, a `source_status`, and a nullable `source_deleted_at`. A partial
unique index SHALL enforce at most one HEAD per `(source_id, source_external_id)` while allowing
multiple version rows.

#### Scenario: One HEAD per source external item

- **WHEN** the same external item is ingested twice under one source
- **THEN** exactly one HEAD document exists for that `(source_id, source_external_id)`

#### Scenario: Deleting a source keeps documents

- **WHEN** a source is deleted
- **THEN** its documents are retained with `source_id` set to NULL and re-ingesting the same external item under a new source creates a distinct document

### Requirement: Deleted-from-source documents retain history and are marked

When an in-scope source item is removed or trashed at the provider, the server SHALL mark its HEAD
document `source_status = 'deleted_from_source'` and stamp `source_deleted_at`, retaining the
document row, its versions, its chunks, and its graph attribution. Documents marked
`deleted_from_source` SHALL be excluded from default document listings and retrieval. If the item
re-appears in scope, a new active version SHALL be appended and `source_status` SHALL return to
`active`.

#### Scenario: Provider removal marks the document

- **WHEN** an in-scope item is removed or trashed at the provider
- **THEN** its HEAD document is marked `deleted_from_source` with a timestamp and is excluded from default listings and retrieval

#### Scenario: History survives a source removal

- **WHEN** a `deleted_from_source` document's version history is requested
- **THEN** the server still returns all retained versions

#### Scenario: Re-appearance reactivates the document

- **WHEN** a previously removed item re-appears in scope with changed content
- **THEN** a new active version is appended and `source_status` returns to `active`
