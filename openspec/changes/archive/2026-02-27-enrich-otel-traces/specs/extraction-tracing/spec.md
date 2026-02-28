## ADDED Requirements

### Requirement: Extraction job span per worker job
Each extraction worker (document parsing, object extraction, chunk embedding, graph embedding, graph relationship embedding) SHALL create an OTel span for every job it processes. The span SHALL begin when the job is dequeued and end when the job completes or fails.

#### Scenario: Document parsing job produces a span
- **WHEN** the DocumentParsingWorker dequeues a job
- **THEN** a span named `extraction.document_parsing` SHALL be created with attributes: `emergent.job.id`, `emergent.document.id`, `emergent.project.id`, `emergent.document.content_type`
- **AND** the span SHALL end with `ok` status on success or `error` status on failure with `error.message` set

#### Scenario: Object extraction job produces a span
- **WHEN** the ObjectExtractionWorker dequeues a job
- **THEN** a span named `extraction.object_extraction` SHALL be created with attributes: `emergent.job.id`, `emergent.document.id`, `emergent.project.id`
- **AND** on completion the span SHALL include attributes: `emergent.extraction.entity_count`, `emergent.extraction.relationship_count`

#### Scenario: Embedding jobs produce spans
- **WHEN** ChunkEmbeddingWorker, GraphEmbeddingWorker, or GraphRelationshipEmbeddingWorker dequeue a job
- **THEN** a span named `extraction.chunk_embedding`, `extraction.graph_embedding`, or `extraction.relationship_embedding` respectively SHALL be created
- **AND** the span SHALL carry `emergent.job.id` and `emergent.project.id`

#### Scenario: Failed job records error on span
- **WHEN** any extraction job fails with an error
- **THEN** the span status SHALL be set to `error`
- **AND** `span.RecordError(err)` SHALL be called to capture the error type and message
- **AND** no stack trace or payload content SHALL be included in span attributes

### Requirement: No payload content in extraction spans
Extraction spans SHALL NOT include document text, chunk content, embedding vectors, LLM prompts, or LLM completions as span attributes or span events. Only IDs, counts, and timing are permitted.

#### Scenario: Entity text is not in span attributes
- **WHEN** an object extraction job completes and entities have been extracted
- **THEN** the span attributes SHALL NOT contain any `entity.name`, `entity.description`, or `entity.content` fields
- **AND** only `emergent.extraction.entity_count` (an integer) SHALL be recorded

### Requirement: ExtractionPipeline child spans for stages
When the ExtractionPipeline runs inside an ObjectExtractionWorker job span, it SHALL create child spans for each major stage: entity extraction LLM call, relationship extraction LLM call, and quality check.

#### Scenario: Pipeline stages appear as child spans
- **WHEN** an object extraction job span is active and ExtractionPipeline.Run() executes
- **THEN** the trace SHALL contain child spans: `extraction.pipeline.extract_entities`, `extraction.pipeline.extract_relationships`, `extraction.pipeline.quality_check`
- **AND** each child span SHALL include `emergent.job.id` propagated from the parent context

#### Scenario: Quality check span records orphan rate
- **WHEN** the quality check stage completes
- **THEN** the `extraction.pipeline.quality_check` span SHALL include attribute `emergent.extraction.orphan_rate` (float, 0.0–1.0)
- **AND** if the orphan rate exceeds threshold, the span SHALL include event `extraction.quality_warning`
