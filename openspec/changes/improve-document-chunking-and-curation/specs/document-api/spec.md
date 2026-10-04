## ADDED Requirements

### Requirement: Edit a chunk

The API SHALL let a client replace a chunk's text and SHALL return the resulting revision reference. Editing a chunk SHALL schedule re-embedding and preserve the chunk's document provenance.

#### Scenario: Chunk edited

- **WHEN** a client submits new text for an existing chunk
- **THEN** the API returns the new revision reference and the chunk is scheduled for re-embedding

#### Scenario: Unknown chunk

- **WHEN** a client edits a chunk id that does not exist
- **THEN** the API returns a not-found error response

#### Scenario: Oversized edit rejected

- **WHEN** a client submits chunk text larger than the server-side maximum
- **THEN** the API rejects it with a validation error and records no revision

### Requirement: List chunk revisions

The API SHALL return a chunk's revisions ordered by ascending revision sequence number, each with its text, author, and timestamp. The original content SHALL be present as the first revision.

#### Scenario: Revisions listed

- **WHEN** a client lists revisions for an edited chunk
- **THEN** the API returns the revisions ordered by sequence number with text, author, and timestamp

#### Scenario: Unedited chunk

- **WHEN** a client lists revisions for a chunk that has never been edited
- **THEN** the API returns the chunk's original revision only

#### Scenario: Revision listing is bounded

- **WHEN** a chunk has more revisions than the server's listing cap
- **THEN** the API returns at most the cap, in ascending sequence order

### Requirement: Diff and roll back a chunk

The API SHALL return the diff between two revisions of a chunk and SHALL let a client roll a chunk back to a previous revision.

#### Scenario: Diff returned

- **WHEN** a client requests the diff between two revisions of a chunk
- **THEN** the API returns the textual differences between them

#### Scenario: Rollback applied

- **WHEN** a client rolls a chunk back to a previous revision
- **THEN** the API restores that revision's text and records the rollback as a new revision

#### Scenario: Oversized diff rejected

- **WHEN** a client requests a diff whose inputs exceed the server's diff size limit
- **THEN** the API returns a validation error instead of attempting the diff

#### Scenario: Rollback to current is a no-op

- **WHEN** a client rolls a chunk back to the revision whose text is already current
- **THEN** the API returns the current revision and records no new revision

### Requirement: Manage chunking configuration

The API SHALL let a client read and write a project's chunking configuration (strategy and parameters) and SHALL reject invalid configurations.

#### Scenario: Read configuration

- **WHEN** a client reads the active project's chunking configuration
- **THEN** the API returns the configured strategy and parameters, or the default when unset

#### Scenario: Write invalid configuration rejected

- **WHEN** a client writes a chunking configuration with an unknown strategy or out-of-range parameters
- **THEN** the API returns a validation error and the previous configuration is unchanged
