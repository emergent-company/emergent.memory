# entity-extraction Specification

## Purpose
TBD - created by archiving change optimize-extraction-batch-calls. Update Purpose after archive.
## Requirements
### Requirement: Semantic Document Chunking

The system SHALL use semantic analysis to segment documents into coherent chunks based on topic shifts, rather than fixed token counts.

#### Scenario: Chunking based on semantic similarity

- **WHEN** processing a document for extraction
- **THEN** the system generates embeddings for sequential sentences (e.g. using `text-embedding-004`)
- **AND** calculates cosine similarity between adjacent sentences
- **AND** creates a new chunk ONLY when similarity drops below a configurable threshold (indicating a topic shift)
- **AND** ensures chunks do not exceed the model's context window (splitting by max tokens only as a fallback)

#### Scenario: Chunk metadata preservation

- **WHEN** creating semantic chunks
- **THEN** each chunk retains metadata including `start_char`, `end_char`, and `source_text`
- **AND** tracks the original page numbers (if available) spanned by the chunk

### Requirement: Consolidated Zod Schema

The system SHALL define extraction schemas using Zod that encompass all target entity types in a single structure, enabling single-pass extraction.

#### Scenario: Unified schema construction

- **WHEN** preparing extraction for $N$ entity types
- **THEN** the system constructs a single `z.object` schema
- **AND** the schema includes top-level keys for each entity type (e.g., `people`, `dates`, `liabilities`)
- **AND** each key maps to a `z.array` of that specific entity's schema
- **AND** strict typing is enforced (enums, dates, numbers) to leverage LLM structured output capabilities

### Requirement: Map-Reduce Extraction Architecture

The system SHALL utilize a Map-Reduce pattern orchestrated via LangGraph to process chunks in parallel and aggregate results.

#### Scenario: Parallel "Map" processing

- **WHEN** processing a document with multiple chunks
- **THEN** the system spawns parallel extraction tasks ("Map" step) for each chunk
- **AND** each task runs independently using the Unified Zod Schema
- **AND** failures in one chunk do not stop the processing of others

#### Scenario: Aggregation "Reduce" step

- **WHEN** all chunks have been processed
- **THEN** the system collects the lists of extracted entities from all successful chunks
- **AND** merges them into a single master list
- **AND** performs deduplication based on entity identity (e.g., name + type)

### Requirement: Validation & Reflexion (Self-Correction)

The system SHALL automatically attempt to correct schema validation errors using a "Reflexion" loop.

#### Scenario: Validation failure handling

- **WHEN** an LLM response fails Zod validation (e.g., string instead of number)
- **THEN** the system captures the validation error
- **AND** feeds the error + original response back to the LLM ("Reflexion" step)
- **AND** requests a corrected JSON output
- **AND** retries up to a maximum limit (e.g., 3 attempts) before marking the chunk as failed

### Requirement: Tiered Model Strategy

The system SHALL use **Gemini 2.5 Flash** for the bulk extraction tasks to ensure high throughput and cost efficiency.

#### Scenario: Extraction model selection

- **WHEN** performing the "Map" step (chunk extraction)
- **THEN** the system utilizes **Gemini 2.5 Flash**
- **AND** utilizes the model's native "Structured Output" / "Tool Calling" mode

### Requirement: Extraction Logging

The system SHALL log the performance and cost metrics of the new architecture.

#### Scenario: Semantic chunking logging

- **WHEN** chunking a document
- **THEN** logs the number of chunks generated vs. the number of tokens
- **AND** logs the average semantic similarity score

#### Scenario: Batch performance logging

- **WHEN** completing a job
- **THEN** logs the reduction in API calls compared to the legacy baseline (Types × Chunks)
- **AND** logs the total cost saved

### Requirement: LangGraph Extraction Pipeline

The system SHALL implement a multi-node extraction pipeline using LangGraph that decouples entity extraction from relationship linking to maximize graph density and quality.

#### Scenario: Full pipeline execution on narrative document

- **WHEN** a document is submitted for extraction
- **AND** the document is classified as "narrative" type
- **THEN** the pipeline executes nodes in sequence: Router → Extractor → Resolver → Builder → Auditor
- **AND** entity extraction uses narrative-focused prompts (characters, themes, locations)
- **AND** relationships are built using temp_ids assigned during extraction

#### Scenario: Full pipeline execution on legal document

- **WHEN** a document is submitted for extraction
- **AND** the document is classified as "legal" type
- **THEN** the pipeline executes nodes in sequence: Router → Extractor → Resolver → Builder → Auditor
- **AND** entity extraction uses legal-focused prompts (defined terms, parties, dates)

#### Scenario: Pipeline handles unknown document type

- **WHEN** a document cannot be classified into a known category
- **THEN** the router assigns category "other"
- **AND** a generic extraction prompt is used

### Requirement: Document Router Node

The pipeline SHALL include a Document_Router node that classifies documents to select the appropriate extraction strategy.

#### Scenario: Classify narrative text

- **WHEN** the router receives text from a story, book, or narrative
- **THEN** it returns `{ category: 'narrative' }`
- **AND** the classification uses only the first 2000 characters for efficiency

#### Scenario: Classify legal text

- **WHEN** the router receives text from a contract, covenant, or legal document
- **THEN** it returns `{ category: 'legal' }`

#### Scenario: Router uses structured output

- **WHEN** the router LLM call completes
- **THEN** the output is validated against a strict schema
- **AND** invalid responses are rejected with an error

### Requirement: Entity Extractor Node

The pipeline SHALL include an Entity_Extractor node that extracts entities with temporary IDs, focusing only on nodes (not relationships).

#### Scenario: Extract entities with temp_ids

- **WHEN** the extractor processes a document
- **THEN** each extracted entity includes a unique `temp_id` (e.g., "peter_1", "clause_5")
- **AND** the temp*id format is `{name_slug}*{sequence}`
- **AND** relationships are NOT extracted in this step

#### Scenario: Category-specific extraction prompts

- **WHEN** the document category is "narrative"
- **THEN** the prompt instructs: "Focus on characters, emotional themes, locations"
- **WHEN** the document category is "legal"
- **THEN** the prompt instructs: "Focus on defined terms, parties, effective dates"

#### Scenario: Extractor uses structured output

- **WHEN** the extractor LLM call completes
- **THEN** the output is validated against Entity schema
- **AND** each entity has: name, type, description, temp_id, properties

### Requirement: Identity Resolver Node (Code-Based)

The pipeline SHALL include an Identity_Resolver node that maps temp_ids to real UUIDs using deterministic code logic (no LLM).

#### Scenario: Resolve entity to existing UUID via vector search

- **WHEN** an extracted entity name matches an existing entity with similarity > 0.90
- **THEN** the temp_id is mapped to the existing UUID
- **AND** the mapping is stored in `resolved_uuid_map`

#### Scenario: Generate new UUID for novel entity

- **WHEN** an extracted entity name does not match any existing entity (similarity <= 0.90)
- **THEN** a new UUID is generated
- **AND** the temp_id is mapped to the new UUID

#### Scenario: Resolution is deterministic

- **WHEN** the same entity name is processed multiple times
- **THEN** it consistently resolves to the same UUID
- **AND** no LLM calls are made during resolution

### Requirement: Relationship Builder Node

The pipeline SHALL include a Relationship_Builder node that connects entities using their temp_ids.

#### Scenario: Build relationships using temp_ids

- **WHEN** the builder processes extracted entities and original text
- **THEN** relationships reference entities by their temp_ids
- **AND** no new entities are created during this step
- **AND** the constraint "You MUST use the provided temp_ids" is enforced

#### Scenario: Builder receives entity context

- **WHEN** the builder prompt is constructed
- **THEN** it includes the list of extracted entities with their temp_ids
- **AND** it includes the original document text
- **AND** it includes the document category for context

### Requirement: Quality Auditor Node

The pipeline SHALL include a Quality_Auditor node that validates extraction quality and triggers retry loops for orphan entities.

#### Scenario: Detect orphan entities

- **WHEN** the auditor analyzes relationships and entities
- **THEN** it identifies entities that appear in neither source_ref nor target_ref of any relationship
- **AND** these are flagged as "orphans"

#### Scenario: Quality check passes

- **WHEN** all entities have at least one relationship
- **THEN** `quality_check_passed` is set to true
- **AND** the pipeline proceeds to END

#### Scenario: Quality check fails with retry

- **WHEN** orphan entities are detected
- **AND** `retry_count` < 3
- **THEN** `quality_check_passed` is set to false
- **AND** feedback is added: "Entities [X, Y] are orphans. Find their connections."
- **AND** the pipeline loops back to Relationship_Builder

#### Scenario: Quality check fails after max retries

- **WHEN** orphan entities are detected
- **AND** `retry_count` >= 3
- **THEN** a warning is logged
- **AND** the pipeline proceeds to END with partial results
- **AND** orphan entities are still persisted

### Requirement: Graph State Management

The pipeline SHALL maintain a typed GraphState that persists across all nodes.

#### Scenario: State includes all required fields

- **WHEN** a pipeline execution starts
- **THEN** the state includes:
  - `original_text`: The document content
  - `file_metadata`: Source information
  - `doc_category`: Classification result
  - `extracted_entities`: List of entities with temp_ids
  - `resolved_uuid_map`: temp_id to UUID mappings
  - `final_relationships`: List of relationships
  - `quality_check_passed`: Boolean flag
  - `retry_count`: Number of retry attempts
  - `feedback_log`: Accumulated feedback messages

#### Scenario: Feedback log accumulates across retries

- **WHEN** the Quality_Auditor adds feedback
- **AND** the pipeline retries
- **THEN** the new feedback is appended to existing feedback
- **AND** previous feedback is preserved

### Requirement: Structured Output Validation

All LLM nodes SHALL use structured output with strict schema validation.

#### Scenario: Router output validated

- **WHEN** the Document_Router returns a response
- **THEN** it is validated against: `{ category: 'narrative' | 'legal' | 'technical' | 'other' }`

#### Scenario: Extractor output validated

- **WHEN** the Entity_Extractor returns a response
- **THEN** each entity is validated against Entity schema
- **AND** invalid entities are rejected

#### Scenario: Builder output validated

- **WHEN** the Relationship_Builder returns a response
- **THEN** each relationship is validated against Relationship schema
- **AND** invalid relationships are rejected

### Requirement: Pipeline Feature Flag

The LangGraph pipeline SHALL be gated behind a feature flag for gradual rollout.

#### Scenario: Enable LangGraph pipeline via environment

- **WHEN** `EXTRACTION_PIPELINE_MODE=langgraph` is set
- **THEN** the new pipeline is used for extraction jobs

#### Scenario: Default to existing pipeline

- **WHEN** no feature flag is set
- **THEN** the existing single-pass extraction is used
- **AND** no breaking changes occur

### Requirement: Adaptive Scaling Configuration

The system SHALL allow operators to enable adaptive concurrency scaling for extraction workers.

#### Scenario: Enable adaptive scaling for a worker

- **WHEN** an operator configures `enable_adaptive_scaling: true` for an extraction worker
- **THEN** the worker dynamically adjusts its concurrency based on system health score
- **AND** respects the configured `min_concurrency` and `max_concurrency` bounds
- **AND** uses health-aware polling to prevent resource exhaustion

#### Scenario: Disable adaptive scaling for a worker

- **WHEN** an operator configures `enable_adaptive_scaling: false` for an extraction worker
- **THEN** the worker uses static concurrency defined by `worker_concurrency`
- **AND** ignores system health scores when fetching jobs
- **AND** maintains legacy behavior for backward compatibility

#### Scenario: Default adaptive scaling behavior

- **WHEN** an extraction worker starts without explicit adaptive scaling configuration
- **THEN** `enable_adaptive_scaling` defaults to `false`
- **AND** the worker operates with legacy static concurrency

### Requirement: Concurrency Bounds Configuration

The system SHALL allow operators to define minimum and maximum concurrency limits for extraction workers with adaptive scaling enabled.

#### Scenario: Configure minimum concurrency

- **WHEN** an operator sets `min_concurrency` for a worker
- **THEN** the system validates that `min_concurrency >= 1`
- **AND** ensures the worker never processes fewer than `min_concurrency` jobs concurrently
- **AND** uses this as the floor during critical health conditions

#### Scenario: Configure maximum concurrency

- **WHEN** an operator sets `max_concurrency` for a worker
- **THEN** the system validates that `max_concurrency >= min_concurrency`
- **AND** validates that `max_concurrency <= 50` (safety limit)
- **AND** ensures the worker never processes more than `max_concurrency` jobs concurrently
- **AND** uses this as the ceiling during safe health conditions

#### Scenario: Default concurrency bounds

- **WHEN** a worker has adaptive scaling enabled but no explicit min/max configuration
- **THEN** `min_concurrency` defaults to 1
- **AND** `max_concurrency` defaults to 10
- **AND** these defaults apply until explicitly overridden

### Requirement: Worker Configuration API Extension

The system SHALL extend the existing worker configuration API to support adaptive scaling parameters.

#### Scenario: Retrieve worker configuration with adaptive scaling

- **WHEN** an operator queries `GET /admin/extraction/embedding/config`
- **THEN** the response includes:
  - `worker_concurrency` (integer, legacy field for backward compatibility)
  - `enable_adaptive_scaling` (boolean, default: false)
  - `min_concurrency` (integer, default: 1)
  - `max_concurrency` (integer, default: 10)
  - `current_concurrency` (integer, current effective concurrency)
  - `health_score` (integer 0-100, latest system health score)

#### Scenario: Update worker configuration with adaptive scaling

- **WHEN** an operator posts to `POST /admin/extraction/embedding/config` with adaptive scaling fields
- **THEN** the system validates all constraints:
  - `min_concurrency >= 1`
  - `max_concurrency >= min_concurrency`
  - `max_concurrency <= 50`
- **AND** applies the configuration on the next worker polling cycle
- **AND** returns the updated configuration in the response
- **AND** logs the configuration change with operator identity

#### Scenario: Backward compatibility with legacy API

- **WHEN** an operator updates only `worker_concurrency` via the API
- **THEN** the system treats it as static concurrency if adaptive scaling is disabled
- **AND** treats it as `max_concurrency` if adaptive scaling is enabled
- **AND** logs a deprecation warning recommending explicit adaptive scaling fields

### Requirement: Health-Aware Job Polling Integration

The system SHALL integrate health monitoring into extraction worker polling loops to enable dynamic concurrency.

#### Scenario: Health check before job fetch

- **WHEN** an extraction worker with adaptive scaling enabled enters its polling cycle
- **THEN** the worker queries the system health score before fetching jobs
- **AND** adjusts its concurrency limit based on the health score and configured bounds
- **AND** fetches only up to the adjusted concurrency limit

#### Scenario: Semaphore-based concurrency control

- **WHEN** processing extraction jobs with adaptive concurrency
- **THEN** the worker maintains a buffered channel semaphore with capacity equal to current concurrency
- **AND** updates the semaphore capacity when concurrency is adjusted
- **AND** blocks new job starts if the semaphore is full

#### Scenario: In-flight job handling during concurrency reduction

- **WHEN** concurrency is reduced while extraction jobs are in flight
- **THEN** the worker allows currently running jobs to complete normally
- **AND** waits for running jobs to finish before starting new jobs if over the new limit
- **AND** logs delayed jobs due to health-based throttling

### Requirement: Worker-Specific Adaptive Scaling Metrics

The system SHALL expose Prometheus metrics for adaptive scaling behavior per extraction worker type.

#### Scenario: Current concurrency metrics

- **WHEN** an extraction worker's concurrency is adjusted
- **THEN** the system updates a `extraction_worker_current_concurrency` gauge
- **AND** labels it with `worker_type` (e.g., "graph_embedding", "chunk_embedding", "document_parsing", "object_extraction")

#### Scenario: Concurrency adjustment events

- **WHEN** a worker's concurrency changes due to health score
- **THEN** the system increments a `extraction_worker_concurrency_adjustments_total` counter
- **AND** labels it with:
  - `worker_type`
  - `direction` ("increase" or "decrease")
  - `reason` ("health_critical", "health_warning", "health_safe")

#### Scenario: Throttled extraction jobs

- **WHEN** extraction jobs are delayed due to concurrency limits
- **THEN** the system increments a `extraction_jobs_throttled_total` counter
- **AND** labels it with `worker_type`

### Requirement: Gradual Rollout Support

The system SHALL support enabling adaptive scaling gradually across different extraction worker types.

#### Scenario: Enable adaptive scaling for ChunkEmbedding worker only

- **WHEN** an operator enables adaptive scaling for the ChunkEmbedding worker
- **THEN** only ChunkEmbedding jobs use health-aware concurrency
- **AND** other workers (GraphEmbedding, DocumentParsing, ObjectExtraction) continue with static concurrency
- **AND** the system logs which workers have adaptive scaling enabled

#### Scenario: Phased rollout across worker types

- **WHEN** an operator incrementally enables adaptive scaling for additional worker types
- **THEN** each worker independently adjusts concurrency based on health
- **AND** configuration changes apply on the next polling cycle without restart
- **AND** the system maintains separate metrics per worker type

#### Scenario: Emergency rollback for a worker type

- **WHEN** an operator disables adaptive scaling for a specific worker type
- **THEN** that worker immediately reverts to its configured static concurrency
- **AND** other workers with adaptive scaling remain unaffected
- **AND** the change takes effect within one polling cycle

### Requirement: Extraction Worker Logging for Adaptive Scaling

The system SHALL log adaptive scaling behavior for extraction workers with context for debugging.

#### Scenario: Concurrency adjustment logging

- **WHEN** an extraction worker's concurrency is adjusted
- **THEN** the system logs at INFO level:
  - Worker type (e.g., "GraphEmbedding")
  - Previous concurrency
  - New concurrency
  - Current health score
  - Triggering health zone (critical/warning/safe)

#### Scenario: Health-based job throttling logging

- **WHEN** extraction jobs are delayed due to health-based concurrency limits
- **THEN** the system logs at DEBUG level:
  - Worker type
  - Number of jobs waiting
  - Current concurrency limit
  - Current health score

#### Scenario: Configuration change logging

- **WHEN** an operator updates adaptive scaling configuration for a worker
- **THEN** the system logs at INFO level:
  - Worker type
  - Changed fields (enable_adaptive_scaling, min_concurrency, max_concurrency)
  - Previous and new values
  - Operator identity (if available)

