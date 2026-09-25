## ADDED Requirements

### Requirement: Rollback bounds its object scan to archive-carrying objects
The rollback operation SHALL restrict its object scan to objects that carry at least one `migration_archive` entry (`migration_archive <> '[]'::jsonb`), rather than fetching every object in the project. The scan SHALL still visit every archive-carrying object, including across multiple list pages.

#### Scenario: Rollback does not full-scan an archive-free project
- **GIVEN** a project whose objects carry no `migration_archive` entries
- **WHEN** a rollback executes
- **THEN** the object scan SHALL NOT fetch every object in the project
- **THEN** the rollback SHALL report zero objects restored

#### Scenario: Rollback restores only matching archived objects
- **GIVEN** a project containing one object with an archive entry targeting the rollback version and one archive-free object
- **WHEN** a rollback executes
- **THEN** the archive-carrying object SHALL be restored
- **THEN** the archive-free object SHALL NOT be restored

#### Scenario: Rollback completeness is preserved across pages
- **GIVEN** a project with more archive-carrying objects than one list page
- **WHEN** a rollback executes with the archive predicate
- **THEN** every archive-carrying object SHALL be visited across pages

### Requirement: Forward migration still visits archive-free objects
The forward migration operation SHALL NOT filter its object scan by the presence of a `migration_archive` entry. Objects with an empty archive SHALL still be migrated and counted, because a first-ever migration has zero archived objects and objects created after a prior migration may also have none.

#### Scenario: Migrate counts objects with an empty archive
- **GIVEN** a project whose objects carry no `migration_archive` entries
- **WHEN** a forward migration executes
- **THEN** every object SHALL be migrated and counted

### Requirement: Migrate and rollback stream results page-by-page
Migrate and rollback SHALL iterate the affected object set one list page at a time rather than materialising the full result set in memory. A caller-supplied page callback SHALL be invoked once per page, and the iteration SHALL stop as soon as the callback returns an error without fetching further pages.

#### Scenario: Full set is not materialised
- **GIVEN** a result set larger than one list page
- **WHEN** a migrate or rollback executes
- **THEN** objects SHALL be processed one page at a time
- **THEN** the whole result set SHALL NOT be accumulated in memory before processing

### Requirement: Synchronous request path is bounded by a hard cap
The synchronous migrate and rollback request path SHALL be bounded by a configurable hard cap on the number of objects scanned. When a scan would exceed the cap, the operation SHALL abort with a clear 4xx error stating the cap and that the operation must be narrowed.

#### Scenario: Hard cap aborts with a clear error
- **GIVEN** a configured hard cap of N scanned objects
- **WHEN** a migrate or rollback scans more than N objects
- **THEN** the operation SHALL abort with a 4xx error naming the cap

#### Scenario: Rollback hard-cap abort is atomic
- **GIVEN** a rollback that exceeds the hard cap mid-scan
- **WHEN** the rollback aborts
- **THEN** the single transaction SHALL roll back so no objects are restored and no registry changes are written

#### Scenario: Execute hard-cap abort is not transactional
- **GIVEN** a forward migration that exceeds the hard cap mid-scan
- **WHEN** the migration aborts
- **THEN** the current type MAY be left partially migrated
- **THEN** the `kb.schema_migration_runs` run row SHALL NOT be written

### Requirement: Rollback rejects max_objects combined with restore_type_registry
The rollback operation SHALL reject a request that combines `max_objects` with `restore_type_registry`, because the registry restore is all-or-nothing and cannot be skipped when the `max_objects` cap is reached mid-scan.

#### Scenario: Combined request fails loudly
- **GIVEN** a rollback request with `restore_type_registry: true` and `max_objects: N`
- **WHEN** the rollback executes
- **THEN** the request SHALL fail with a 4xx bad-request error
- **THEN** no objects SHALL be restored and no registry changes SHALL be written
