## ADDED Requirements

### Requirement: entity-query property filters use indexed JSONB containment

`entity-query` SHALL compile its `filters` map into a single bound JSONB
containment predicate (`properties @> $jsonb`) rather than per-key string
interpolation, so a composite GIN index on `(project_id, type, properties
jsonb_path_ops)` over head-main rows can serve the predicate. Property filter
values SHALL NOT be interpolated into SQL text. A filter key that is not a valid
property key SHALL be dropped.

#### Scenario: Filter values are bound, not interpolated

- **WHEN** a client calls `entity-query` with a `filters` value containing a quote
- **THEN** the query still executes as a bound containment predicate and no SQL is injected

#### Scenario: Filtered pagination does not scan the whole table

- **WHEN** a client calls `entity-query` with a type and a property filter and the
  composite GIN index exists
- **THEN** the pagination COUNT and page query are served by an index (GIN) path
  rather than a full sequential scan of `kb.graph_objects`

#### Scenario: Invalid filter key is dropped

- **WHEN** a `filters` key is not a valid property key
- **THEN** that key is ignored and the remaining filters still apply

#### Scenario: Filter values are JSON-type exact

- **WHEN** a `filters` value is compared against a property that exists as a JSON
  string on one entity and a JSON number on another
- **THEN** only the entity whose JSON type and value match is returned, because
  containment is type-exact (the previous `properties->>'k' = 'v'` text comparison
  matched both)

### Requirement: entity-query calls are bounded

`entity-query` SHALL enforce a configurable hard per-call deadline and, when
`field_strategy="full"` is requested, SHALL cap the effective `limit` to a
configurable maximum and surface the cap in a warning. A call that exceeds the
deadline SHALL return an explicit timeout error.

#### Scenario: Full-strategy limit is capped

- **WHEN** a client calls `entity-query` with `field_strategy: "full"` and a `limit`
  above the configured cap
- **THEN** the effective limit is the cap, at most that many entities are returned,
  and the response warning reports the cap

#### Scenario: Call deadline is enforced

- **WHEN** an entity-query call cannot complete within the configured deadline
- **THEN** it returns a timeout error naming the deadline instead of blocking indefinitely

### Requirement: entity-query supports key-prefix identity scoping

`entity-query` SHALL accept an optional `key_prefix` input that restricts results
to entities whose canonical key starts with the given prefix. The prefix scope
SHALL combine (AND) with `type_name` and `filters`, and SHALL NOT change the
meaning of a filter used on its own.

#### Scenario: Key prefix scopes a non-unique property filter

- **WHEN** a client calls `entity-query` with a property filter that is not unique
  to one entity and a `key_prefix` identifying one parent identity
- **THEN** only entities whose key begins with that prefix are returned

#### Scenario: Absent key prefix leaves filter semantics unchanged

- **WHEN** a client calls `entity-query` with `filters` and no `key_prefix`
- **THEN** results are the same as before key-prefix scoping existed
