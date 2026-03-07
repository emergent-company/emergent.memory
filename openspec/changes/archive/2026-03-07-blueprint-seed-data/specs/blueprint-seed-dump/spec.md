## ADDED Requirements

### Requirement: Dump subcommand

The CLI SHALL provide a `memory blueprints dump <output-dir>` subcommand that exports graph objects and relationships from the current project into seed files that are directly re-applyable with `memory blueprints <output-dir>`.

#### Scenario: Basic dump writes seed files

- **WHEN** `memory blueprints dump <output-dir>` is run
- **THEN** the CLI SHALL create `<output-dir>/seed/objects/` and `<output-dir>/seed/relationships/` directories
- **AND** each directory SHALL contain one or more `.jsonl` files, grouped by object/relationship type
- **AND** the output SHALL be valid input to `memory blueprints <output-dir>`

#### Scenario: Output directory is created if missing

- **WHEN** `memory blueprints dump <output-dir>` is run and `<output-dir>` does not exist
- **THEN** the CLI SHALL create the directory (and `seed/objects/` and `seed/relationships/` subdirectories) automatically

#### Scenario: Files are split at 50 MB

- **WHEN** a type's output would exceed 50 MB in a single file
- **THEN** the CLI SHALL split output into numbered files (`<Type>.001.jsonl`, `<Type>.002.jsonl`, …)
- **AND** the loader SHALL reassemble split files transparently by sorting filenames alphabetically

### Requirement: Dump format (JSONL)

All dump output SHALL use JSONL format (one JSON object per line), grouped by type.

#### Scenario: Each file contains one type

- **WHEN** `memory blueprints dump` is run on a project with multiple object types
- **THEN** each output file SHALL contain records of a single type
- **AND** the filename SHALL be `<TypeName>.jsonl` (or split variants)

### Requirement: Dump filtering by object type

The dump subcommand SHALL support restricting the export to specific object types.

#### Scenario: Filter by type(s)

- **WHEN** `memory blueprints dump <output-dir> --types TypeA,TypeB` is run
- **THEN** the CLI SHALL only export objects of the specified types
- **AND** SHALL only export relationships where both endpoints are in the exported object set

#### Scenario: No filter exports everything

- **WHEN** `memory blueprints dump <output-dir>` is run without `--types`
- **THEN** all object types SHALL be exported

### Requirement: Dump uses cursor pagination for large datasets

The dump subcommand SHALL paginate through all objects and relationships using cursor-based pagination.

#### Scenario: All pages are fetched

- **WHEN** the project has more objects than a single API page (250 per page)
- **THEN** the CLI SHALL follow `next_cursor` until all pages are retrieved
- **AND** all objects SHALL appear in the output files

#### Scenario: Progress is printed during dump

- **WHEN** `memory blueprints dump` is paginating through a large dataset
- **THEN** the CLI SHALL print progress to stdout (e.g., `  objects: 500 fetched…`)

### Requirement: Dump portable key references in relationships

The dump subcommand SHALL use object `key` values in relationship references wherever possible.

#### Scenario: Relationship endpoints written as keys when available

- **WHEN** a relationship endpoint object has a `key` field set
- **THEN** the dumped relationship SHALL use `srcKey` / `dstKey` fields
- **AND** SHALL NOT include `srcId` / `dstId` for those endpoints

#### Scenario: Synthetic key assigned to keyless objects

- **WHEN** a relationship endpoint object has no `key` field
- **THEN** the dumper SHALL assign a synthetic key `_id:<entityID>` to that object
- **AND** the dumped relationship SHALL use `srcKey` / `dstKey` with the synthetic value
- **AND** the object record in `seed/objects/` SHALL include the synthetic key field
- **AND** this ensures all relationships are expressed portably, without raw project-scoped IDs

### Requirement: Dump summary output

The dump subcommand SHALL print a summary when complete.

#### Scenario: Summary line

- **WHEN** `memory blueprints dump` completes successfully
- **THEN** the CLI SHALL print the count of objects and relationships written and the output directory path
