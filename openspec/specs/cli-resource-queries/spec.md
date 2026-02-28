# cli-resource-queries Specification

## Purpose
TBD - created by archiving change enhance-cli-ux. Update Purpose after archive.
## Requirements
### Requirement: Filter resources by attributes

The CLI SHALL support filtering list commands using `--filter` flag.

#### Scenario: Filter by single attribute

- **WHEN** user runs `emergent-cli projects list --filter name=myproject`
- **THEN** system returns only projects where name contains "myproject"

#### Scenario: Filter by multiple attributes

- **WHEN** user runs `emergent-cli documents list --filter status=completed,type=pdf`
- **THEN** system returns only documents where status is "completed" AND type is "pdf"

#### Scenario: Case-insensitive filtering

- **WHEN** user runs `emergent-cli projects list --filter name=MyProject`
- **THEN** system matches case-insensitively (finds "myproject", "MyProject", "MYPROJECT")

### Requirement: Sort resources

The CLI SHALL support sorting list results using `--sort` flag.

#### Scenario: Sort by field ascending

- **WHEN** user runs `emergent-cli projects list --sort name`
- **THEN** system returns projects sorted by name in ascending order

#### Scenario: Sort by field descending

- **WHEN** user runs `emergent-cli projects list --sort name:desc`
- **THEN** system returns projects sorted by name in descending order

#### Scenario: Sort by multiple fields

- **WHEN** user runs `emergent-cli documents list --sort status:asc,created_at:desc`
- **THEN** system sorts by status ascending, then by created_at descending

### Requirement: Paginate large result sets

The CLI SHALL support pagination using `--limit` and `--offset` flags.

#### Scenario: Limit number of results

- **WHEN** user runs `emergent-cli projects list --limit 10`
- **THEN** system returns at most 10 projects

#### Scenario: Skip results with offset

- **WHEN** user runs `emergent-cli projects list --offset 20 --limit 10`
- **THEN** system returns items 21-30 from the result set

#### Scenario: Show pagination metadata

- **WHEN** user runs paginated query
- **THEN** system displays "Showing 21-30 of 157 projects" before table

### Requirement: Search resources by text

The CLI SHALL support full-text search using `--search` flag.

#### Scenario: Search across all text fields

- **WHEN** user runs `emergent-cli documents list --search "quarterly report"`
- **THEN** system returns documents where title, description, or content contains "quarterly report"

#### Scenario: Combine search with filters

- **WHEN** user runs `emergent-cli documents list --search "report" --filter status=completed`
- **THEN** system returns only completed documents containing "report"

### Requirement: Select specific fields to display

The CLI SHALL support field selection using `--fields` flag.

#### Scenario: Display only specified fields

- **WHEN** user runs `emergent-cli projects list --fields name,id`
- **THEN** system displays table with only name and id columns

#### Scenario: Display all fields

- **WHEN** user runs `emergent-cli projects list --fields all`
- **THEN** system displays table with all available fields

#### Scenario: Invalid field name error

- **WHEN** user specifies a field that doesn't exist
- **THEN** system displays error message listing available fields

### Requirement: Query nested resources

The CLI SHALL support querying resources with parent-child relationships.

#### Scenario: List documents within project

- **WHEN** user runs `emergent-cli documents list --project myproj`
- **THEN** system returns documents that belong to project "myproj"

#### Scenario: List extractions for document

- **WHEN** user runs `emergent-cli extractions list --document doc-123`
- **THEN** system returns extractions for document "doc-123"

### Requirement: Date range filtering

The CLI SHALL support filtering by date ranges using `--from` and `--to` flags.

#### Scenario: Filter by date range

- **WHEN** user runs `emergent-cli documents list --from 2024-01-01 --to 2024-12-31`
- **THEN** system returns documents created between January 1 and December 31, 2024

#### Scenario: Filter by relative date

- **WHEN** user runs `emergent-cli documents list --from 7d`
- **THEN** system returns documents created in the last 7 days

#### Scenario: Support multiple date formats

- **WHEN** user provides dates as `2024-01-01`, `Jan 1 2024`, or `7d`
- **THEN** system parses and applies the date filter correctly

### Requirement: Export query results

The CLI SHALL support exporting query results to files.

#### Scenario: Export to JSON file

- **WHEN** user runs `emergent-cli projects list --output json > projects.json`
- **THEN** system writes JSON array of projects to projects.json

#### Scenario: Export to CSV file

- **WHEN** user runs `emergent-cli projects list --output csv > projects.csv`
- **THEN** system writes CSV with headers and project data to projects.csv

### Requirement: Show result count

The CLI SHALL display the total count of matching results.

#### Scenario: Show count with results

- **WHEN** user runs any list command
- **THEN** system displays "Found X items" or "Showing Y-Z of X items" before results

#### Scenario: Count-only mode

- **WHEN** user runs `emergent-cli projects list --count-only`
- **THEN** system displays only the count number without table or data

