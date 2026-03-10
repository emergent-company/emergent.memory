## ADDED Requirements

### Requirement: Six category research agent definitions

The blueprint SHALL define six agent definitions, one per AI news category, each responsible for discovering and storing fresh news items for that category daily.

#### Scenario: Research agents defined in blueprint YAML

- **WHEN** the blueprint is applied
- **THEN** six agent definitions SHALL be created: `ai-research-papers`, `ai-model-releases`, `ai-tools-products`, `ai-industry-news`, `ai-ethics-safety`, `ai-applications-demos`
- **AND** each SHALL have a category-specific system prompt, a list of default search queries, and access to a `web_search` MCP tool

#### Scenario: Each research agent has a targeted system prompt

- **WHEN** an AI research agent runs
- **THEN** its system prompt SHALL instruct it to: search for content from the past 24 hours, evaluate relevance to the category, skip low-quality or duplicate results
- **AND** the prompt SHALL specify what information to extract: title, URL, summary (2-3 sentences), and key metadata fields for that object type

### Requirement: Research agents check for duplicates before creating objects

Each research agent SHALL query the knowledge graph for existing objects with matching URL before creating a new object.

#### Scenario: Duplicate URL detected

- **WHEN** a research agent finds a news item with a URL that already exists in the graph
- **THEN** the agent SHALL skip creation of that object
- **AND** the agent SHALL log that the item was skipped as a duplicate

#### Scenario: No duplicate — new object created

- **WHEN** a research agent finds a news item whose URL does not exist in the graph
- **THEN** the agent SHALL create a new object of the appropriate type with all extracted properties populated
- **AND** the object SHALL include a `discovered_date` value set to today's date in its properties

### Requirement: Research agents use graph search tool

Each research agent SHALL have access to graph search tools to query existing objects before creation.

#### Scenario: Graph search tool available to research agents

- **WHEN** a research agent is triggered
- **THEN** it SHALL have access to both a `web_search` MCP tool (external) and graph object creation/search tools (internal)
- **AND** the agent SHALL use graph search to check for existing objects before creating new ones

### Requirement: Research agents run on a daily schedule

All six research agents SHALL be configured with a daily trigger that runs in the early morning.

#### Scenario: Daily trigger configuration

- **WHEN** the blueprint is applied
- **THEN** each research agent definition SHALL have a `trigger` configured for daily execution
- **AND** agents SHALL be designed to run independently so a failure in one does not block others

### Requirement: Research agent default search queries seeded

The blueprint SHALL provide seed data containing default search queries for each category agent.

#### Scenario: Seed queries available per category

- **WHEN** the blueprint is applied with seed data
- **THEN** graph objects of a `AISearchQuery` type (or plain objects labeled `search-query`) SHALL be created for each category
- **AND** each object SHALL contain a `query_text` property with a pre-written search query string
- **AND** each SHALL be labeled with its category name for retrieval by the corresponding agent

#### Scenario: Seed queries include date-aware templates

- **WHEN** a research agent executes a search query from seed data
- **THEN** queries that include `{date}` placeholders SHALL have the placeholder replaced with today's date (YYYY-MM-DD) before executing
