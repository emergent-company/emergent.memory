## ADDED Requirements

### Requirement: Edit a chunk from the documents page

The documents page SHALL let a user edit a chunk's text inline and save the change, showing the result in the chunk list.

#### Scenario: Edit and save a chunk

- **WHEN** the user edits a chunk's text and saves
- **THEN** the chunk list shows the updated text

#### Scenario: Edit fails

- **WHEN** saving a chunk edit fails
- **THEN** a clear error is shown and the chunk's previous text is preserved

### Requirement: View a chunk's revision history

The documents page SHALL show a chunk's revision history and let the user view the diff between revisions and roll back to a previous revision.

#### Scenario: History shown

- **WHEN** the user opens a chunk's history
- **THEN** the revisions are listed with their author and timestamp

#### Scenario: Roll back a chunk

- **WHEN** the user selects a previous revision and confirms rollback
- **THEN** the chunk list shows that revision's text
