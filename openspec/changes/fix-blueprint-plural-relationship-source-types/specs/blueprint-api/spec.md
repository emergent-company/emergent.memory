## ADDED Requirements

### Requirement: Blueprint relationship types support plural source and target types

A blueprint pack's relationship type SHALL be able to declare `sourceTypes`
and/or `targetTypes` as arrays in addition to the singular `sourceType` and
`targetType`, matching the CLI loader. On apply, each plural declaration SHALL
be expanded into the cross-product of source types × target types and posted to
the schemas service as singular `sourceType`/`targetType` entries, so a
relationship declared with `sourceTypes: [Alpha, Beta]` and `targetType: Gamma`
registers two relationship schemas (Alpha→Gamma and Beta→Gamma).

#### Scenario: Plural source types expand to singular schemas

- **WHEN** a blueprint pack declares a relationship type with `sourceTypes:
  [Alpha, Beta]` and `targetType: Gamma`, and the pack defines the object types
  Alpha, Beta, and Gamma
- **THEN** applying the blueprint registers TWO relationship schemas for that
  relationship name — `Alpha→Gamma` and `Beta→Gamma` — each carrying a non-empty
  singular `sourceType` and `targetType`

#### Scenario: Singular declarations are unchanged

- **WHEN** a blueprint pack declares a relationship type with only the singular
  `sourceType` and `targetType`
- **THEN** applying the blueprint registers exactly one relationship schema,
  unchanged

#### Scenario: Plural declarations round-trip in the stored manifest

- **WHEN** a blueprint imported from a GitHub URL declares plural
  `sourceTypes`/`targetTypes`
- **THEN** the stored blueprint manifest preserves the plural fields, and the
  import does not fail with a missing-`sourceType` schema error
