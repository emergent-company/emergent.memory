## ADDED Requirements

### Requirement: Reparse a document

The API SHALL let a client reparse a document that has stored original bytes, with an optional new parse configuration, and SHALL return a reference to the resulting parse job.

#### Scenario: Reparse requested

- **WHEN** a client requests a reparse of a document with stored original bytes and an optional engine or OCR override
- **THEN** the API starts a parse job using the stored original bytes and returns a reference to it

#### Scenario: Reparse without original bytes

- **WHEN** a client requests a reparse of a document that has no stored original bytes
- **THEN** the API returns a validation error and starts no job

#### Scenario: Unknown document

- **WHEN** a client requests a reparse of a document id that does not exist
- **THEN** the API returns a not-found error response

### Requirement: Get parse status and timeline

The API SHALL return a document's most recent parse state, progress, and stage timeline.

#### Scenario: Status returned

- **WHEN** a client requests a document's parse status
- **THEN** the API returns the most recent parse job's state, current stage, and stage timeline

#### Scenario: Never parsed

- **WHEN** a client requests parse status for a document that has never been parsed
- **THEN** the API returns an empty timeline and an unparsed state, not an error

### Requirement: Stop a parse

The API SHALL let a client stop a running or queued parse for a document and SHALL report the resulting state.

#### Scenario: Stop requested

- **WHEN** a client requests a stop on a running parse
- **THEN** the API cancels the parse and reports the document as cancelled

#### Scenario: Nothing to stop

- **WHEN** a client requests a stop on a parse that has already finished
- **THEN** the API reports that there is nothing to stop
