## ADDED Requirements

### Requirement: CLI accepts --stats flag

The `emergent projects list` command SHALL accept an optional `--stats` flag that enables display of project statistics.

#### Scenario: List projects without stats flag

- **WHEN** user runs `emergent projects list` without the `--stats` flag
- **THEN** system displays project list with basic information only (name, ID, KB purpose)
- **AND** system does NOT display statistics

#### Scenario: List projects with stats flag

- **WHEN** user runs `emergent projects list --stats`
- **THEN** system displays project list with basic information AND statistics for each project

### Requirement: Display document count

When `--stats` flag is enabled, the system SHALL display the count of documents for each project.

#### Scenario: Project with documents

- **WHEN** user runs `emergent projects list --stats` for a project with 42 documents
- **THEN** system displays "Documents: 42" in the stats section

#### Scenario: Project with no documents

- **WHEN** user runs `emergent projects list --stats` for a project with 0 documents
- **THEN** system displays "Documents: 0" in the stats section

### Requirement: Display object count

When `--stats` flag is enabled, the system SHALL display the count of graph objects (excluding deleted objects) for each project.

#### Scenario: Project with objects

- **WHEN** user runs `emergent projects list --stats` for a project with 156 graph objects
- **THEN** system displays "Objects: 156" in the stats section

#### Scenario: Count excludes deleted objects

- **WHEN** user runs `emergent projects list --stats` for a project with 100 total objects, 10 of which have deleted_at set
- **THEN** system displays "Objects: 90" in the stats section

### Requirement: Display relationship count

When `--stats` flag is enabled, the system SHALL display the count of graph relationships for each project.

#### Scenario: Project with relationships

- **WHEN** user runs `emergent projects list --stats` for a project with 89 relationships
- **THEN** system displays "Relationships: 89" in the stats section

### Requirement: Display extraction job counts

When `--stats` flag is enabled, the system SHALL display extraction job counts: total jobs, running jobs, and queued (pending) jobs.

#### Scenario: Project with mixed job statuses

- **WHEN** user runs `emergent projects list --stats` for a project with 10 total jobs, 1 running, 2 pending
- **THEN** system displays "Extraction jobs: 10 total, 1 running, 2 queued" in the stats section

#### Scenario: Project with no active jobs

- **WHEN** user runs `emergent projects list --stats` for a project with 5 total jobs, 0 running, 0 pending
- **THEN** system displays "Extraction jobs: 5 total" in the stats section (omitting zero counts for running/queued)

#### Scenario: Project with no jobs

- **WHEN** user runs `emergent projects list --stats` for a project with 0 jobs
- **THEN** system displays "Extraction jobs: 0 total" in the stats section

### Requirement: Display installed template packs

When `--stats` flag is enabled, the system SHALL display installed template packs with their names, versions, object types, and relationship types.

#### Scenario: Project with multiple template packs

- **WHEN** user runs `emergent projects list --stats` for a project with 2 active template packs
- **THEN** system displays "Template packs:" header under stats
- **AND** each template pack shows on indented line with format "- name@version"
- **AND** below each pack, object types are listed as "Objects: Type1, Type2, Type3"
- **AND** below each pack, relationship types are listed as "Relationships: REL1, REL2"

#### Scenario: Project with one template pack

- **WHEN** user runs `emergent projects list --stats` for a project with 1 active template pack defining 3 object types and 2 relationship types
- **THEN** system displays "Template packs:" followed by pack name@version
- **AND** system displays object type names comma-separated
- **AND** system displays relationship type names comma-separated

#### Scenario: Project with no template packs

- **WHEN** user runs `emergent projects list --stats` for a project with no active template packs
- **THEN** system displays "Template packs: none" in the stats section

#### Scenario: Template pack with no relationships

- **WHEN** user runs `emergent projects list --stats` for a project with a template pack that defines only object types (no relationships)
- **THEN** system displays the pack with its object types
- **AND** system displays "Relationships: none" or omits the Relationships line

### Requirement: Stats are indented under project

When `--stats` flag is enabled, the system SHALL display statistics indented under the corresponding project with a "Stats:" label.

#### Scenario: Stats formatting

- **WHEN** user runs `emergent projects list --stats`
- **THEN** system displays each project's stats indented with bullet points (•) under a "Stats:" header
- **AND** stats appear after project name, ID, and KB purpose (if present)

### Requirement: Backend API supports stats query parameter

The backend API endpoint `GET /api/projects` SHALL accept an optional `include_stats` query parameter.

#### Scenario: Request without stats parameter

- **WHEN** client calls `GET /api/projects` without `include_stats` parameter
- **THEN** response includes project data WITHOUT stats field
- **AND** response matches existing ProjectDTO schema

#### Scenario: Request with stats parameter false

- **WHEN** client calls `GET /api/projects?include_stats=false`
- **THEN** response includes project data WITHOUT stats field

#### Scenario: Request with stats parameter true

- **WHEN** client calls `GET /api/projects?include_stats=true`
- **THEN** response includes project data WITH stats field populated for each project
- **AND** stats field contains documentCount, objectCount, relationshipCount, totalJobs, runningJobs, queuedJobs, templatePacks
- **AND** each template pack in templatePacks includes name, version, objectTypes array, relationshipTypes array

### Requirement: Stats calculations are accurate

The backend SHALL calculate statistics using real-time database queries filtered by project_id.

#### Scenario: Document count accuracy

- **WHEN** backend calculates stats for a project
- **THEN** documentCount equals COUNT(\*) from kb.documents WHERE project_id = <project_id>

#### Scenario: Object count accuracy

- **WHEN** backend calculates stats for a project
- **THEN** objectCount equals COUNT(\*) from kb.graph_objects WHERE project_id = <project_id> AND deleted_at IS NULL

#### Scenario: Relationship count accuracy

- **WHEN** backend calculates stats for a project
- **THEN** relationshipCount equals COUNT(\*) from kb.graph_relationships WHERE project_id = <project_id>

#### Scenario: Total jobs accuracy

- **WHEN** backend calculates stats for a project
- **THEN** totalJobs equals COUNT(\*) from kb.object_extraction_jobs WHERE project_id = <project_id>

#### Scenario: Running jobs accuracy

- **WHEN** backend calculates stats for a project
- **THEN** runningJobs equals COUNT(\*) from kb.object_extraction_jobs WHERE project_id = <project_id> AND status = 'running'

#### Scenario: Queued jobs accuracy

- **WHEN** backend calculates stats for a project
- **THEN** queuedJobs equals COUNT(\*) from kb.object_extraction_jobs WHERE project_id = <project_id> AND status = 'pending'

#### Scenario: Template packs accuracy

- **WHEN** backend calculates stats for a project
- **THEN** templatePacks contains all records from kb.project_template_packs joined with kb.graph_template_packs WHERE project_id = <project_id> AND active = true
- **AND** each entry includes name and version from graph_template_packs
- **AND** each entry includes objectTypes array extracted from keys of object_type_schemas JSONB field
- **AND** each entry includes relationshipTypes array extracted from keys of relationship_type_schemas JSONB field

### Requirement: Stats respect existing authorization

The backend SHALL only return statistics for projects the authenticated user has access to.

#### Scenario: User can only see their projects' stats

- **WHEN** backend calculates stats with `include_stats=true`
- **THEN** stats are calculated only for projects where user has project membership
- **AND** authorization logic matches existing `GET /api/projects` behavior

### Requirement: SDK client supports stats option

The Go SDK client `pkg/sdk/projects.Client` SHALL provide an option to request stats when listing projects.

#### Scenario: SDK list without stats

- **WHEN** SDK client calls `List()` method without stats option
- **THEN** SDK sends request to `GET /api/projects` without include_stats parameter

#### Scenario: SDK list with stats

- **WHEN** SDK client calls `List()` method with `IncludeStats: true` option
- **THEN** SDK sends request to `GET /api/projects?include_stats=true`
- **AND** SDK parses response including optional Stats field

### Requirement: CLI get command accepts --stats flag

The `emergent projects get` command SHALL accept an optional `--stats` flag that enables display of project statistics for a single project.

#### Scenario: Get project without stats flag

- **WHEN** user runs `emergent projects get <name-or-id>` without the `--stats` flag
- **THEN** system displays project details with basic information only (name, ID, org ID, KB purpose)
- **AND** system does NOT display statistics

#### Scenario: Get project with stats flag

- **WHEN** user runs `emergent projects get <name-or-id> --stats`
- **THEN** system displays project details with basic information AND statistics

### Requirement: Get command displays stats in dedicated section

When `--stats` flag is enabled on `projects get` command, the system SHALL display statistics in a dedicated "Stats:" section below basic project info.

#### Scenario: Stats section formatting

- **WHEN** user runs `emergent projects get <name-or-id> --stats`
- **THEN** system displays basic project info first (Project, Org ID, KB Purpose)
- **AND** system displays blank line followed by "Stats:" header
- **AND** system displays stats indented with bullet points (•)

### Requirement: Get command has aligned label formatting

The `emergent projects get` command SHALL display labels and values with consistent alignment for readability.

#### Scenario: Label alignment without stats

- **WHEN** user runs `emergent projects get <name-or-id>`
- **THEN** labels (Project, Org ID, KB Purpose) are left-aligned with consistent width
- **AND** values start at the same column position for all fields
- **AND** format is "Label: <spaces> Value"

#### Scenario: Multiline values are properly indented

- **WHEN** user runs `emergent projects get <name-or-id>` for a project with long KB Purpose text
- **THEN** first line of value appears after label
- **AND** subsequent lines of value align with the first line (not with label)

### Requirement: Backend GET endpoint supports stats query parameter

The backend API endpoint `GET /api/projects/:id` SHALL accept an optional `include_stats` query parameter.

#### Scenario: Get request without stats parameter

- **WHEN** client calls `GET /api/projects/:id` without `include_stats` parameter
- **THEN** response includes project data WITHOUT stats field

#### Scenario: Get request with stats parameter true

- **WHEN** client calls `GET /api/projects/:id?include_stats=true`
- **THEN** response includes project data WITH stats field populated
- **AND** stats field contains documentCount, objectCount, relationshipCount, totalJobs, runningJobs, queuedJobs, templatePacks
- **AND** each template pack in templatePacks includes name, version, objectTypes array, relationshipTypes array

### Requirement: SDK client Get method supports stats option

The Go SDK client `pkg/sdk/projects.Client` SHALL provide an option to request stats when getting a single project.

#### Scenario: SDK Get without stats

- **WHEN** SDK client calls `Get()` method without stats option
- **THEN** SDK sends request to `GET /api/projects/:id` without include_stats parameter

#### Scenario: SDK Get with stats

- **WHEN** SDK client calls `Get()` method with `IncludeStats: true` option
- **THEN** SDK sends request to `GET /api/projects/:id?include_stats=true`
- **AND** SDK parses response including optional Stats field
