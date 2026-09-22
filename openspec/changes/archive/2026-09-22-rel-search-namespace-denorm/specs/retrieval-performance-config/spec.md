## ADDED Requirements

### Requirement: Relationship namespace predicate stays on the embedding table
Relationship vector search SHALL apply the namespace predicate on `kb.graph_relationships.namespace`, denormalised from the source object, rather than on the joined `kb.graph_objects` row. `namespace` SHALL be populated on every relationship insert and inherited across relationship versions (tombstone, restore, patch, fast-forward, similarity-merge, branch copy) so it mirrors the source object's namespace. The predicate MUST remain absent when namespace is unset (`namespace: "all"` or no namespace requested), preserving the unfiltered path.

#### Scenario: Namespace-scoped search filters on the relationship row
- **WHEN** a relationship vector search runs with an explicit namespace
- **THEN** the query MUST filter on `kb.graph_relationships.namespace = ?` and MUST NOT filter on the joined `kb.graph_objects.namespace`

#### Scenario: Unset namespace leaves the query unfiltered
- **WHEN** a relationship vector search runs with no namespace (unset or `"all"`)
- **THEN** the query MUST contain no namespace predicate

#### Scenario: Namespace survives relationship versioning
- **WHEN** a version of an existing relationship is created (patch, restore, tombstone, merge, or branch copy)
- **THEN** the new row MUST carry the previous HEAD's namespace

#### Scenario: Namespace is backfilled for existing relationships
- **WHEN** migration `00172` is applied to a database with existing relationships
- **THEN** every relationship with a resolvable source object MUST be backfilled with that object's namespace

### Requirement: Relationship namespace denormalisation is reversible
The namespace denormalisation migration SHALL be reversible: its Down migration MUST drop the index and the `namespace` column without affecting other relationship data.

#### Scenario: Rolling back the migration
- **WHEN** migration `00172` is rolled back
- **THEN** `kb.graph_relationships.namespace` and `idx_graph_relationships_namespace` MUST no longer exist
