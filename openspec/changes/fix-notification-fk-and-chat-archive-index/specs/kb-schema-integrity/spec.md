# kb-schema-integrity Specification

## Purpose

Guarantees about the `kb` schema's referential integrity: that a single,
unambiguous delete action governs each foreign key, and that the indexes behind
hot default query paths match the queries they serve.

## ADDED Requirements

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
