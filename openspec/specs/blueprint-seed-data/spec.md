## ADDED Requirements

### Requirement: Seed directory in blueprint format

The Blueprint directory format SHALL support an optional `seed/` subdirectory alongside `packs/` and `agents/`. The `seed/` directory SHALL contain two subdirectories:

- `seed/objects/` — JSONL files where each line is a graph object record
- `seed/relationships/` — JSONL files where each line is a relationship record

Files with extensions other than `.jsonl` SHALL be silently skipped.

#### Scenario: Blueprint with seed directory is loaded

- **WHEN** `memory blueprints <dir>` is run on a directory that contains a `seed/objects/` or `seed/relationships/` subdirectory
- **THEN** the CLI SHALL parse all `.jsonl` files in those directories (including split files like `<Type>.001.jsonl`)
- **AND** files with unsupported extensions SHALL be silently skipped
- **AND** parse errors SHALL be reported as warnings and processing SHALL continue for remaining files

#### Scenario: Blueprint without seed directory is unaffected

- **WHEN** `memory blueprints <dir>` is run on a directory with no `seed/` subdirectory
- **THEN** the CLI SHALL apply packs and agents normally with no error
- **AND** the seed phase SHALL be a no-op

### Requirement: Seed file format (JSONL)

Each line in a seed file SHALL be a JSON object (JSONL format).

#### Scenario: Valid object record

- **WHEN** a `.jsonl` file in `seed/objects/` is parsed
- **THEN** each line SHALL be a JSON object with at minimum a `type` field
- **AND** optional fields SHALL include: `key` (string), `properties` (map), `labels` (list of strings), `status` (string)

#### Scenario: Valid relationship record

- **WHEN** a `.jsonl` file in `seed/relationships/` is parsed
- **THEN** each line SHALL be a JSON object with at minimum a `type` field
- **AND** relationship endpoints SHALL be expressed as either (`srcId` + `dstId`) or (`srcKey` + `dstKey`)

#### Scenario: Relationship with unresolvable key

- **WHEN** a relationship references a `srcKey` or `dstKey` that cannot be resolved to an object in the project
- **THEN** the CLI SHALL record an error result for that relationship
- **AND** processing SHALL continue for the remaining relationships

### Requirement: Seed applied after packs and agents

The CLI SHALL apply seed data as the final phase of a blueprint apply run, after template packs and agent definitions.

#### Scenario: Apply order is packs → agents → seed

- **WHEN** `memory blueprints <dir>` is run with packs, agents, and seed files present
- **THEN** template packs SHALL be applied first
- **AND** agent definitions SHALL be applied second
- **AND** seed objects and relationships SHALL be applied third

### Requirement: Bulk object creation with batching

The seeder SHALL create objects via the bulk API in batches, to support large datasets without timeouts.

#### Scenario: Object batch size

- **WHEN** a seed directory contains more than 100 objects
- **THEN** objects SHALL be sent to the API in batches of at most 100 per request
- **AND** all batches SHALL be processed even if an earlier batch returns errors

#### Scenario: Summary output

- **WHEN** the seed phase completes
- **THEN** the CLI SHALL print counts of objects and relationships created, updated, skipped, and errored
- **AND** seed counts SHALL appear alongside pack and agent counts in the final summary line

### Requirement: Key-based deduplication for objects

Objects with a `key` field SHALL be deduplicated against existing objects in the project.

#### Scenario: Object with key already exists, no --upgrade

- **WHEN** `memory blueprints` is run and an object with the same `key` already exists in the project
- **AND** `--upgrade` is not set
- **THEN** the CLI SHALL skip that object and record it as skipped

#### Scenario: Object with key already exists, with --upgrade

- **WHEN** `memory blueprints` is run and an object with the same `key` already exists in the project
- **AND** `--upgrade` is set
- **THEN** the CLI SHALL update the existing object's properties, labels, and status
- **AND** record it as updated

#### Scenario: Object without key is always created

- **WHEN** a seed object has no `key` field
- **THEN** the CLI SHALL always create a new object regardless of `--upgrade`

### Requirement: Dry-run support for seed data

Seed data operations SHALL respect the `--dry-run` flag.

#### Scenario: Dry run prints intended actions

- **WHEN** `memory blueprints <dir> --dry-run` is run with seed files present
- **THEN** the CLI SHALL print what objects and relationships would be created or updated
- **AND** no API calls SHALL be made for seed data

### Requirement: GitHub URL source for seed data

Seed data SHALL be supported when the blueprint source is a GitHub URL.

#### Scenario: GitHub-hosted blueprint with seed directory

- **WHEN** `memory blueprints https://github.com/org/repo` is run and the repo contains a `seed/` directory
- **THEN** the CLI SHALL download the repo, parse the seed directory, and apply seed data as if it were a local directory
