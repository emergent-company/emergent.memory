## ADDED Requirements

### Requirement: entity-query enforces a type's declared scope key

When the queried type declares a `scopeKey` (in its registry JSON schema),
`entity-query` SHALL reject a `filters` entry on a property that is neither the
declared scope property nor the declared identity property unless the scope
property is also present in `filters` or an explicit `key_prefix` is supplied.
The rejection SHALL be fail-closed — no query is executed — and SHALL carry an
actionable, caller-visible error naming the offending filter and the required
scope key: `query entities: filter "<filter>" requires scope key "<scope>"
(declared scope for type <Type>)`. When the scope key is supplied, results SHALL
be scoped to that document by combining the scope predicate with the other
filters. A type with no declared scope key SHALL keep the previous behaviour
exactly.

#### Scenario: Non-identity filter without the scope key is rejected

- **WHEN** a type `LegalParagraph` declares scope key `law_ref_id` and a client
  calls `entity-query` with `filters:{chapter_id:"kapittel-2"}` and no
  `law_ref_id` and no `key_prefix`
- **THEN** the call fails with an error naming `chapter_id` and the required
  scope key `law_ref_id`, and no entities are returned

#### Scenario: Scope key scopes the filter to one document

- **WHEN** the same call includes `law_ref_id` in `filters`
- **THEN** only entities whose `law_ref_id` matches are returned (the scope
  predicate is ANDed with the other filters)

#### Scenario: key_prefix still satisfies the requirement

- **WHEN** a client passes `key_prefix` instead of the scope property
- **THEN** the call is not rejected and results are scoped to that key prefix

#### Scenario: Identity property filter is allowed alone

- **WHEN** a client filters only on the declared `identityProperty`
- **THEN** the call is not rejected

#### Scenario: Type without a declaration is unchanged

- **WHEN** a type has no `scopeKey` declaration
- **THEN** a bare non-unique property filter behaves exactly as before (no
  rejection)
