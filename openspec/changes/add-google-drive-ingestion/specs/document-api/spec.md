## ADDED Requirements

### Requirement: List a document's version history

The API SHALL expose a document's version history, returning each version ordered by version
number with its id, version, creation timestamp, and source revision when present.

#### Scenario: Version history for a versioned document

- **WHEN** a client requests the version history of a document that has multiple versions
- **THEN** the API returns every version ordered by version number

#### Scenario: Version history for an unversioned document

- **WHEN** a client requests the version history of a document that has a single version
- **THEN** the API returns that single version rather than an error

### Requirement: Document responses expose source provenance

Document listing and detail responses SHALL expose source provenance — the source id, external
item id, revision, sync time, and source status — so a client can distinguish uploaded documents
from sourced ones and identify documents marked `deleted_from_source`.

#### Scenario: Sourced document reports provenance

- **WHEN** a client reads a document ingested from a source
- **THEN** the response includes its source id, external item id, revision, sync time, and source status

#### Scenario: Deleted-from-source document is identifiable

- **WHEN** a client reads a document marked `deleted_from_source`
- **THEN** the response reports that source status

### Requirement: Default listings exclude deleted-from-source documents

Default document listing SHALL exclude documents whose source status is `deleted_from_source`
unless the client explicitly requests them.

#### Scenario: Removed source document hidden by default

- **WHEN** a client lists documents and a document is marked `deleted_from_source`
- **THEN** that document is not included unless explicitly requested
