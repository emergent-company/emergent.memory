## ADDED Requirements

### Requirement: Digest agent definition in blueprint

The blueprint SHALL define one agent named `ai-morning-digest` responsible for synthesizing the day's gathered AI news into a concise, engaging morning summary.

#### Scenario: Digest agent defined in blueprint YAML

- **WHEN** the blueprint is applied
- **THEN** an agent definition named `ai-morning-digest` SHALL be created
- **AND** it SHALL have access to graph search tools to query all AI news objects created today
- **AND** it SHALL have a detailed system prompt defining the digest format and curation criteria

### Requirement: Digest agent reads today's items from the graph

The digest agent SHALL query the knowledge graph to retrieve AI news objects created or discovered on the current date, across all six categories.

#### Scenario: Querying today's news items

- **WHEN** the digest agent runs
- **THEN** it SHALL search for objects of each AI news type where `discovered_date` equals today
- **AND** it SHALL retrieve at minimum: title, summary, url, and key metadata for each item

#### Scenario: No items found for a category

- **WHEN** the digest agent finds zero items for a given category on the current date
- **THEN** it SHALL omit that category's section from the digest
- **AND** it SHALL NOT fabricate or hallucinate items for missing categories

### Requirement: Digest output format

The digest agent SHALL produce a structured, readable summary following a defined format.

#### Scenario: Digest structure

- **WHEN** the digest agent produces output
- **THEN** the output SHALL contain: a short punchy intro line (1 sentence), followed by up to 6 category sections, each with a category heading and 1-3 highlighted items
- **AND** each highlighted item SHALL include: item title as a link (markdown), and 1-2 sentences of editorial commentary explaining why it matters

#### Scenario: Length constraint

- **WHEN** the digest is generated
- **THEN** total output SHALL be between 250 and 600 words
- **AND** if more than 3 items exist for a category, the agent SHALL select only the most significant/interesting ones

#### Scenario: Tone and style

- **WHEN** the digest agent writes commentary
- **THEN** the tone SHALL be informative, slightly enthusiastic, and opinionated — not dry or listy
- **AND** the agent SHALL highlight what is genuinely surprising, important, or exciting, not merely restate the headline

### Requirement: Digest agent runs after research agents complete

The digest agent SHALL be triggered after the six research agents have completed their daily run.

#### Scenario: Digest trigger timing

- **WHEN** the daily schedule runs
- **THEN** the digest agent SHALL run after all six category research agents have finished
- **AND** the trigger configuration SHALL enforce this ordering (e.g., later scheduled time or explicit dependency)

### Requirement: Digest agent stores output as a graph object

The digest agent SHALL save each produced digest as a graph object for historical retrieval.

#### Scenario: Digest saved to graph

- **WHEN** the digest agent finishes generating output
- **THEN** it SHALL create a graph object with type `AIDailyDigest` (plain object labeled `daily-digest`)
- **AND** the object properties SHALL include: `date` (today's date), `content` (full digest markdown text), `item_count` (total number of items included)

#### Scenario: Retrieving past digests

- **WHEN** a user queries the graph for objects labeled `daily-digest`
- **THEN** past digest objects SHALL be returned in reverse chronological order by `date`
