# cli-journal Specification

## ADDED Requirements

### Requirement: Journal list `--cursor` flag

`memory journal list` SHALL accept a `--cursor` flag carrying the opaque token
returned as `next_cursor` by a previous invocation. When supplied, the command
SHALL pass it to the server's `cursor` query parameter and return the page of
journal entries that immediately follows the entry the cursor points at, using
the server's existing `(created_at, id)` keyset pagination; the page SHALL not
repeat entries from the previous page. When `--cursor` is omitted the command's
behavior SHALL be unchanged.

#### Scenario: Cursor resumes after the last entry without overlap

- **WHEN** `memory journal list --limit 2` prints a `Next cursor: <token>` line
- **AND** `memory journal list --limit 2 --cursor <token>` runs
- **THEN** the second invocation returns the entries following the first page with no repeats

#### Scenario: Default behavior without a cursor is unchanged

- **WHEN** `memory journal list` runs without `--cursor`
- **THEN** it returns the most recent entries for the requested `--since` window exactly as before

#### Scenario: Last page omits the cursor

- **WHEN** the server reports no further entries (`has_more` false)
- **THEN** no `Next cursor` line is printed and the JSON response reports `has_more: false`

### Requirement: Journal list surfaces the next-page cursor

After a non-final page, `memory journal list` SHALL print the actual
`next_cursor` token so the user can copy it: a `Next cursor: <token>` line and a
`Use --cursor <token> to fetch the next page.` hint. With `--output json` the
response SHALL include `next_cursor` and `has_more`.

#### Scenario: Text output shows the token

- **WHEN** `memory journal list --limit 2` returns a page with more entries available
- **THEN** the output ends with a `Next cursor: <token>` line and a hint to pass it via `--cursor`

#### Scenario: JSON output carries next_cursor

- **WHEN** `memory journal list --limit 2 --output json` returns a page with more entries available
- **THEN** the JSON object contains a non-empty `next_cursor` field and `has_more: true`

### Requirement: Invalid cursor surfaces a clear error

An invalid or malformed `--cursor` value SHALL be rejected by the server with an
HTTP 400; the CLI SHALL surface that as a non-zero exit with an error mentioning
the invalid cursor, never a panic.

#### Scenario: Malformed cursor returns a clear error

- **WHEN** `memory journal list --cursor not-a-valid-token` runs against the server
- **THEN** the command exits non-zero with an error mentioning the invalid cursor
