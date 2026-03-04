## ADDED Requirements

### Requirement: Directory Structure

The install format SHALL use a folder-based layout where each resource type has its own subdirectory and each resource is defined in a single file.

#### Scenario: Valid directory layout

- **WHEN** a user creates an Emergent config directory
- **THEN** the directory SHALL support the following structure:
  ```
  <root>/
    packs/        # one file per template pack
    agents/       # one file per agent definition
  ```
- **AND** each subdirectory is optional — an install source with only `packs/` or only `agents/` SHALL be valid
- **AND** unknown subdirectories SHALL be ignored without error

#### Scenario: One resource per file

- **WHEN** a file exists inside `packs/` or `agents/`
- **THEN** each file SHALL define exactly one resource
- **AND** the filename (without extension) SHALL be used as a human-readable identifier but SHALL NOT be required to match the resource's `name` field
- **AND** files with extensions `.json`, `.yaml`, and `.yml` SHALL be recognized
- **AND** files with any other extension SHALL be ignored without error

### Requirement: Template Pack File Schema

Each file in `packs/` SHALL conform to a defined schema describing one template pack.

#### Scenario: Required fields

- **WHEN** a pack file is parsed
- **THEN** the following fields SHALL be required:
  - `name` (string): unique identifier for the pack
  - `version` (string): semver string (e.g. `"1.0.0"`)
  - `objectTypes` (array): at least one object type definition
- **AND** missing required fields SHALL cause the file to be skipped with a clear error message naming the file and missing field

#### Scenario: Optional pack fields

- **WHEN** a pack file is parsed
- **THEN** the following fields SHALL be optional:
  - `description` (string)
  - `author` (string)
  - `license` (string)
  - `repositoryUrl` (string)
  - `documentationUrl` (string)
  - `relationshipTypes` (array)
  - `uiConfigs` (object)
  - `extractionPrompts` (object)

#### Scenario: Object type definition

- **WHEN** an entry in `objectTypes` is parsed
- **THEN** each object type SHALL require:
  - `name` (string): identifier for the type
  - `label` (string): human-readable display name
- **AND** SHALL optionally include:
  - `description` (string)
  - `properties` (object): field definitions

#### Scenario: Relationship type definition

- **WHEN** an entry in `relationshipTypes` is parsed
- **THEN** each relationship type SHALL require:
  - `name` (string)
  - `label` (string)
  - `sourceTypes` (array of strings): allowed source object type names
  - `targetTypes` (array of strings): allowed target object type names
- **AND** SHALL optionally include:
  - `description` (string)

#### Scenario: YAML pack file example

- **WHEN** a user authors a pack file in YAML
- **THEN** the following SHALL be a valid minimal example:
  ```yaml
  name: my-research-pack
  version: 1.0.0
  description: Object types for research workflows
  objectTypes:
    - name: paper
      label: Research Paper
      description: An academic paper or article
    - name: author
      label: Author
  relationshipTypes:
    - name: written_by
      label: Written By
      sourceTypes: [paper]
      targetTypes: [author]
  ```

### Requirement: Agent Definition File Schema

Each file in `agents/` SHALL conform to a defined schema describing one agent definition.

#### Scenario: Required fields

- **WHEN** an agent file is parsed
- **THEN** the following field SHALL be required:
  - `name` (string): unique identifier for the agent within the project
- **AND** missing the `name` field SHALL cause the file to be skipped with a clear error message

#### Scenario: Optional agent fields

- **WHEN** an agent file is parsed
- **THEN** the following fields SHALL be optional:
  - `description` (string)
  - `systemPrompt` (string): the agent's system prompt; multiline strings are idiomatic in YAML
  - `model` (object): `{ provider, name }` model selection
  - `tools` (array of strings): tool names the agent can use
  - `flowType` (string): e.g. `"sequential"`, `"react"`
  - `isDefault` (boolean): whether this agent is the project default
  - `maxSteps` (integer)
  - `defaultTimeout` (integer, seconds)
  - `visibility` (string): `"project"`, `"external"`, or `"internal"`
  - `config` (object): arbitrary agent configuration
  - `workspaceConfig` (object): workspace-level configuration

#### Scenario: YAML agent file example

- **WHEN** a user authors an agent file in YAML
- **THEN** the following SHALL be a valid example:
  ```yaml
  name: research-assistant
  description: Helps users find and summarise research papers
  systemPrompt: |
    You are a research assistant. Your job is to help users
    find relevant papers and summarise key findings clearly.
  model:
    provider: google
    name: gemini-2.0-flash
  tools:
    - search
    - graph_query
  flowType: react
  isDefault: true
  visibility: project
  ```

### Requirement: JSON and YAML Equivalence

The install format SHALL treat JSON and YAML files as equivalent representations of the same schema.

#### Scenario: JSON pack file

- **WHEN** a file in `packs/` has a `.json` extension
- **THEN** it SHALL be parsed as JSON and validated against the same schema as YAML pack files

#### Scenario: Mixed file formats in one directory

- **WHEN** `packs/` contains both `.json` and `.yaml` files
- **THEN** all files SHALL be processed regardless of format
- **AND** the format of one file SHALL NOT affect parsing of others
