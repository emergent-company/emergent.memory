## ADDED Requirements

### Requirement: Cross-pack relationship endpoints

The offline blueprint validation SHALL validate each relationship type's
`sourceType`/`targetType` endpoints against the union of the object types
declared by all packs in the blueprint, not only the pack that declares the
relationship. An endpoint naming an object type owned by a sibling pack in the
same blueprint SHALL be accepted. The install preflight SHALL additionally
accept an endpoint that names an object type already present in the project
(an installed pack's type or a built-in type), resolving the project's types
with a best-effort read-only call issued only when an endpoint is not defined by
any pack in the blueprint. An endpoint that matches no object type in any pack
of the blueprint and none already in the project SHALL be reported as an error
naming the relationship and the unmatched type. The offline `validate` command
SHALL NOT make API calls and therefore validates against the blueprint's packs
only.

#### Scenario: Cross-pack target accepted

- **WHEN** a pack declares a relationship whose `targetType` is an object type defined by a different pack in the same blueprint
- **THEN** validation reports no error for that endpoint

#### Scenario: Cross-pack source accepted

- **WHEN** a pack declares a relationship whose `sourceType` is an object type defined by a different pack in the same blueprint
- **THEN** validation reports no error for that endpoint

#### Scenario: Project-owned endpoint accepted at install

- **WHEN** the install preflight is given the project's existing object type names and an endpoint names one of them even though no pack in the blueprint defines it
- **THEN** validation reports no error for that endpoint
- **AND** the project's types are consulted with a read-only call only when an endpoint is not defined by any pack in the blueprint

#### Scenario: Unknown endpoint rejected

- **WHEN** a relationship's `sourceType` or `targetType` matches no object type in any pack of the blueprint and none already in the project
- **THEN** the install preflight reports an error naming the relationship and the unmatched type
- **AND** nothing is applied
