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

### Requirement: Blueprint relationship-type expansion is bounded per pack

Because plural declarations expand as a cross-product, the total number of
singular relationship-type definitions produced for a single pack SHALL be
bounded. The per-pack limit SHALL be 10 000 expanded definitions. When a pack's
expansion would exceed the limit, apply SHALL fail with `400 bad_request` whose
message names the offending definition, and SHALL reject the pack before
allocating the expanded output, so an over-budget pack cannot cause a
memory-exhausting allocation. A pack expanding to exactly the limit SHALL be
accepted; only totals strictly greater than the limit are rejected. The stored
blueprint manifest is unaffected — the bound applies only to the singular
payload built for the schemas service.

#### Scenario: Over-budget expansion is rejected before allocation

- **WHEN** a blueprint pack's relationship types would expand to more than
  10 000 singular definitions — whether one definition with 10 001 source types
  or several individually-modest definitions whose cumulative total exceeds
  10 000
- **THEN** applying the blueprint fails with `400 bad_request`, the error message
  names the definition whose expansion crossed the limit, and no expanded
  definition list is materialized

#### Scenario: Expansion at the limit is accepted

- **WHEN** a blueprint pack's relationship types expand to exactly 10 000
  singular definitions
- **THEN** applying the blueprint succeeds and registers all 10 000 relationship
  schemas
