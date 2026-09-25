## ADDED Requirements

### Requirement: Backup export nulls references to soft-deleted objects
When a backup is exported without deleted rows (`IncludeDeleted == false`), the exporter SHALL emit `chat_conversations.object_id` as NULL when its referenced `kb.graph_objects` row is missing or soft-deleted. When deleted rows are included (`IncludeDeleted == true`), the exporter SHALL emit `object_id` unchanged.

#### Scenario: Excluding deleted objects nulls the dangling pointer
- **GIVEN** a chat conversation whose `object_id` references a soft-deleted graph object
- **WHEN** the project is exported with `IncludeDeleted == false`
- **THEN** the exported conversation SHALL have `object_id` set to NULL
- **THEN** the conversation itself SHALL remain in the archive

#### Scenario: Including deleted objects preserves the pointer
- **GIVEN** a chat conversation whose `object_id` references a soft-deleted graph object
- **WHEN** the project is exported with `IncludeDeleted == true`
- **THEN** the exported conversation SHALL retain its original `object_id`
- **THEN** the referenced graph object SHALL also be present in the archive

#### Scenario: Live object references are preserved
- **GIVEN** a chat conversation whose `object_id` references a non-deleted graph object
- **WHEN** the project is exported with `IncludeDeleted == false`
- **THEN** the exported conversation SHALL retain its original `object_id`
