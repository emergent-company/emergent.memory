## ADDED Requirements

### Requirement: GitHub import records source provenance

A blueprint imported from a GitHub repository SHALL persist its source
provenance on the blueprint record: the source kind, a normalized source URL,
and the source ref. The source kind SHALL be `github`; the source URL SHALL be
`https://github.com/<org>/<repo>` with no ref suffix and no `.git`; the source
ref SHALL be the ref the archive was fetched at, with the `HEAD` sentinel
normalized to the empty string to mean the repository default branch.

#### Scenario: Import records kind, URL, and ref

- **WHEN** a caller imports a blueprint from
  `https://github.com/<org>/<repo>` with an explicit ref (via the `ref` field,
  a `#<ref>` fragment, or a `/tree/<ref>` suffix)
- **THEN** the created blueprint carries `sourceKind` `github`, `sourceUrl`
  `https://github.com/<org>/<repo>`, and `sourceRef` equal to that ref

#### Scenario: Default ref normalized to empty

- **WHEN** a caller imports a blueprint from `https://github.com/<org>/<repo>`
  without specifying a ref
- **THEN** the created blueprint carries `sourceRef` equal to the empty string
  (the repository default branch), not `HEAD`

#### Scenario: `.git` suffix stripped from the source URL

- **WHEN** a caller imports a blueprint from
  `https://github.com/<org>/<repo>.git`
- **THEN** the recorded `sourceUrl` is `https://github.com/<org>/<repo>`
  (no `.git`)

#### Scenario: Non-imported blueprints carry no provenance

- **WHEN** a blueprint is created through the ordinary create path (not a GitHub
  import)
- **THEN** its `sourceKind`, `sourceUrl`, and `sourceRef` are empty

### Requirement: Created blueprints carry a canonical manifest checksum

`CreateBlueprint` SHALL compute and persist the canonical manifest checksum at
create time — using the same sha256-over-JSON algorithm `PublishBlueprint` uses
— so a draft carries a checksum for drift comparison without requiring a
publish step.

#### Scenario: Draft carries a non-empty checksum

- **WHEN** a blueprint draft is created
- **THEN** the created blueprint's checksum is a non-empty sha256 hash of its
  JSON manifest

### Requirement: Source provenance survives publish and apply

Publishing a blueprint SHALL NOT clear its source provenance, and applying it
SHALL NOT alter the provenance fields; the provenance recorded at import time
SHALL be preserved for the life of the blueprint record.

#### Scenario: Publish preserves provenance

- **WHEN** a GitHub-imported blueprint is published
- **THEN** its `sourceKind`, `sourceUrl`, and `sourceRef` are unchanged

#### Scenario: Apply preserves provenance

- **WHEN** a GitHub-imported blueprint is applied to a project
- **THEN** its `sourceKind`, `sourceUrl`, and `sourceRef` are unchanged
