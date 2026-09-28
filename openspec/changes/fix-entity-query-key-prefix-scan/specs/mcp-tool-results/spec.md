## ADDED Requirements

### Requirement: entity-query key-prefix scope is index-backed

`entity-query` SHALL compile its optional `key_prefix` scope into an indexable
bytewise range on the canonical key rather than a `starts_with(go.key, ?)`
function call, so the planner can seek the partial bytewise index
`idx_graph_objects_project_type_key_c` instead of scanning the whole
`kb.graph_objects` heap. The range SHALL be `key COLLATE "C" >= prefix AND key
COLLATE "C" < upper`, where `upper` is the prefix with its final byte
incremented (exact under bytewise comparison and correct for prefixes ending in
a separator). Bytewise comparison is required because the database default
collation is not bytewise and would over-exclude separator-terminated prefixes.
The entities returned and all other scoping/filter/`ids` semantics SHALL be
unchanged.

#### Scenario: Key-prefix scope does not scan the whole table

- **WHEN** a client calls `entity-query` with a `key_prefix` and the bytewise index exists
- **THEN** the query is served by an `Index Scan using idx_graph_objects_project_type_key_c`
- **AND** the plan contains no sequential scan of `kb.graph_objects`

#### Scenario: Separator-terminated prefix returns the same entities

- **WHEN** a client passes a `key_prefix` ending in a separator (e.g. `lov/1997-06-13-44#`)
- **THEN** exactly the entities whose key starts with that prefix are returned
- **AND** a trailing-`0xFF` prefix with no representable successor still falls back to `starts_with` and returns the same entities

## MODIFIED Requirements

### Requirement: entity-query calls are bounded

`entity-query` SHALL enforce a configurable hard per-call deadline created at the
tool entry, so it covers every path of a call — branch resolution, the `ids[]`
fast-path, the type/pagination queries, and relationship enrichment — and, when
`field_strategy="full"` is requested, SHALL cap the effective `limit` to a
configurable maximum and surface the cap in a warning. A call that exceeds the
deadline SHALL return an explicit timeout error. The `limit` input schema
description SHALL state the effective full-strategy cap, so the advertised
`Maximum: 200` does not contradict the runtime clamp. A relationship-enrichment
failure (including deadline exhaustion) on an `include_relationships=true` call
SHALL be surfaced as an error rather than silently returning entities with their
relationships omitted.

#### Scenario: Full-strategy limit is capped

- **WHEN** a client calls `entity-query` with `field_strategy: "full"` and a `limit`
  above the configured cap
- **THEN** the effective limit is the cap, at most that many entities are returned,
  and the response warning reports the cap

#### Scenario: Full-strategy cap is stated in the input schema

- **WHEN** a client reads the `entity-query` `limit` input schema
- **THEN** its description names the effective full-strategy cap

#### Scenario: Call deadline is enforced

- **WHEN** an entity-query call cannot complete within the configured deadline
- **THEN** it returns a timeout error naming the deadline instead of blocking indefinitely

#### Scenario: Deadline covers the ids fast-path

- **WHEN** an `entity-query` call with `ids` cannot complete within the configured deadline
- **THEN** it returns the same timeout error (the deadline is created at tool entry, not after the ids path)

#### Scenario: Relationship enrichment failure is surfaced

- **WHEN** an `entity-query` call with `include_relationships=true` cannot complete
  relationship enrichment because the call deadline was exhausted
- **THEN** it returns the explicit timeout error
- **AND** it does not return `ok:true` with entities missing their relationships
