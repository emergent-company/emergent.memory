## ADDED Requirements

### Requirement: AI News template pack definition

The system SHALL provide a template pack named `AI News` that defines object types for storing AI news items across six topic categories.

#### Scenario: Template pack installs via blueprint

- **WHEN** `memory blueprints blueprints/ai-news/` is run
- **THEN** the `AI News` template pack SHALL be created in the target project
- **AND** all six object types SHALL be available for creating graph objects

### Requirement: Six AI news object types

The template pack SHALL define exactly six object types, each representing a distinct AI topic category.

#### Scenario: AIResearchPaper object type exists

- **WHEN** the AI News template pack is installed
- **THEN** an object type `AIResearchPaper` SHALL exist with properties: title (string, required), summary (string), url (string, required), authors (string), published_date (date), source (string), topics (string)

#### Scenario: AIModelRelease object type exists

- **WHEN** the AI News template pack is installed
- **THEN** an object type `AIModelRelease` SHALL exist with properties: title (string, required), summary (string), url (string, required), provider (string), model_family (string), release_date (date), benchmark_highlights (string), access_type (string)

#### Scenario: AIToolRelease object type exists

- **WHEN** the AI News template pack is installed
- **THEN** an object type `AIToolRelease` SHALL exist with properties: title (string, required), summary (string), url (string, required), maker (string), category (string), release_date (date), pricing (string)

#### Scenario: AIIndustryEvent object type exists

- **WHEN** the AI News template pack is installed
- **THEN** an object type `AIIndustryEvent` SHALL exist with properties: title (string, required), summary (string), url (string, required), companies_involved (string), event_type (string), date (date), significance (string)

#### Scenario: AIEthicsUpdate object type exists

- **WHEN** the AI News template pack is installed
- **THEN** an object type `AIEthicsUpdate` SHALL exist with properties: title (string, required), summary (string), url (string, required), topic_area (string), stakeholders (string), date (date), implications (string)

#### Scenario: AIApplicationDemo object type exists

- **WHEN** the AI News template pack is installed
- **THEN** an object type `AIApplicationDemo` SHALL exist with properties: title (string, required), summary (string), url (string, required), demo_type (string), technology_used (string), date (date), wow_factor (string)

### Requirement: Relationship types for AI news objects

The template pack SHALL define relationship types enabling connections between AI news items.

#### Scenario: COVERS_TOPIC relationship

- **WHEN** an AI news object is related to a topic cluster
- **THEN** a `COVERS_TOPIC` relationship SHALL be creatable between any two AI news objects or from any AI news object to a plain object representing a topic

#### Scenario: RELATES_TO relationship

- **WHEN** two news items reference the same technology, event, or person
- **THEN** a `RELATES_TO` relationship SHALL be creatable between any two AI news objects

#### Scenario: SUPERSEDES relationship

- **WHEN** a new model release or tool update replaces a previous one
- **THEN** a `SUPERSEDES` relationship SHALL be creatable from newer to older AI news objects of the same type
