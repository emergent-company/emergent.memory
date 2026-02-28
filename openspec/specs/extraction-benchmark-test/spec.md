# extraction-benchmark-test Specification

## Purpose
TBD - created by archiving change synthetic-document-extraction-test. Update Purpose after archive.
## Requirements
### Requirement: CLI invocation with explicit flags

The script SHALL accept the following CLI flags at invocation time:

- `--host` (required): base URL of the target server (e.g., `https://api.dev.emergent-company.ai`)
- `--api-key` (required): API key for authentication (format `emt_...`)
- `--project-id` (required): project ID to run the benchmark against
- `--min-entity-recall` (optional, default `0.60`): minimum required entity recall
- `--min-entity-precision` (optional, default `0.50`): minimum required entity precision
- `--min-rel-recall` (optional, default `0.50`): minimum required relationship recall
- `--min-rel-precision` (optional, default `0.40`): minimum required relationship precision
- `--log-file` (optional, default `docs/tests/extraction_bench_log.jsonl`): path to append JSONL run record
- `--poll-timeout` (optional, default `120`): seconds to wait for extraction job completion

The script SHALL print usage and exit non-zero if any required flag is missing.

#### Scenario: Invoked with all required flags

- **WHEN** the script is run as `go run ./cmd/extraction-bench/ --host <url> --api-key <key> --project-id <id>`
- **THEN** the script SHALL proceed through all benchmark phases and exit 0 on success

#### Scenario: Missing required flag

- **WHEN** any of `--host`, `--api-key`, or `--project-id` is absent
- **THEN** the script SHALL print a usage message and exit with a non-zero code

---

### Requirement: Hardcoded synthetic IMDB-style dataset

The script SHALL embed a fixed, deterministic dataset of at least 8 movies, 12 people, and 20 relationships (ACTED_IN, DIRECTED, WROTE) as Go struct literals within the source file. The dataset SHALL NOT be fetched from any external source at runtime, ensuring byte-identical input across all environments and runs.

#### Scenario: Dataset is self-contained

- **WHEN** the script runs in any environment
- **THEN** the same set of movies, people, and relationships SHALL be used as ground truth with no network calls to load the dataset

#### Scenario: Dataset includes all three relationship types

- **WHEN** the ground truth is defined
- **THEN** it SHALL contain at least one ACTED_IN, one DIRECTED, and one WROTE relationship

---

### Requirement: Synthetic prose document generation

The script SHALL generate a plain-text, English-prose document from the hardcoded dataset. The document SHALL describe each movie in a separate paragraph, mentioning the title, release year, genre, director, writer(s), and lead actor(s) in natural language. The document SHALL be deterministic — given the same dataset, the generator SHALL always produce the same byte sequence.

#### Scenario: Document describes all movies

- **WHEN** the document is generated from a dataset of N movies
- **THEN** the document SHALL contain a textual mention of all N movie titles

#### Scenario: Document mentions all people

- **WHEN** the document is generated
- **THEN** every person referenced in a relationship SHALL have their name appear in the document at least once

#### Scenario: Document uses prose, not structured data

- **WHEN** the document is generated
- **THEN** it SHALL be readable English prose without JSON, CSV, XML, or markdown table formatting

---

### Requirement: Document upload via multipart form API

The script SHALL upload the generated synthetic document to the configured server using `POST /api/documents/upload` as a multipart/form-data request, authenticating via the `Authorization: Bearer <api-key>` header and including the project ID in the `X-Project-ID` header. The script SHALL store the document ID returned in the response for use in the subsequent extraction job.

#### Scenario: Successful upload returns document ID

- **WHEN** the synthetic document is posted to the upload endpoint with valid credentials
- **THEN** the response SHALL have HTTP 200 or 201 status and include a document ID in the response body

#### Scenario: Upload failure causes script to abort

- **WHEN** the upload returns a non-success HTTP status
- **THEN** the script SHALL print the error, exit non-zero, and not attempt extraction

---

### Requirement: Extraction job creation with IMDB-style schema

The script SHALL create an extraction job via `POST /api/admin/extraction-jobs` referencing the uploaded document ID, using an extraction config that includes:

- Object types: `Movie` (properties: title, year, genre, runtime) and `Person` (properties: name, birthYear)
- Relationship types: `ACTED_IN` (Person→Movie), `DIRECTED` (Person→Movie), `WROTE` (Person→Movie)
- `source_type` of `"document"` with `source_id` set to the uploaded document's ID

The script SHALL authenticate using the same API key and project ID headers.

#### Scenario: Extraction job created successfully

- **WHEN** the job is posted with valid document ID and schema
- **THEN** the response SHALL have HTTP 201 status and include a job ID and initial status of `"queued"`

#### Scenario: Job creation failure causes script to abort

- **WHEN** the job creation returns a non-201 status
- **THEN** the script SHALL print the error body and exit non-zero

---

### Requirement: Polling for extraction job completion

The script SHALL poll `GET /api/admin/extraction-jobs/{jobID}` at 2-second intervals until the job status is either `"completed"` or `"failed"`, or until the `--poll-timeout` value in seconds is reached. Progress SHALL be printed to stdout during polling. If the job fails or times out, the script SHALL print the error message and exit non-zero.

#### Scenario: Job completes within timeout

- **WHEN** the extraction job transitions to `"completed"` before the timeout
- **THEN** the script SHALL proceed to the scoring phase

#### Scenario: Job fails

- **WHEN** the job status is `"failed"`
- **THEN** the script SHALL print the job's `error_message` and exit non-zero

#### Scenario: Polling exceeds timeout

- **WHEN** the poll timeout is exhausted before the job reaches a terminal state
- **THEN** the script SHALL print a timeout message and exit non-zero

---

### Requirement: Graph query to retrieve extracted objects and relationships

After successful extraction, the script SHALL query `GET /api/graph/objects` and `GET /api/graph/relationships` to retrieve all entities and relationships created within the project scope.

#### Scenario: Objects retrieved after extraction

- **WHEN** extraction completes
- **THEN** the graph objects endpoint SHALL return a response the script can parse for scoring

#### Scenario: Empty extraction is handled gracefully

- **WHEN** the graph query returns zero objects
- **THEN** the script SHALL compute 0% precision and recall, log the result, and evaluate against thresholds (likely failing)

---

### Requirement: Precision and recall scoring with fuzzy name matching

The script SHALL compare extracted objects and relationships to the ground truth dataset using case-insensitive substring matching on the primary identifying field (movie title or person name). The script SHALL compute and print four metrics:

- **Entity recall**: fraction of ground-truth entities whose name appears in any extracted object
- **Entity precision**: fraction of extracted objects whose name matches any ground-truth entity
- **Relationship recall**: fraction of ground-truth relationships whose source and target both matched extracted objects
- **Relationship precision**: fraction of extracted relationships whose source and target both match ground-truth objects

#### Scenario: Fuzzy match accepts minor LLM variations

- **WHEN** the LLM extracts a name that is a case-insensitive substring of a ground-truth name (or vice versa)
- **THEN** the match SHALL count as a true positive

#### Scenario: Completely unrelated extracted entity is a false positive

- **WHEN** the LLM extracts an entity whose name has no substring relationship with any ground-truth name
- **THEN** it SHALL count as a false positive and reduce precision

---

### Requirement: Structured results table printed per run

After scoring, the script SHALL print to stdout:

- A summary table with metric name, computed value (as percentage), threshold, and pass/fail indicator
- Lists of matched entities, missing entities (false negatives), and spurious entities (false positives)
- The same breakdown for relationships

#### Scenario: Results printed regardless of pass/fail

- **WHEN** the scoring phase completes
- **THEN** the full results table SHALL appear in stdout output whether the script exits 0 or non-zero

---

### Requirement: JSONL run log appended per invocation

The script SHALL append one JSON line to the file specified by `--log-file` after each run. The record SHALL include: timestamp, host, project ID, entity recall, entity precision, relationship recall, relationship precision, extraction job ID, document ID, and whether thresholds were met.

#### Scenario: Log file created if absent

- **WHEN** the log file path does not exist
- **THEN** the script SHALL create the file and write the first record

#### Scenario: Log file appended when it exists

- **WHEN** the log file already contains previous run records
- **THEN** the script SHALL append a new line without modifying existing records

---

### Requirement: Configurable pass/fail thresholds via flags

The script SHALL exit 0 only if all four metrics meet or exceed their respective threshold values. Otherwise it SHALL exit with a non-zero code. Default thresholds are permissive to accommodate model variability:

- Entity recall: 0.60
- Entity precision: 0.50
- Relationship recall: 0.50
- Relationship precision: 0.40

#### Scenario: All metrics meet thresholds

- **WHEN** all four computed metrics are at or above their threshold values
- **THEN** the script SHALL exit 0

#### Scenario: Any metric is below threshold

- **WHEN** any computed metric falls below its threshold
- **THEN** the script SHALL print which metric(s) failed and exit non-zero

#### Scenario: Thresholds overridden via flag

- **WHEN** `--min-entity-recall=0.80` is passed
- **THEN** the entity recall threshold SHALL be 0.80 rather than the default 0.60

