## ADDED Requirements

### Requirement: Clone restore skips unresolvable global schema links
Clone restore SHALL drop (not insert) any `kb.project_schemas` or `kb.project_edge_schema_registry` row whose `schema_id` cannot be resolved through the clone ID-remap table. These unresolvable values reference deployment-global builtin `kb.graph_schemas` rows that are absent from a project-scoped archive; the target deployment's own builtin provisioning already creates the correct link.

#### Scenario: Clone into a differently-bootstrapped deployment
- **GIVEN** a backup archive whose `kb.project_schemas` rows reference the source deployment's global builtin schema UUID
- **WHEN** the archive is cloned into a target deployment whose own builtin schema UUID differs
- **THEN** the clone SHALL NOT fail on the `project_schemas_schema_id_fkey` constraint
- **THEN** the unresolvable `schema_id` link SHALL be skipped, not inserted
- **THEN** project-owned schema links SHALL still be remapped to the target's new UUIDs
- **THEN** the target deployment's own builtin link SHALL remain present

### Requirement: Clone foreign keys resolve through a declared per-column policy
Clone restore SHALL resolve foreign-key columns through a declared per-column policy rather than an ad-hoc per-table nulling list. A column with no declared policy SHALL be rewritten when its value is in the remap table and left unchanged otherwise; a declared policy SHALL be applied only to non-empty values that are not in the remap table.

#### Scenario: Policy actions
- **GIVEN** a clone foreign-key column with a declared policy
- **WHEN** the column value is present in the remap table
- **THEN** the value SHALL be rewritten to the remapped UUID and the policy SHALL NOT apply
- **WHEN** the value is absent from the remap table and the policy is `null`
- **THEN** the column SHALL be set to NULL
- **WHEN** the value is absent and the policy is `skip`
- **THEN** the row SHALL be dropped
- **WHEN** the value is absent and the policy is `fail`
- **THEN** the restore SHALL fail with an error naming the table, column, and value
