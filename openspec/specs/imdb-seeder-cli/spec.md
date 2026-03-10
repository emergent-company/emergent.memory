## ADDED Requirements

### Requirement: Configuration via flags and environment variables

The seeder SHALL accept all connection and runtime parameters via CLI flags, with environment variable fallbacks. No values SHALL be hardcoded.

| Flag | Env var | Required | Default | Description |
|---|---|---|---|---|
| `--server` | `MEMORY_SERVER` | yes | — | Memory server URL |
| `--token` | `MEMORY_PROJECT_TOKEN` | yes | — | Project token |
| `--project` | `MEMORY_PROJECT_ID` | yes | — | Project ID |
| `--state-dir` | `MEMORY_STATE_DIR` | no | `~/.imdb-seed-state` | Checkpoint directory |
| `--limit` | `SEED_LIMIT` | no | 0 (no limit) | Cap on number of titles to ingest |
| `--votes` | `SEED_MIN_VOTES` | no | 5000 | Minimum vote count threshold |

#### Scenario: Missing required flag

- **WHEN** the seeder is run without `--server`, `--token`, or `--project`
- **THEN** it SHALL exit with a non-zero status code and print a clear error message naming the missing parameter

#### Scenario: Flag overrides environment variable

- **WHEN** both a flag and its corresponding environment variable are set
- **THEN** the flag value SHALL take precedence

### Requirement: TSV streaming and decompression

The seeder SHALL download and stream the following IMDb dataset files from `https://datasets.imdbws.com/`, decompressing gzip on the fly without writing the raw compressed data to disk:

- `title.ratings.tsv.gz`
- `title.basics.tsv.gz`
- `title.principals.tsv.gz`
- `title.crew.tsv.gz`
- `name.basics.tsv.gz`
- `title.episode.tsv.gz`
- `title.akas.tsv.gz`

#### Scenario: Streaming a dataset file

- **WHEN** the seeder fetches a TSV file
- **THEN** it SHALL use a `gzip.Reader` wrapping the HTTP response body
- **AND** it SHALL process rows without buffering the entire file in memory

#### Scenario: Download to local cache

- **WHEN** a TSV file has already been downloaded to the state directory
- **THEN** the seeder SHALL read from the local cache instead of re-downloading

### Requirement: High-value filtering

The seeder SHALL filter incoming data to retain only high-signal entries.

#### Scenario: Filtering by vote count

- **WHEN** parsing `title.ratings.tsv.gz`
- **THEN** the seeder SHALL only retain title IDs where `numVotes` is greater than or equal to `--votes` (default 5000)

#### Scenario: Filtering by title type

- **WHEN** parsing `title.basics.tsv.gz`
- **THEN** the seeder SHALL retain titles of types: `movie`, `tvSeries`, `tvMiniSeries`, `tvEpisode`, `tvMovie`, `short`, `videoGame`

#### Scenario: Limit cap applied

- **WHEN** `--limit N` is set and N > 0
- **THEN** the seeder SHALL stop processing titles after N titles have been ingested

### Requirement: Object ingestion

The seeder SHALL create graph objects for all filtered entities via the Memory graph API.

#### Scenario: Movie/title objects created

- **WHEN** a filtered title is processed
- **THEN** it SHALL be inserted as the appropriate object type (`Movie`, etc.) with properties: `title`, `original_title`, `release_year`, `rating`, `votes`, `runtime_minutes`, `title_type`, `rating_tier`, `duration_category`, `release_decade`

#### Scenario: Person objects created

- **WHEN** a cast or crew member associated with a filtered title is processed
- **THEN** they SHALL be inserted as a `Person` object with properties: `name`, `birth_year`, `death_year`

#### Scenario: Supporting objects created

- **WHEN** genres, characters, seasons, and professions are encountered
- **THEN** they SHALL be inserted as `Genre`, `Character`, `Season`, and `Profession` objects respectively

#### Scenario: Bulk batching

- **WHEN** objects are sent to the API
- **THEN** they SHALL be sent in batches of at most 100 per request using the bulk create endpoint
- **AND** a worker pool of 20 concurrent goroutines SHALL be used

### Requirement: Relationship ingestion

The seeder SHALL create graph relationships between ingested objects after all objects have been created.

#### Scenario: Acting relationships

- **WHEN** a principal record shows an actor role
- **THEN** an `ACTED_IN` relationship SHALL be created between the `Person` and the title object, with `character_name` and `ordering` properties

#### Scenario: Crew relationships

- **WHEN** a principal or crew record shows a director or writer role
- **THEN** a `DIRECTED` or `WROTE` relationship SHALL be created between the `Person` and the title object

#### Scenario: Genre, episode, season relationships

- **WHEN** title metadata includes genres, episode, or season data
- **THEN** the corresponding `IN_GENRE`, `EPISODE_OF`, `IN_SEASON`, `SEASON_OF` relationships SHALL be created

### Requirement: Checkpoint and resume

The seeder SHALL persist progress to the state directory so that an interrupted run can be resumed without re-ingesting already-created data.

#### Scenario: SIGINT during object ingestion

- **WHEN** the seeder receives SIGINT or SIGTERM during the objects phase
- **THEN** it SHALL finish the current batch, save state to `state.json`, and exit cleanly

#### Scenario: Resuming after interruption

- **WHEN** the seeder is run and `state.json` exists indicating objects are done
- **THEN** it SHALL skip the objects phase and resume from the relationships phase

#### Scenario: Retry failed relationships

- **WHEN** the seeder is run with `RETRY_FAILED=true`
- **THEN** it SHALL replay only the batches recorded in `rels_failed.jsonl`
