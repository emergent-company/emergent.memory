## Purpose

The board card drawer surfaces what a completed run produced: its declared deliverables (with a per-item resolution state) and its completion note.

## ADDED Requirements

### Requirement: Card drawer surfaces a completed run's deliverables

The board card drawer SHALL render a completed run's declared deliverables (each an object `type` + `key` taken from the run summary). A deliverable whose `(type, key)` resolves to an existing object SHALL render as a link to that object's detail page; a declared deliverable that does not resolve SHALL render visibly muted with a "not found" hint and SHALL NEVER be silently dropped. Resolution is best-effort presentation: a lookup failure degrades that one row to `missing` and SHALL NOT block or fail the drawer.

#### Scenario: Resolved deliverable links to the object

- **WHEN** a completed run declared a deliverable whose type and key resolve to an existing object
- **THEN** the drawer renders it as a link to the object's detail page

#### Scenario: Declared-but-not-found deliverable stays visible

- **WHEN** a completed run declared a deliverable whose type and key do not resolve to an object
- **THEN** the drawer renders it muted with a "not found" hint
- **AND** does not render a link or drop the row

#### Scenario: No deliverables declared shows an empty state

- **WHEN** a run completed without declaring any deliverables
- **THEN** the drawer renders an explicit empty state (e.g. "No deliverables declared")

### Requirement: Card drawer surfaces a completion note

The board card drawer SHALL render the newest completed run's summary prose as a sanitized markdown completion note, and SHALL omit the section entirely when there is no summary.

#### Scenario: Completion note rendered

- **WHEN** the newest run's summary carries prose
- **THEN** the drawer renders it as sanitized markdown

#### Scenario: Empty summary omitted

- **WHEN** the newest run's summary has no prose
- **THEN** no completion-note section is rendered
