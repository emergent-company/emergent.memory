## ADDED Requirements

### Requirement: Upload a document revision from the CLI
The `memory documents` command SHALL support uploading a local file as a new revision of an existing document via `memory documents upload <file> --revision-of <documentId>`. The command SHALL target the documents revisions API and SHALL report the created revision's id and version number. The existing upload behaviour SHALL be unchanged when `--revision-of` is omitted.

#### Scenario: Upload a revision
- **WHEN** the user runs `memory documents upload notes.md --revision-of <id>`
- **THEN** a new revision is created for that document and the command prints the revision id and version number

#### Scenario: Upload without revision flag unchanged
- **WHEN** the user runs `memory documents upload notes.md` without `--revision-of`
- **THEN** a standalone document is created as before

### Requirement: List revisions from the CLI
The CLI SHALL support `memory documents revisions <documentId>` to list a logical document's revisions, ordered newest-first, showing each revision's version number, id, current flag, and conversion status. The command SHALL support `--output json` consistent with other `documents` subcommands.

#### Scenario: List revisions
- **WHEN** the user runs `memory documents revisions <id>` for a revised document
- **THEN** the command prints each revision newest-first with its version number, id, and current flag

### Requirement: Diff revisions from the CLI
The CLI SHALL support `memory documents diff <documentId> [--from <version>] [--to <version>]` to show the content diff and entity delta between two revisions. When flags are omitted the command SHALL diff the previous revision against the current revision. The command SHALL support `--output json`.

#### Scenario: Diff default revisions
- **WHEN** the user runs `memory documents diff <id>` without version flags
- **THEN** the command shows the line diff and entity delta between the previous and current revisions

#### Scenario: Diff explicit revisions
- **WHEN** the user runs `memory documents diff <id> --from 1 --to 3`
- **THEN** the command shows the diff and delta for those two revisions

### Requirement: Apply or discard a revision from the CLI
The CLI SHALL support `memory documents apply-revision <documentId> --revision <version>` and `memory documents discard-revision <documentId> --revision <version>`. Apply SHALL report added, updated, and removed counts. Both commands SHALL require the target revision explicitly.

#### Scenario: Apply a revision
- **WHEN** the user runs `memory documents apply-revision <id> --revision 2`
- **THEN** the revision's staged delta is applied to the main graph and the command prints the added/updated/removed counts

#### Scenario: Discard a revision
- **WHEN** the user runs `memory documents discard-revision <id> --revision 2`
- **THEN** the revision and its staged objects are removed and the command confirms discarding

#### Scenario: Missing revision flag
- **WHEN** the user runs `memory documents apply-revision <id>` without `--revision`
- **THEN** the command fails with a usage error and makes no API call
