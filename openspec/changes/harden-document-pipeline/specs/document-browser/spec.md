## ADDED Requirements

### Requirement: Show parse progress and timeline

The documents page SHALL show a document's parse state and, for an in-flight or failed parse, its stage timeline with the current or failed stage highlighted.

#### Scenario: In-progress parse shown

- **WHEN** the user opens a document whose parse is running
- **THEN** the page shows the current parse stage and progress

#### Scenario: Failed parse shown

- **WHEN** the user opens a document whose parse failed
- **THEN** the page shows the stage timeline with the failed stage and its error

### Requirement: Stop a parse from the documents page

The documents page SHALL let the user stop an in-flight parse.

#### Scenario: Stop an in-flight parse

- **WHEN** the user activates stop on a running parse and confirms
- **THEN** the document's parse is cancelled and the page reflects the cancelled state

### Requirement: Reparse a document from the documents page

The documents page SHALL let the user reparse a document, optionally selecting a parser engine.

#### Scenario: Reparse a document

- **WHEN** the user activates reparse and optionally selects an engine
- **THEN** the page starts a new parse and shows the parse progress
