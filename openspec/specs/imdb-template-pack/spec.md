## ADDED Requirements

### Requirement: Object types defined

The template pack SHALL define the following object types with their respective properties:

- `Movie` — properties: `title` (string), `original_title` (string), `release_year` (number), `rating` (number), `votes` (number), `runtime_minutes` (number), `title_type` (string), `rating_tier` (string), `duration_category` (string), `release_decade` (string)
- `Person` — properties: `name` (string), `birth_year` (number), `death_year` (number)
- `Genre` — properties: `name` (string)
- `Character` — properties: `name` (string)
- `Season` — properties: `season_number` (number)
- `Profession` — properties: `name` (string)

#### Scenario: Pack applied to a project

- **WHEN** `memory blueprints <source>` is run against a project
- **THEN** all six object types SHALL be registered in the project's type registry
- **AND** each type SHALL have the properties listed above available for object creation

#### Scenario: Pack applied twice without --upgrade

- **WHEN** `memory blueprints <source>` is run on a project that already has the pack installed
- **AND** `--upgrade` is not set
- **THEN** the CLI SHALL skip all existing types without error

### Requirement: Relationship types defined

The template pack SHALL define the following relationship types:

- `ACTED_IN` — Person → Movie, properties: `character_name` (string), `ordering` (number)
- `DIRECTED` — Person → Movie
- `WROTE` — Person → Movie
- `IN_GENRE` — Movie → Genre
- `HAS_PROFESSION` — Person → Profession
- `KNOWN_FOR` — Person → Movie
- `PLAYED` — Person → Character
- `APPEARS_IN` — Character → Movie
- `EPISODE_OF` — Movie → Movie (episode to parent series)
- `IN_SEASON` — Movie → Season
- `SEASON_OF` — Season → Movie (season to parent series)

#### Scenario: Relationship types registered

- **WHEN** the pack is applied
- **THEN** all eleven relationship types SHALL be registered in the project's type registry
- **AND** each relationship SHALL enforce its defined source and destination object types

### Requirement: Pack file format

The pack definition SHALL be a single YAML file under `packs/` at the repo root, following the Memory blueprint pack format.

#### Scenario: Pack file is valid YAML

- **WHEN** the pack file is parsed by `memory blueprints`
- **THEN** it SHALL load without errors
- **AND** it SHALL conform to the Memory template pack schema (name, object_types, relationship_types)
