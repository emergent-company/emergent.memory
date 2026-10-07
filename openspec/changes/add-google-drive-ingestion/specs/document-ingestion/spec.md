## ADDED Requirements

### Requirement: Ingest a document from an external source

The server SHALL upsert a document supplied by an external source, identified by a `source_id`
and a provider `source_external_id`, recording both on the document together with the provider
revision id, a hash of the parsed text, and a sync timestamp. Ingesting an item whose HEAD
document already exists for `(source_id, source_external_id)` SHALL update that HEAD rather than
create a second HEAD.

#### Scenario: First ingestion creates a document

- **WHEN** an item with a new `(source_id, source_external_id)` is ingested
- **THEN** the server creates a HEAD document recording the source id, external id, revision id, and text hash

#### Scenario: Repeated ingestion targets the existing HEAD

- **WHEN** an item whose `(source_id, source_external_id)` already has a HEAD document is ingested
- **THEN** the server updates that HEAD document and does not create a second HEAD

#### Scenario: Concurrent duplicate ingestion is deduplicated

- **WHEN** the same source item is ingested concurrently
- **THEN** exactly one HEAD document exists for that `(source_id, source_external_id)`

### Requirement: Change detection is by parsed text, not raw bytes

The server SHALL treat an ingested item as changed only when the hash of its **parsed text**
differs from the HEAD document's stored text hash. A raw-byte or revision-id change that leaves
the parsed text identical SHALL NOT create a new version or schedule re-extraction; it SHALL
update only the revision id and sync timestamp.

#### Scenario: Identical parsed text is a no-op version

- **WHEN** an item is re-ingested, its provider revision id changed, and its parsed text is identical
- **THEN** the server updates the revision id and sync timestamp, creates no new version, and does not re-extract

#### Scenario: Changed parsed text produces a new version

- **WHEN** an item is re-ingested and its parsed text hash differs from the HEAD's stored text hash
- **THEN** the server creates a new document version and schedules re-extraction

### Requirement: Ingested content flows through the existing pipeline

Ingested content SHALL reach the existing pipeline unchanged: the document version is chunked
through the existing chunking path and extraction is triggered for it, with resulting graph
objects attributable to the source.

#### Scenario: Ingested item is chunked and extracted

- **WHEN** an ingested item produces a new document version
- **THEN** the version is chunked through the existing chunking path and extraction is triggered for it

### Requirement: Ingestion is project-scoped and authenticated

All ingestion operations SHALL require the same authentication boundary as the existing document
endpoints and SHALL be scoped to the active project, and MUST NOT ingest into or read from another
project.

#### Scenario: Unauthenticated ingestion rejected

- **WHEN** a caller invokes an ingestion operation without valid credentials
- **THEN** the server returns an unauthorized response and ingests nothing

#### Scenario: Cross-project isolation

- **WHEN** an item is ingested whose external id exists under a source in a different project
- **THEN** the server creates or updates a document in the caller's active project only
