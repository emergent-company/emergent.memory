## ADDED Requirements

### Requirement: Document detail shows a revisions section
The gateway document detail page SHALL render a Revisions section listing the logical document's revisions newest-first, each showing its version number, creation timestamp, current flag, and conversion status. Under the revision model every document is a group of at least one revision, so the section SHALL present a "single version" state when the group has exactly one revision, and a list when it has more than one.

#### Scenario: Document with multiple revisions
- **WHEN** the document detail page renders for a document in a group with more than one revision
- **THEN** the Revisions section lists each revision newest-first with its version number and current flag

#### Scenario: Document with exactly one revision
- **WHEN** the document detail page renders for a document in a group with exactly one revision
- **THEN** the Revisions section shows a "single version" state rather than a list of one

### Requirement: Revisions section shows the revision delta
The Revisions section SHALL present the content diff and entity delta between the previous revision and a selected revision, showing added/removed lines and added/updated/removed graph objects and relationships. The delta SHALL be loaded from the revision diff API and SHALL render an explicit "not yet available" state while a revision's content is still parsing.

#### Scenario: Delta available
- **WHEN** a user views a revision whose content has parsed
- **THEN** the section shows the line diff and the added/updated/removed entity lists for that revision against the previous revision

#### Scenario: Revision still parsing
- **WHEN** a user views a revision whose content has not finished parsing
- **THEN** the section shows a "processing" state instead of a delta

### Requirement: Revisions section applies or discards a revision
The Revisions section SHALL provide actions to apply a staged revision to the main graph and to discard it. Apply SHALL show the resulting added/updated/removed counts on success. Discard SHALL require confirmation. The currently current revision SHALL NOT be offered a discard action.

#### Scenario: Apply a staged revision
- **WHEN** the user clicks Apply on a staged revision
- **THEN** the revision's delta is applied to the main graph and the page reports the applied counts

#### Scenario: Discard a staged revision
- **WHEN** the user confirms Discard on a non-current revision
- **THEN** the revision and its staged objects are removed and the revisions list refreshes

#### Scenario: Current revision has no discard action
- **WHEN** the Revisions section renders the current revision
- **THEN** no Discard action is offered for it

### Requirement: Upload a revision from the document detail page
The document detail page SHALL provide an upload control that creates a new revision of the current document rather than a standalone document.

#### Scenario: Upload a revision from the detail page
- **WHEN** the user submits a file through the revision upload control
- **THEN** a new revision of the current logical document is created and appears at the top of the revisions list
