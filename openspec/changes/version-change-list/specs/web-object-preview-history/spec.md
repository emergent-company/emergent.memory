## ADDED Requirements

### Requirement: Per-version change disclosure in the object preview history

Each entry in the object preview's history timeline SHALL be expandable to show the
changes recorded for that version. An entry SHALL be collapsed by default and its
disclosure control SHALL be keyboard operable. When the server recorded a per-version
`change_summary`, expanding the entry SHALL list the added paths as `+ <path>`, the
removed paths as `− <path>`, and the updated paths as `<path>: <from> → <to>` (values
rendered compactly and truncated), and SHALL surface merge provenance (`source`,
`strategy`, `enriched_keys`) without breaking the layout. When no `change_summary` was
recorded for a version, the expanded entry SHALL derive a diff from the preceding
version — the `status` and `assignee` as `<from> → <to>`, and each changed top-level
property path (scalar values as `<path>: <from> → <to>`, nested/complex values as
`<path>: changed`, added/removed paths as `+`/`−`). When neither a summary nor a
derivable difference exists, the expanded entry SHALL show a muted note. Unexpected or
unknown `change_summary` shapes SHALL be tolerated — the renderer SHALL NOT panic or
blank the entry.

#### Scenario: Expanding an entry reveals its recorded change list

- **WHEN** a version carries a recorded `change_summary` and the user expands its history entry
- **THEN** the entry lists the added paths as `+ <path>`, the removed paths as `− <path>`, and the updated paths as `<path>: <from> → <to>`
- **AND** merge provenance (`source`, `strategy`, `enriched_keys`) renders without breaking the layout

#### Scenario: A version with no recorded summary derives its changes

- **WHEN** a version carries no `change_summary` (for example a board status transition) and the user expands its entry
- **THEN** the entry reports the status change as `status: <from> → <to>`, the assignee change as `assignee: <from> → <to>`, and each changed top-level property path (scalar values as `<path>: <from> → <to>`, nested/complex values as `<path>: changed`)

#### Scenario: Nothing to show

- **WHEN** a version has neither a recorded summary nor any derivable difference from its predecessor
- **THEN** the expanded entry shows a muted note such as "No recorded changes for this version."

#### Scenario: Entries are collapsed by default and keyboard operable

- **WHEN** the history timeline is rendered
- **THEN** each entry is collapsed by default and expands on activation
- **AND** the entry's disclosure control is operable by keyboard (Enter/Space)

#### Scenario: Unexpected summary shapes are tolerated

- **WHEN** a version's `change_summary` carries keys or value types the renderer does not expect
- **THEN** the entry renders without blanking or panicking, falling back to the derived diff (or the no-changes note) when the summary yields no change lines
