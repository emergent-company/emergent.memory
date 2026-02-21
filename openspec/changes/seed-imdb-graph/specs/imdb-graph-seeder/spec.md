## ADDED Requirements

### Requirement: Opt-in Execution

The IMDb seeding process MUST only execute when a specific environment variable flag is set to true.

#### Scenario: Running without flag

- **WHEN** the test suite executes without `RUN_IMDB_BENCHMARK=true`
- **THEN** the benchmark test gracefully skips execution and logs a skip message

#### Scenario: Running with flag

- **WHEN** the test suite executes with `RUN_IMDB_BENCHMARK=true`
- **THEN** the benchmark test executes the full streaming, ingestion, and querying pipeline

### Requirement: TSV Streaming and Decompression

The seeder MUST download and stream the official IMDb dataset files (`title.basics.tsv.gz`, `title.ratings.tsv.gz`, `title.principals.tsv.gz`, `name.basics.tsv.gz`) over HTTP, decompressing them on the fly without writing massive intermediary files to disk.

#### Scenario: Streaming titles

- **WHEN** fetching the IMDb datasets
- **THEN** the system uses `gzip.Reader` wrapping an `http.Response.Body` to process rows in a memory-efficient stream

### Requirement: High-Value Filtering

The seeder MUST filter out obscure or irrelevant data by applying quality thresholds to the incoming streams.

#### Scenario: Filtering by vote count

- **WHEN** parsing `title.ratings.tsv`
- **THEN** the seeder only retains `tconst` IDs for movies where `numVotes` is greater than 20,000

#### Scenario: Discarding non-movies

- **WHEN** parsing `title.basics.tsv`
- **THEN** the seeder completely ignores rows where `titleType` is not equal to "movie"

### Requirement: Graph Entity Ingestion

The seeder MUST insert valid `kb.graph_objects` representing the filtered IMDb data.

#### Scenario: Inserting movies

- **WHEN** a filtered title is processed
- **THEN** it is inserted as an object of type `Movie` with properties `title`, `release_year`, `rating`, `votes`, and `runtime_minutes`

#### Scenario: Inserting people

- **WHEN** a cast or crew member associated with a filtered movie is processed
- **THEN** they are inserted as an object of type `Person` with properties `name`, `birth_year`, and `death_year`

### Requirement: Graph Relationship Ingestion

The seeder MUST create valid `kb.graph_relationships` connecting the ingested entities based on their IMDb principal roles.

#### Scenario: Actor relationships

- **WHEN** a principal record shows an actor role
- **THEN** an `ACTED_IN` relationship is created between the `Person` and the `Movie`, containing the `character_name` as a property

#### Scenario: Crew relationships

- **WHEN** a principal record shows a director or writer role
- **THEN** a `DIRECTED` or `WROTE` relationship is created between the `Person` and the `Movie`

### Requirement: Benchmarking Agent Queries

The test suite MUST verify that the Natural Language Agent can successfully reason over the massive ingested dataset.

#### Scenario: Multi-hop reasoning

- **WHEN** the agent is asked a complex question requiring multi-hop traversal (e.g., "Did director X ever work with actor Y?")
- **THEN** the agent correctly chains `search_entities` and `get_entity_edges` tools to traverse the dense relationships and return the correct answer
