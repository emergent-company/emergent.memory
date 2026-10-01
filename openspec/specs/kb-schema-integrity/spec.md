# kb-schema-integrity Specification

## Purpose
Guarantees about the `kb` schema's referential integrity: that a single,
unambiguous delete action governs each foreign key, and that the indexes behind
hot default query paths match the queries they serve.

## Requirements

### Requirement: Notification project references cascade on project deletion

`kb.notifications.project_id` SHALL be guarded by exactly one foreign key to
`kb.projects(id)`, with `ON DELETE CASCADE`. Deleting a project SHALL delete every
notification whose `project_id` is that project. Project-scoped notifications
SHALL NOT be retained with a cleared (NULL) project reference, because every
project-scoped read path is keyed on `project_id`. Account-scope notifications,
which carry no project, SHALL be unaffected.

A migration SHALL assert the constraint set (exactly one foreign key from
`kb.notifications` to `kb.projects(id)`) and fail if it does not hold, so the
conflicting-constraint state cannot silently return.

#### Scenario: Project deletion removes its project-scoped notifications

- **WHEN** a project that has project-scoped notifications is deleted
- **THEN** those notifications are deleted, not left behind with a NULL `project_id`

#### Scenario: Account-scope notifications survive project deletion

- **WHEN** a project is deleted
- **THEN** account-scope notifications (which carry a NULL `project_id`) remain

#### Scenario: A single cascade constraint is asserted

- **WHEN** the forward migration runs against a schema that has two foreign keys
  on `kb.notifications.project_id`
- **THEN** it SHALL drop the extra constraint, keep the CASCADE constraint, and
  raise an error if exactly one FK to `kb.projects(id)` is not present afterwards

### Requirement: Default chat conversation list is index-supported

The default chat conversation list query — project scope, archived excluded,
most-recently-updated first — SHALL be supported by a partial B-tree index on
`kb.chat_conversations (project_id, updated_at DESC) WHERE is_archived = false`.
The `is_archived` predicate SHALL be the index's partial condition, not a key
column, so archived rows are excluded from the index entirely. The optional
owner/shared filter SHALL NOT be part of the index key, because it is absent from
the shared-list path.

#### Scenario: The default list shape has a matching index

- **WHEN** the default list query (`project_id = ?`, `is_archived = false`,
  `ORDER BY updated_at DESC`) is planned
- **THEN** the partial index on `(project_id, updated_at DESC) WHERE is_archived
  = false` is a candidate and can supply both the filter and the ordering without
  an explicit sort

#### Scenario: The include-archived path is unaffected

- **WHEN** archived conversations are included (no `is_archived = false` predicate)
- **THEN** the partial index is not a candidate, and the query still returns the
  archived and active conversations correctly
