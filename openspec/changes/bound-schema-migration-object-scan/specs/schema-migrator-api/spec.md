## ADDED Requirements

### Requirement: Batch migration operations bound their object scan to archive-carrying objects
The migrate and rollback operations SHALL restrict their object scan to objects that actually carry a migration archive, rather than fetching every object in the project. The scan SHALL still visit every object that matches the migration or rollback target.

#### Scenario: Rollback does not full-scan an archive-free project
- **GIVEN** a project whose objects carry no `migration_archive` entries
- **WHEN** a rollback executes
- **THEN** the object scan SHALL NOT fetch every object in the project
- **THEN** the rollback SHALL report zero objects restored without a full-table scan

#### Scenario: Migrate scans only affected archived objects
- **WHEN** a forward migration executes and reads object archives
- **THEN** the scan SHALL be bounded to objects with at least one `migration_archive` entry
- **THEN** every affected object SHALL still be migrated and counted

#### Scenario: Completeness is preserved
- **GIVEN** a project with more archive-carrying objects than one list page
- **WHEN** a migrate or rollback executes with the archive predicate
- **THEN** every archive-carrying object SHALL be visited across pages
