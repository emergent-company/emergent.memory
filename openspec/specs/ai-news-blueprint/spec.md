## ADDED Requirements

### Requirement: Blueprint directory structure

The system SHALL include a `blueprints/ai-news/` directory in the repository following the standard Emergent blueprint format.

#### Scenario: Blueprint directory layout

- **WHEN** the `blueprints/ai-news/` directory is created
- **THEN** it SHALL contain: `packs/ai-news-pack.json` (template pack), `agents/` directory with one YAML file per agent, `seed/objects/` with JSONL seed data files, and a `README.md` explaining prerequisites and setup

#### Scenario: Blueprint applied with memory CLI

- **WHEN** a user runs `memory blueprints blueprints/ai-news/ --project <project-id>`
- **THEN** the template pack SHALL be installed in the project
- **AND** all 7 agent definitions SHALL be created
- **AND** seed objects (default search queries) SHALL be created with key-based deduplication

### Requirement: Template pack JSON file

The blueprint SHALL include a `packs/ai-news-pack.json` file defining the AI News template pack.

#### Scenario: Pack JSON is valid and loadable

- **WHEN** the blueprint is applied
- **THEN** `packs/ai-news-pack.json` SHALL parse as valid JSON
- **AND** it SHALL define the pack name as `AI News`, include all 6 object type definitions, and include the 3 relationship type definitions

### Requirement: Agent definition YAML files

The blueprint SHALL include 7 YAML files in the `agents/` directory, one per agent.

#### Scenario: Agent YAML files present

- **WHEN** the blueprint directory is read
- **THEN** `agents/` SHALL contain: `research-papers.yaml`, `model-releases.yaml`, `tools-products.yaml`, `industry-news.yaml`, `ethics-safety.yaml`, `applications-demos.yaml`, `morning-digest.yaml`

#### Scenario: Each agent YAML is valid

- **WHEN** the blueprint is applied
- **THEN** each YAML file SHALL be parseable as a valid agent definition
- **AND** each SHALL specify: `name`, `description`, `system_prompt`, `model`, `tools`, `trigger`, and `visibility: project`

### Requirement: Seed data JSONL files for search queries

The blueprint SHALL include seed data with default search queries for each category.

#### Scenario: Seed query objects created on apply

- **WHEN** the blueprint is applied with seed data
- **THEN** at least 3 search query seed objects SHALL be created per category (18+ total)
- **AND** each seed object SHALL have a unique `key` in the format `ai-news-query-<category>-<n>`
- **AND** each SHALL have a `query_text` property and a `category` property

#### Scenario: Queries cover today's date

- **WHEN** seed queries are used by research agents
- **THEN** queries intended to filter by recency SHALL use a `{date}` placeholder
- **AND** the agent's system prompt SHALL instruct it to replace `{date}` with today's ISO date before searching

### Requirement: Blueprint README documents prerequisites

The blueprint SHALL include a README that documents what must be configured before applying the blueprint.

#### Scenario: README covers MCP web search setup

- **WHEN** a user reads `blueprints/ai-news/README.md`
- **THEN** it SHALL explain that a web-search MCP server (Brave, Exa, or Tavily) must be registered in the project before agents can function
- **AND** it SHALL provide example configuration for at least one provider

#### Scenario: README covers schedule configuration

- **WHEN** a user reads `blueprints/ai-news/README.md`
- **THEN** it SHALL explain how to verify that daily agent triggers are enabled
- **AND** it SHALL document the recommended run order: research agents first, digest agent last
