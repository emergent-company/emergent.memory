## RENAMED Requirements

### Requirement: Personal Assistant Template Pack
FROM: Personal Assistant Template Pack
TO: Personal Assistant Schema

### Requirement: Product Framework Template Pack
FROM: Product Framework Template Pack
TO: Product Framework Schema

### Requirement: Product-specific AI Prompts
FROM: Product-specific AI Prompts
TO: Product-specific AI Prompts (no rename needed)

## MODIFIED Requirements

### Requirement: CLI command for managing schemas

The system SHALL provide a CLI command group `schemas` (replacing `template-packs`) for managing MemorySchema records. All subcommands and user-visible output SHALL use the term "schema"/"schemas".

#### Scenario: List schemas via CLI

- **WHEN** the user runs `memory schemas list`
- **THEN** the system SHALL return a list of available MemorySchema records

#### Scenario: Install schema via CLI

- **WHEN** the user runs `memory schemas install <schema-id>`
- **THEN** the system SHALL assign the schema to the current project and confirm "Schema installed."

#### Scenario: Create schema via CLI

- **WHEN** the user runs `memory schemas create --file schema.json`
- **THEN** the system SHALL create a new MemorySchema record and confirm "Schema created!"

#### Scenario: Old template-packs command does not exist

- **WHEN** the user runs `memory template-packs list`
- **THEN** the CLI SHALL return an error "unknown command: template-packs"
- **AND** no backward-compat alias SHALL be registered

### Requirement: UI labels use "Schema" terminology

The system SHALL display "Schema" and "Schemas" in all user-facing UI surfaces that previously displayed "Template Pack" or "Template Packs".

#### Scenario: Settings nav label

- **WHEN** a user views the project settings navigation
- **THEN** the nav item previously labelled "Template Packs" SHALL be labelled "Schemas"

#### Scenario: Available and installed schema lists

- **WHEN** a user views the schemas settings page
- **THEN** the section headers SHALL read "Installed Schemas" and "Available Schemas" (not "Template Packs")

#### Scenario: Install button label

- **WHEN** a user views an available schema
- **THEN** the install action button SHALL read "Install Schema" (not "Install Template Pack")
